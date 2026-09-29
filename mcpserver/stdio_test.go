package mcpserver

import (
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMissingParamStaysStructuredOnTheToolResult(t *testing.T) {
	cases := []struct {
		name    string
		res     InvokeResult
		err     error
		isError bool
		text    string
	}{
		{
			name:    "missing param",
			res:     InvokeResult{Status: "error", Code: "missing_param", Error: "operation assets.get: id required"},
			err:     errors.New("operation assets.get: id required"),
			isError: true,
			text:    `"code":"missing_param"`,
		},
		{
			name: "confirmation",
			res:  InvokeResult{Status: "confirmation_required", ApprovalID: "id"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool, _, err := invokeToolResult(tc.res, tc.err)
			require.NoError(t, err)
			assert.Equal(t, tc.isError, tool.IsError)
			if tc.text == "" {
				return
			}
			require.NotEmpty(t, tool.Content)
			text := tool.Content[0].(*mcp.TextContent).Text
			assert.Contains(t, text, tc.text)
		})
	}
}
