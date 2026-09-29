package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aiveto/veto/catalog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type (
	// Options configures stdio MCP serving.
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
		OperationID string            `json:"operation_id" jsonschema:"operation id"`
		Params      map[string]string `json:"params" jsonschema:"parameters"`
		ApprovalID  string            `json:"approval_id" jsonschema:"approval id from confirmation"`
	}
)

// RunStdio serves MCP over stdin/stdout.
func RunStdio(ctx context.Context, srv *Server, opt Options) error {
	impl := &mcp.Implementation{Name: "veto", Version: "0.1.0"}
	server := mcp.NewServer(impl, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "capabilities_search",
		Description: "Search operations in the contract catalog",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args searchArgs) (*mcp.CallToolResult, any, error) {
		matches, err := srv.Search(ctx, args.Query, args.Offset, args.Limit)
		if err != nil {
			return toolError(err)
		}
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
		b, err := srv.Describe(ctx, args.OperationID)
		if err != nil {
			return toolError(err)
		}
		return textResult(string(b))
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "capabilities_invoke",
		Description: "Invoke an operation through policy and HTTP. confirmation_required includes approval_id. Send that id on the next invoke to resume. Pending calls stay in this process.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args invokeArgs) (*mcp.CallToolResult, any, error) {
		res, err := srv.Invoke(ctx, args.OperationID, args.Params, args.ApprovalID)
		if err != nil && res.Status == "error" {
			return toolError(err)
		}
		b, err := json.Marshal(res)
		if err != nil {
			return toolError(err)
		}
		return textResult(string(b))
	})

	if opt.Grouped {
		for _, resource := range groupedResources(srv.Catalog) {
			group := resource
			mcp.AddTool(server, &mcp.Tool{
				Name:        group,
				Description: "Invoke an operation in " + group,
			}, func(ctx context.Context, req *mcp.CallToolRequest, args invokeArgs) (*mcp.CallToolResult, any, error) {
				op := srv.Catalog.ByID(args.OperationID)
				if op == nil || op.Group != group || op.Exposure == catalog.ExposureDiscovery {
					return toolError(fmt.Errorf("operation %q is not in group %s", args.OperationID, group))
				}
				res, err := srv.Invoke(ctx, args.OperationID, args.Params, args.ApprovalID)
				if err != nil && res.Status == "error" {
					return toolError(err)
				}
				b, err := json.Marshal(res)
				if err != nil {
					return toolError(err)
				}
				return textResult(string(b))
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
				res, err := srv.Invoke(ctx, args.OperationID, args.Params, args.ApprovalID)
				if err != nil && res.Status == "error" {
					return toolError(err)
				}
				b, err := json.Marshal(res)
				if err != nil {
					return toolError(err)
				}
				return textResult(string(b))
			})
		}
	}

	return server.Run(ctx, &mcp.StdioTransport{})
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

// RegisterTools lists tool names that would be registered (for tests).
func RegisterTools(cat *catalog.Catalog, opt Options) []string {
	return ToolNames(cat, opt.Pins, opt.DirectPins, opt.Grouped)
}

// ValidatePins returns an error if a pin is not in the catalog.
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
