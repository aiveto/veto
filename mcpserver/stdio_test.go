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
			res:     InvokeResult{Status: "error", Code: "missing_param", Error: "operation orders.get: id required"},
			err:     errors.New("operation orders.get: id required"),
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
			text, ok := tool.Content[0].(*mcp.TextContent)
			require.True(t, ok)
			assert.Contains(t, text.Text, tc.text)
		})
	}
}

func TestInvokeErrorReturnsTheSanitizedCause(t *testing.T) {
	const secret = "super-secret"
	cause := `Get "https://user:` + secret + `@api.example/orders?api_key=` + secret + `": dial tcp: connection refused Authorization: Bearer ` + secret
	tool, _, err := invokeToolResult(InvokeResult{Status: "error", OperationID: "orders.get"}, errors.New(cause))
	require.NoError(t, err)
	require.NotNil(t, tool)
	assert.True(t, tool.IsError)
	require.NotEmpty(t, tool.Content)
	text, ok := tool.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, text.Text, "connection refused")
	assert.Contains(t, text.Text, `"error"`)
	assert.NotContains(t, text.Text, secret)

	stored, _, err := invokeToolResult(InvokeResult{Status: "error", Error: "api_key=" + secret}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, stored.Content)
	storedText, ok := stored.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, storedText.Text, "api_key=REDACTED")
	assert.NotContains(t, storedText.Text, secret)
}
