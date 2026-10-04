// Package mcpserver is the MCP adapter for search, describe, and invoke.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/capability"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runtime"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version is the MCP server build. A release sets it with -X.
var Version = "dev"

type Options struct {
	Pins       []string
	DirectPins bool
	Grouped    bool
	// ChatApproval lets an elicitation answer approve a held call. RunStdio turns it on.
	ChatApproval bool
}

func RunStdio(ctx context.Context, srv *capability.Server, opt Options) error {
	opt.ChatApproval = true
	return newMCP(srv, opt).Run(ctx, &mcp.StdioTransport{})
}

func newMCP(srv *capability.Server, opt Options) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "veto", Version: Version}, nil)
	register(server, srv, opt)
	return server
}

func register(server *mcp.Server, srv *capability.Server, opt Options) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        capability.SearchName,
		Description: capability.SearchDescription,
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args capability.SearchArgs) (*mcp.CallToolResult, any, error) {
		if err := ctx.Err(); err != nil {
			return toolError(err)
		}
		b, err := srv.RunSearch(args)
		if err != nil {
			return toolError(err)
		}
		return textResult(string(b))
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        capability.DescribeName,
		Description: capability.DescribeDescription,
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args capability.DescribeArgs) (*mcp.CallToolResult, any, error) {
		if err := ctx.Err(); err != nil {
			return toolError(err)
		}
		b, err := srv.RunDescribe(args)
		if err != nil {
			return toolError(err)
		}
		return textResult(string(b))
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        capability.InvokeName,
		Description: capability.InvokeDescription,
		Annotations: invokeAnnotations(true),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args capability.InvokeArgs) (*mcp.CallToolResult, any, error) {
		return invokeCall(ctx, req, srv, args, opt.ChatApproval)
	})

	if opt.Grouped {
		for _, resource := range groupedResources(srv.Catalog) {
			group := resource
			mcp.AddTool(server, &mcp.Tool{
				Name:        group,
				Description: "Invoke an operation in " + group,
				Annotations: groupAnnotations(srv.Catalog, group),
			}, func(ctx context.Context, req *mcp.CallToolRequest, args capability.InvokeArgs) (*mcp.CallToolResult, any, error) {
				op := srv.Catalog.ByID(args.OperationID)
				if op == nil || op.Group != group {
					return toolError(fmt.Errorf("operation %q is not in group %s", args.OperationID, group))
				}
				return invokeCall(ctx, req, srv, args, opt.ChatApproval)
			})
		}
	}

	if opt.DirectPins {
		for _, pin := range opt.Pins {
			op := srv.Catalog.ByID(pin)
			if op == nil {
				continue
			}
			pinnedID := pin
			mcp.AddTool(server, &mcp.Tool{
				Name:        pinnedID,
				Description: op.Description,
				Annotations: operationAnnotations(op),
			}, func(ctx context.Context, req *mcp.CallToolRequest, args capability.InvokeArgs) (*mcp.CallToolResult, any, error) {
				args.OperationID = pinnedID
				return invokeCall(ctx, req, srv, args, opt.ChatApproval)
			})
		}
	}
}

func invokeCall(ctx context.Context, req *mcp.CallToolRequest, srv *capability.Server, args capability.InvokeArgs, chat bool) (*mcp.CallToolResult, any, error) {
	caller := callerID(ctx, req)
	if args.Preview {
		out, err := srv.Preview(ctx, runtime.Request{
			Operation: args.OperationID,
			Arguments: args.Params,
			Caller:    caller,
		})
		return previewToolResult(srv, out, err)
	}
	answered := false
	if reply, ok := elicitationReply(req); ok && chat {
		answered = true
		if reply == nil || reply.Action != "accept" {
			return invokeToolResult(srv, capability.InvokeResult{
				Status:      runtime.StatusConfirmationRequired,
				ApprovalID:  requestState(req),
				OperationID: args.OperationID,
			}, nil)
		}
		approved, err := acceptElicitation(ctx, srv, req, args.OperationID)
		if err != nil {
			return toolError(err)
		}
		args.ApprovalID = approved
	}
	ctx = auth.WithUserToken(ctx, args.Token)
	ctx = auth.WithCaller(ctx, caller)
	res, err := srv.Call(ctx, runtime.Request{
		Operation: args.OperationID,
		Arguments: args.Params,
		Approval:  args.ApprovalID,
		Caller:    caller,
		Fields:    args.Fields,
		Offset:    args.Offset,
		Limit:     args.Limit,
	})
	if !answered && res.Status == runtime.StatusConfirmationRequired && chat && clientCanElicit(req) {
		var pending *policy.PendingConfirmation
		if srv != nil && srv.Calls != nil && srv.Calls.State != nil {
			pending = srv.Calls.State.Pending(ctx, res.ApprovalID)
		}
		return elicitConfirmation(res, pending), nil, nil
	}
	return invokeToolResult(srv, res, err)
}

