package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/catalog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type (
	Options struct {
		Pins       []string
		DirectPins bool
		Grouped    bool
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
	}
)

func RunStdio(ctx context.Context, srv *Server, opt Options) error {
	impl := &mcp.Implementation{Name: "veto", Version: "0.1.0"}
	server := mcp.NewServer(impl, nil)
	register(server, srv, opt)
	return server.Run(ctx, &mcp.StdioTransport{})
}

func register(server *mcp.Server, srv *Server, opt Options) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "capabilities_search",
		Description: "Search operations in the contract catalog",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args searchArgs) (*mcp.CallToolResult, any, error) {
		if err := ctx.Err(); err != nil {
			return toolError(err)
		}
		matches := srv.Search(args.Query, args.Offset, args.Limit)
		b, err := json.Marshal(matches)
		if err != nil {
			return toolError(err)
		}
		return textResult(string(b))
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "capabilities_describe",
		Description: "Describe one operation by id",
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
		Description: "Invoke an operation through policy and HTTP. params values are strings. params.body may be a JSON object and is sent as the request body. confirmation_required includes a pending id. That id does not run the call. veto approve records the approval and prints the id a later invoke accepts once.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args invokeArgs) (*mcp.CallToolResult, any, error) {
		return invokeCall(ctx, srv, args)
	})

	if opt.Grouped {
		for _, resource := range groupedResources(srv.Catalog) {
			group := resource
			mcp.AddTool(server, &mcp.Tool{
				Name:        group,
				Description: "Invoke an operation in " + group,
			}, func(ctx context.Context, req *mcp.CallToolRequest, args invokeArgs) (*mcp.CallToolResult, any, error) {
				op := srv.Catalog.ByID(args.OperationID)
				if op == nil || op.Group != group {
					return toolError(fmt.Errorf("operation %q is not in group %s", args.OperationID, group))
				}
				return invokeCall(ctx, srv, args)
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
			}, func(ctx context.Context, req *mcp.CallToolRequest, args invokeArgs) (*mcp.CallToolResult, any, error) {
				args.OperationID = pinnedID
				return invokeCall(ctx, srv, args)
			})
		}
	}
}

func invokeCall(ctx context.Context, srv *Server, args invokeArgs) (*mcp.CallToolResult, any, error) {
	params, err := stringParams(args.Params)
	if err != nil {
		return toolError(err)
	}
	ctx = auth.WithUserToken(ctx, args.Token)
	res, err := srv.Invoke(ctx, args.OperationID, params, args.ApprovalID)
	return invokeToolResult(res, err)
}

func stringParams(in map[string]any) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		switch val := v.(type) {
		case nil:
			out[k] = ""
		case string:
			out[k] = val
		default:
			text, err := jsonText(val)
			if err != nil {
				return nil, fmt.Errorf("param %s: %w", k, err)
			}
			out[k] = text
		}
	}
	return out, nil
}

func jsonText(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

func invokeToolResult(res InvokeResult, callErr error) (*mcp.CallToolResult, any, error) {
	if callErr != nil && res.Status == "" {
		return toolError(callErr)
	}
	b, err := json.Marshal(res)
	if err != nil {
		return toolError(err)
	}
	result, _, err := textResult(string(b))
	if result != nil {
		result.IsError = res.Status != "" && res.Status != "ok" && res.Status != "confirmation_required"
	}
	return result, nil, err
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
