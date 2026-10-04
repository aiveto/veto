package mcpserver

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runtime"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version is the MCP server build. A release sets it with -X.
var Version = "dev"

type (
	Options struct {
		Pins       []string
		DirectPins bool
		Grouped    bool
		// ChatApproval lets an elicitation answer approve a held call. RunStdio turns it on.
		ChatApproval bool
	}

	searchArgs struct {
		Query  string `json:"query" jsonschema:"search query"`
		Offset int    `json:"offset,omitempty" jsonschema:"hit offset"`
		Limit  int    `json:"limit,omitempty" jsonschema:"page size"`
	}

	describeArgs struct {
		OperationID string `json:"operation_id" jsonschema:"operation id"`
	}

	invokeArgs struct {
		OperationID string         `json:"operation_id" jsonschema:"operation id"`
		Params      map[string]any `json:"params,omitempty" jsonschema:"parameters; strings, or a JSON object for body"`
		ApprovalID  string         `json:"approval_id,omitempty" jsonschema:"approved id from veto approve; a pending id does not run the call"`
		Token       string         `json:"token,omitempty" jsonschema:"user token for this call when the scheme source is invoke"`
		Preview     bool           `json:"preview,omitempty" jsonschema:"resolve, validate, and check policy, then stop before a token URL and upstream HTTP"`
		Fields      []string       `json:"fields,omitempty" jsonschema:"response fields to return; omit them to keep the whole body"`
		Offset      int            `json:"offset,omitempty" jsonschema:"page offset when fields are set"`
		Limit       int            `json:"limit,omitempty" jsonschema:"page size when fields are set"`
	}
)

func RunStdio(ctx context.Context, srv *Server, opt Options) error {
	opt.ChatApproval = true
	return newMCP(srv, opt).Run(ctx, &mcp.StdioTransport{})
}

func newMCP(srv *Server, opt Options) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "veto", Version: Version}, nil)
	register(server, srv, opt)
	return server
}

func register(server *mcp.Server, srv *Server, opt Options) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "capabilities_search",
		Description: "Search operations in the contract catalog",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args searchArgs) (*mcp.CallToolResult, any, error) {
		if err := ctx.Err(); err != nil {
			return toolError(err)
		}
		matches := srv.Search(args.Query, args.Offset, args.Limit)
		b, err := jsonv2.Marshal(matches)
		if err != nil {
			return toolError(err)
		}
		return textResult(string(b))
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "capabilities_describe",
		Description: "Describe one operation by id",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args describeArgs) (*mcp.CallToolResult, any, error) {
		if err := ctx.Err(); err != nil {
			return toolError(err)
		}
		b, err := srv.Describe(args.OperationID)
		if err != nil {
			return toolError(err)
		}
		return textResult(string(b))
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "capabilities_invoke",
		Description: "Invoke an operation through policy and HTTP. params values are strings. params.body may be a JSON object and is sent as the request body. confirmation_required includes a pending id. That id does not run the call. why is held until you approve or missing auth. http is true only when upstream HTTP left. When chat approval is on and the host supports elicitation, the host asks the person; accept runs the call, and decline leaves the pending id. veto approve records the approval and prints the id a later invoke accepts once. preview stops before a token URL and before upstream HTTP. fields names the JSON fields a successful call returns. With no fields, the body is unchanged.",
		Annotations: invokeAnnotations(true),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args invokeArgs) (*mcp.CallToolResult, any, error) {
		return invokeCall(ctx, req, srv, args, opt.ChatApproval)
	})

	if opt.Grouped {
		for _, resource := range groupedResources(srv.Catalog) {
			group := resource
			mcp.AddTool(server, &mcp.Tool{
				Name:        group,
				Description: "Invoke an operation in " + group,
				Annotations: groupAnnotations(srv.Catalog, group),
			}, func(ctx context.Context, req *mcp.CallToolRequest, args invokeArgs) (*mcp.CallToolResult, any, error) {
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
			}, func(ctx context.Context, req *mcp.CallToolRequest, args invokeArgs) (*mcp.CallToolResult, any, error) {
				args.OperationID = pinnedID
				return invokeCall(ctx, req, srv, args, opt.ChatApproval)
			})
		}
	}
}

func invokeCall(ctx context.Context, req *mcp.CallToolRequest, srv *Server, args invokeArgs, chat bool) (*mcp.CallToolResult, any, error) {
	caller := callerID(ctx, req)
	if args.Preview {
		out, err := srv.Preview(ctx, runtime.Request{
			Operation: args.OperationID,
			Arguments: args.Params,
			Caller:    caller,
		})
		return previewToolResult(out, err)
	}
	answered := false
	if reply, ok := elicitationReply(req); ok && chat {
		answered = true
		if reply == nil || reply.Action != "accept" {
			return invokeToolResult(InvokeResult{
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
	return invokeToolResult(res, err)
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

func acceptElicitation(ctx context.Context, srv *Server, req *mcp.CallToolRequest, operationID string) (string, error) {
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

func elicitConfirmation(res InvokeResult, pending *policy.PendingConfirmation) *mcp.CallToolResult {
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

func previewToolResult(out runtime.Preview, callErr error) (*mcp.CallToolResult, any, error) {
	if callErr != nil {
		return toolError(callErr)
	}
	b, err := jsonv2.Marshal(out)
	if err != nil {
		return toolError(err)
	}
	result, _, err := textResult(string(b))
	if result != nil {
		result.IsError = len(out.Errors) > 0
	}
	return result, nil, err
}

func invokeToolResult(res InvokeResult, callErr error) (*mcp.CallToolResult, any, error) {
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
	b, err := jsonv2.Marshal(res)
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