func clientCanElicit(req *mcp.CallToolRequest) bool {
	if req == nil || req.Session == nil {
		return false
	}
	params := req.Session.InitializeParams()
	return params != nil && params.Capabilities != nil && params.Capabilities.Elicitation != nil
}

func elicitationReply(req *mcp.CallToolRequest) (*mcp.ElicitResult, bool) {
	if req == nil || req.Params == nil {
		return nil, false
	}
	raw, ok := req.Params.InputResponses["confirm"]
	if !ok {
		return nil, false
	}
	reply, ok := raw.(*mcp.ElicitResult)
	if !ok {
		return nil, true
	}
	return reply, true
}

func requestState(req *mcp.CallToolRequest) string {
	if req == nil || req.Params == nil {
		return ""
	}
	return req.Params.RequestState
}

func acceptElicitation(ctx context.Context, srv *capability.Server, req *mcp.CallToolRequest, operationID string) (string, error) {
	if srv == nil || srv.Calls == nil || srv.Calls.State == nil {
		return "", errors.New("confirmation state is not set")
	}
	id := requestState(req)
	pending := srv.Calls.State.Pending(ctx, id)
	if pending == nil || pending.OperationID != operationID {
		return "", fmt.Errorf("approval does not match %s", operationID)
	}
	return srv.Calls.State.Approve(ctx, id)
}

func elicitConfirmation(res capability.InvokeResult, pending *policy.PendingConfirmation) *mcp.CallToolResult {
	op := res.OperationID
	var params map[string]string
	var caller string
	if pending != nil {
		op = pending.OperationID
		params = pending.Params
		caller = pending.Caller
	}
	return &mcp.CallToolResult{
		InputRequests: mcp.InputRequestMap{
			"confirm": &mcp.ElicitParams{
				Message: policy.ConfirmAsk(caller, op, params),
			},
		},
		RequestState: res.ApprovalID,
	}
}

func callerID(ctx context.Context, req *mcp.CallToolRequest) string {
	if req != nil && req.Extra != nil && req.Extra.TokenInfo != nil && req.Extra.TokenInfo.UserID != "" {
		return req.Extra.TokenInfo.UserID
	}
	if info := mcpauth.TokenInfoFromContext(ctx); info != nil && info.UserID != "" {
		return info.UserID
	}
	return auth.Caller(ctx)
}

func previewToolResult(srv *capability.Server, out runtime.Preview, callErr error) (*mcp.CallToolResult, any, error) {
	if callErr != nil {
		return toolError(callErr)
	}
	b, err := srv.Encode(out)
	if err != nil {
		return toolError(err)
	}
	result, _, err := textResult(string(b))
	if result != nil {
		result.IsError = len(out.Errors) > 0
	}
	return result, nil, err
}

func invokeToolResult(srv *capability.Server, res capability.InvokeResult, callErr error) (*mcp.CallToolResult, any, error) {
	if callErr != nil && res.Status == "" {
		return toolError(callErr)
	}
	if res.Status == runtime.StatusError {
		cause := res.Error
		if cause == "" && callErr != nil {
			cause = callErr.Error()
		}
		res.Error = sanitizeCause(cause)
	}
	b, err := srv.Encode(res)
	if err != nil {
		return toolError(err)
	}
	result, _, err := textResult(string(b))
	if result != nil {
		result.IsError = res.Status != "" && res.Status != runtime.StatusOK && res.Status != runtime.StatusConfirmationRequired
	}
	return result, nil, err
}

var (
	authHeader   = regexp.MustCompile(`(?i)\b(authorization|veto-caller)\s*:\s*(?:bearer|basic)?\s*\S+`)
	authScheme   = regexp.MustCompile(`(?i)\b(bearer|basic)\s+\S+`)
	httpURL      = regexp.MustCompile(`https?://[^\s"'<>]+`)
	userinfo     = regexp.MustCompile(`(?i)(https?://)[^/\s@"']+@`)
	secretAssign = regexp.MustCompile(`(?i)\b(access_token|refresh_token|client_secret|api[_-]?key|password|authorization|secret|token)=([^\s&"',;]+)`)
)

