package mcpserver

import (
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMissingParamStaysStructuredOnTheToolResult(t *testing.T) {
	res := InvokeResult{Status: "error", Code: "missing_param", Error: "operation assets.get: id required"}
	tool, _, err := invokeToolResult(res, errors.New(res.Error))
	if err != nil {
		t.Fatal(err)
	}
	if !tool.IsError {
		t.Fatal("missing param was not a tool error")
	}
	text := tool.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"code":"missing_param"`) {
		t.Fatalf("tool text: %s", text)
	}
	confirm, _, err := invokeToolResult(InvokeResult{Status: "confirmation_required", ApprovalID: "id"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if confirm.IsError {
		t.Fatal("confirmation was reported as a tool error")
	}
}
