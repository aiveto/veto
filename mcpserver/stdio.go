// Package mcpserver is the MCP adapter for search, describe, and invoke.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/capability"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runtime"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Instructions is the procedure a host gives the model on connect.
const Instructions = "Search, then describe, then invoke. A delete waits until confirmation is stored."

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
	server, err := newMCP(srv, opt)
	if err != nil {
		return err
	}
	return server.Run(ctx, &mcp.StdioTransport{})
}

func newMCP(srv *capability.Server, opt Options) (*mcp.Server, error) {
	if srv != nil {
		if err := ValidateRegistration(srv.Catalog, opt); err != nil {
			return nil, err
		}
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "veto", Version: Version}, &mcp.ServerOptions{
		Instructions: Instructions,
	})
	register(server, srv, opt)
	return server, nil
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
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ capability.InvokeArgs) (*mcp.CallToolResult, any, error) {
		args, err := decodeInvokeArgs(req)
		if err != nil {
			return toolError(err)
		}
		return invokeCall(ctx, req, srv, args, opt.ChatApproval)
	})

	if opt.Grouped {
		for _, resource := range groupedResources(srv.Catalog) {
			group := resource
			mcp.AddTool(server, &mcp.Tool{
				Name:        group,
				Description: groupedDescription(srv.Catalog, group),
				Annotations: groupAnnotations(srv.Catalog, group),
			}, func(ctx context.Context, req *mcp.CallToolRequest, _ capability.InvokeArgs) (*mcp.CallToolResult, any, error) {
				args, err := decodeInvokeArgs(req)
				if err != nil {
					return toolError(err)
				}
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
			}, func(ctx context.Context, req *mcp.CallToolRequest, _ capability.PinArgs) (*mcp.CallToolResult, any, error) {
				pin, err := decodePinArgs(req)
				if err != nil {
					return toolError(err)
				}
				return invokeCall(ctx, req, srv, pin.Invoke(pinnedID), opt.ChatApproval)
			})
		}
	}
}

func decodeInvokeArgs(req *mcp.CallToolRequest) (capability.InvokeArgs, error) {
	if req == nil || req.Params == nil {
		return capability.InvokeArgs{}, nil
	}
	return capability.DecodeInvoke(req.Params.Arguments)
}

func decodePinArgs(req *mcp.CallToolRequest) (capability.PinArgs, error) {
	if req == nil || req.Params == nil {
		return capability.PinArgs{}, nil
	}
	return capability.DecodePin(req.Params.Arguments)
}

func invokeCall(ctx context.Context, req *mcp.CallToolRequest, srv *capability.Server, args capability.InvokeArgs, chat bool) (*mcp.CallToolResult, any, error) {
	caller := callerID(ctx, req)
	ctx = auth.WithUserToken(ctx, args.Token)
	ctx = auth.WithCaller(ctx, caller)
	call := args.Request()
	call.Caller = caller
	if args.Preview {
		out, b, err := srv.EncodePreview(ctx, call)
		return previewToolResult(out, b, err)
	}
	answered := false
	if reply, ok := elicitationReply(req); ok && chat {
		answered = true
		if reply == nil || reply.Action != "accept" {
			res, b, err := srv.EncodeResult(capability.InvokeResult{
				Status:      runtime.StatusConfirmationRequired,
				ApprovalID:  requestState(req),
				OperationID: args.OperationID,
			}, nil)
			return invokeToolResult(res, b, err)
		}
		approved, err := acceptElicitation(ctx, srv, req, args.OperationID)
		if err != nil {
			return toolError(err)
		}
		call.Approval = approved
	}
	res, b, err := srv.EncodeInvoke(ctx, call)
	if !answered && res.Status == runtime.StatusConfirmationRequired && chat && clientCanElicit(req) {
		var pending *policy.PendingConfirmation
		if srv != nil && srv.Calls != nil && srv.Calls.State != nil {
			pending = srv.Calls.State.Pending(ctx, res.ApprovalID)
		}
		return elicitConfirmation(res, pending), nil, nil
	}
	return invokeToolResult(res, b, err)
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
	return approveHeld(ctx, srv, operationID, requestState(req))
}

