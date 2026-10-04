package mcpserver

import (
	"errors"
	"net/http"
	"testing"

	"github.com/aiveto/veto/capability"
	"github.com/aiveto/veto/catalog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolAnnotationsFollowTheOperation(t *testing.T) {
	search := readOnlyAnnotations()
	assert.True(t, search.ReadOnlyHint)
	require.NotNil(t, search.DestructiveHint)
	assert.False(t, *search.DestructiveHint)

	invoke := invokeAnnotations(true)
	assert.False(t, invoke.ReadOnlyHint)
	require.NotNil(t, invoke.DestructiveHint)
	assert.True(t, *invoke.DestructiveHint)

	get := operationAnnotations(&catalog.Operation{Method: http.MethodGet, SideEffect: catalog.SideEffectNone})
	assert.True(t, get.ReadOnlyHint)
	del := operationAnnotations(&catalog.Operation{Method: http.MethodDelete, Kind: catalog.KindDelete, SideEffect: catalog.SideEffectDestructive})
	assert.False(t, del.ReadOnlyHint)
	require.NotNil(t, del.DestructiveHint)
	assert.True(t, *del.DestructiveHint)

	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{ID: "orders.get", Method: http.MethodGet, Group: "orders", SideEffect: catalog.SideEffectNone},
		{ID: "orders.delete", Method: http.MethodDelete, Group: "orders", Kind: catalog.KindDelete, SideEffect: catalog.SideEffectDestructive},
		{ID: "customers.get", Method: http.MethodGet, Group: "customers", SideEffect: catalog.SideEffectNone},
	}}
	orders := groupAnnotations(cat, "orders")
	assert.False(t, orders.ReadOnlyHint)
	require.NotNil(t, orders.DestructiveHint)
	assert.True(t, *orders.DestructiveHint)
	customers := groupAnnotations(cat, "customers")
	assert.True(t, customers.ReadOnlyHint)
}

func TestMissingParamStaysStructuredOnTheToolResult(t *testing.T) {
	cases := []struct {
		name    string
		res     capability.InvokeResult
		err     error
		isError bool
		text    string
	}{
		{
			name:    "missing param",
			res:     capability.InvokeResult{Status: "error", Code: "missing_param", Error: "operation orders.get: id required"},
			err:     errors.New("operation orders.get: id required"),
			isError: true,
			text:    `"code":"missing_param"`,
		},
		{
			name: "confirmation",
			res:  capability.InvokeResult{Status: "confirmation_required", ApprovalID: "id"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool, _, err := invokeToolResult(nil, tc.res, tc.err)
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
	tool, _, err := invokeToolResult(nil, capability.InvokeResult{Status: "error", OperationID: "orders.get"}, errors.New(cause))
	require.NoError(t, err)
	require.NotNil(t, tool)
	assert.True(t, tool.IsError)
	require.NotEmpty(t, tool.Content)
	text, ok := tool.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, text.Text, "connection refused")
	assert.Contains(t, text.Text, `"error"`)
	assert.NotContains(t, text.Text, secret)

	stored, _, err := invokeToolResult(nil, capability.InvokeResult{Status: "error", Error: "api_key=" + secret}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, stored.Content)
	storedText, ok := stored.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, storedText.Text, "api_key=REDACTED")
	assert.NotContains(t, storedText.Text, secret)
}