func sanitizeCause(msg string) string {
	if msg == "" {
		return ""
	}
	msg = authHeader.ReplaceAllString(msg, "$1: REDACTED")
	msg = authScheme.ReplaceAllString(msg, "$1 REDACTED")
	msg = httpURL.ReplaceAllStringFunc(msg, redactURL)
	msg = userinfo.ReplaceAllString(msg, "${1}REDACTED@")
	return secretAssign.ReplaceAllString(msg, "$1=REDACTED")
}

func redactURL(raw string) string {
	end := len(raw)
	for end > 0 && strings.ContainsRune(".,);", rune(raw[end-1])) {
		end--
	}
	core, tail := raw[:end], raw[end:]
	u, err := url.Parse(core)
	if err != nil || u.Host == "" {
		return raw
	}
	changed := false
	if u.User != nil {
		u.User = url.User("REDACTED")
		changed = true
	}
	q := u.Query()
	for k := range q {
		if sensitiveQuery(k) {
			q.Set(k, "REDACTED")
			changed = true
		}
	}
	if !changed {
		return raw
	}
	u.RawQuery = q.Encode()
	return u.String() + tail
}

func sensitiveQuery(name string) bool {
	switch strings.ToLower(strings.ReplaceAll(name, "-", "_")) {
	case "access_token", "api_key", "apikey", "authorization", "client_secret", "password", "refresh_token", "secret", "token":
		return true
	default:
		return false
	}
}

func textResult(s string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: s}},
	}, nil, nil
}

func toolError(err error) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}, nil, err
}

func readOnlyAnnotations() *mcp.ToolAnnotations {
	no := false
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    true,
		DestructiveHint: &no,
		OpenWorldHint:   &no,
	}
}

func invokeAnnotations(destructive bool) *mcp.ToolAnnotations {
	open := true
	return &mcp.ToolAnnotations{
		DestructiveHint: &destructive,
		OpenWorldHint:   &open,
	}
}

func operationAnnotations(op *catalog.Operation) *mcp.ToolAnnotations {
	if operationReadOnly(op) {
		return readOnlyAnnotations()
	}
	return invokeAnnotations(operationDestructive(op))
}

func groupAnnotations(cat *catalog.Catalog, group string) *mcp.ToolAnnotations {
	if cat == nil {
		return invokeAnnotations(true)
	}
	readOnly := true
	destructive := false
	n := 0
	for i := range cat.Operations {
		op := &cat.Operations[i]
		if op.Group != group {
			continue
		}
		n++
		if !operationReadOnly(op) {
			readOnly = false
		}
		if operationDestructive(op) {
			destructive = true
		}
	}
	if n == 0 || readOnly {
		return readOnlyAnnotations()
	}
	return invokeAnnotations(destructive)
}

func operationReadOnly(op *catalog.Operation) bool {
	if op == nil {
		return false
	}
	if op.SideEffect == catalog.SideEffectWrite || op.SideEffect == catalog.SideEffectDestructive {
		return false
	}
	switch op.Method {
	case http.MethodGet, http.MethodHead, "":
		return true
	default:
		return false
	}
}

func operationDestructive(op *catalog.Operation) bool {
	if op == nil {
		return false
	}
	return op.Kind == catalog.KindDelete || op.SideEffect == catalog.SideEffectDestructive || op.Method == http.MethodDelete
}

func RegisterTools(cat *catalog.Catalog, opt Options) []string {
	return ToolNames(cat, opt.Pins, opt.DirectPins, opt.Grouped)
}

// Grouped mode adds one tool per resource, never one tool per operation.
func ToolNames(cat *catalog.Catalog, pins []string, directPins, grouped bool) []string {
	names := []string{
		capability.SearchName,
		capability.DescribeName,
		capability.InvokeName,
	}
	if directPins {
		names = append(names, pins...)
	}
	if grouped {
		names = append(names, groupedResources(cat)...)
	}
	return names
}

func groupedResources(cat *catalog.Catalog) []string {
	if cat == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, op := range cat.Operations {
		if op.Group == "" || op.Exposure == catalog.ExposureDiscovery || seen[op.Group] {
			continue
		}
		seen[op.Group] = true
		out = append(out, op.Group)
	}
	return out
}

func ValidatePins(cat *catalog.Catalog, pins []string) error {
	for _, p := range pins {
		op := cat.ByID(p)
		if op == nil {
			return fmt.Errorf("unknown pin %q", p)
		}
		if op.Exposure == catalog.ExposureDiscovery {
			return fmt.Errorf("pin %q is discovery-only", p)
		}
	}
	return nil
}