func approveHeld(ctx context.Context, srv *capability.Server, operationID, pendingID string) (string, error) {
	if srv == nil || srv.Calls == nil || srv.Calls.State == nil {
		return "", errors.New("confirmation state is not set")
	}
	pending := srv.Calls.State.Pending(ctx, pendingID)
	if pending == nil || pending.OperationID != operationID {
		return "", fmt.Errorf("approval does not match %s", operationID)
	}
	return srv.Calls.State.Approve(ctx, pendingID)
}

func elicitConfirmation(res capability.InvokeResult, pending *policy.PendingConfirmation) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		InputRequests: mcp.InputRequestMap{
			"confirm": confirmElicitParams(res, pending),
		},
		RequestState: res.ApprovalID,
	}
}

func confirmElicitParams(res capability.InvokeResult, pending *policy.PendingConfirmation) *mcp.ElicitParams {
	op := res.OperationID
	var params map[string]string
	var caller string
	if pending != nil {
		op = pending.OperationID
		params = pending.Params
		caller = pending.Caller
	}
	return &mcp.ElicitParams{
		Mode:            "form",
		Message:         policy.ConfirmAsk(caller, op, params),
		RequestedSchema: confirmSchema(),
	}
}

func confirmSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
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

func previewToolResult(out runtime.Preview, b []byte, err error) (*mcp.CallToolResult, any, error) {
	if len(b) == 0 && err != nil {
		return toolError(err)
	}
	result, _, wrapErr := textResult(string(b))
	if result != nil {
		result.IsError = len(out.Errors) > 0
	}
	return result, nil, wrapErr
}

func invokeToolResult(res capability.InvokeResult, b []byte, err error) (*mcp.CallToolResult, any, error) {
	if len(b) == 0 && err != nil {
		return toolError(err)
	}
	result, _, wrapErr := textResult(string(b))
	if result != nil {
		result.IsError = res.Status != "" && res.Status != runtime.StatusOK && res.Status != runtime.StatusConfirmationRequired
	}
	return result, nil, wrapErr
}

func textResult(s string) (*mcp.CallToolResult, any, error) {
	result := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: s}},
	}
	if raw := structuredJSON(s); raw != nil {
		result.StructuredContent = raw
	}
	return result, nil, nil
}

func structuredJSON(s string) json.RawMessage {
	raw := []byte(strings.TrimSpace(s))
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	return json.RawMessage(raw)
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

func groupedDescription(cat *catalog.Catalog, group string) string {
	ids := groupedOperationIDs(cat, group)
	if len(ids) == 0 {
		return "Invoke an operation in " + group
	}
	return "Invoke an operation in " + group + ". Set operation_id to " + strings.Join(ids, ", ") + "."
}

func groupedOperationIDs(cat *catalog.Catalog, group string) []string {
	if cat == nil {
		return nil
	}
	var ids []string
	for _, op := range cat.Operations {
		if op.Group != group || op.Exposure == catalog.ExposureDiscovery {
			continue
		}
		ids = append(ids, op.ID)
	}
	return ids
}

func ValidatePins(cat *catalog.Catalog, pins []string) error {
	return ValidateRegistration(cat, Options{Pins: pins, DirectPins: len(pins) > 0})
}

// ValidateRegistration rejects unknown pins and colliding tool names.
func ValidateRegistration(cat *catalog.Catalog, opt Options) error {
	seen := map[string]string{
		capability.SearchName:   "capability",
		capability.DescribeName: "capability",
		capability.InvokeName:   "capability",
	}
	if opt.Grouped {
		for _, group := range groupedResources(cat) {
			if prev, ok := seen[group]; ok {
				return fmt.Errorf("group %q collides with %s", group, prev)
			}
			seen[group] = "group"
		}
	}
	if !opt.DirectPins {
		return nil
	}
	for _, p := range opt.Pins {
		op := cat.ByID(p)
		if op == nil {
			return fmt.Errorf("unknown pin %q", p)
		}
		if op.Exposure == catalog.ExposureDiscovery {
			return fmt.Errorf("pin %q is discovery-only", p)
		}
		if prev, ok := seen[p]; ok {
			return fmt.Errorf("pin %q collides with %s", p, prev)
		}
		seen[p] = "pin"
	}
	return nil
}
