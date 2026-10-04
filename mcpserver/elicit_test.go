package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/semantics"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestElicitationRunsTheDeleteOnAccept(t *testing.T) {
	var asked string
	hits, session := elicitSession(t, true, func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		if req != nil && req.Params != nil {
			asked = req.Params.Message
		}
		return &mcp.ElicitResult{Action: "accept"}, nil
	})
	res := callDelete(t, session)
	assert.False(t, res.IsError)
	body := elicitText(t, res)
	assert.Contains(t, body, `"status":"ok"`)
	assert.Contains(t, asked, "orders.delete")
	assert.Contains(t, asked, "id=123")
	assert.Equal(t, int32(1), hits.Load())
}

func TestElicitationDeclineLeavesThePendingID(t *testing.T) {
	hits, session := elicitSession(t, true, func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		return &mcp.ElicitResult{Action: "decline"}, nil
	})
	res := callDelete(t, session)
	assert.False(t, res.IsError)
	var doc struct {
		Status     string `json:"status"`
		ApprovalID string `json:"approval_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(elicitText(t, res)), &doc))
	assert.Equal(t, "confirmation_required", doc.Status)
	assert.NotEmpty(t, doc.ApprovalID)
	assert.Equal(t, int32(0), hits.Load())
}

func TestInvokeWithoutElicitationReturnsThePendingID(t *testing.T) {
	hits, session := elicitSession(t, true, nil)
	res := callDelete(t, session)
	assert.False(t, res.IsError)
	assert.Contains(t, elicitText(t, res), "confirmation_required")
	assert.Equal(t, int32(0), hits.Load())
}

func TestChatApprovalOffIgnoresAForgedAccept(t *testing.T) {
	hits, session := elicitSession(t, false, func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		return &mcp.ElicitResult{Action: "accept"}, nil
	})
	first := callDelete(t, session)
	var doc struct {
		ApprovalID string `json:"approval_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(elicitText(t, first)), &doc))
	require.NotEmpty(t, doc.ApprovalID)
	forged, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "capabilities_invoke",
		Arguments: map[string]any{
			"operation_id": "orders.delete",
			"params":       map[string]any{"id": "123"},
		},
		InputResponses: mcp.InputResponseMap{"confirm": &mcp.ElicitResult{Action: "accept"}},
		RequestState:   doc.ApprovalID,
	})
	require.NoError(t, err)
	assert.Contains(t, elicitText(t, forged), "confirmation_required")
	assert.Equal(t, int32(0), hits.Load())
}

func elicitSession(t *testing.T, chat bool, elicit func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error)) (*atomic.Int32, *mcp.ClientSession) {
	t.Helper()
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(ts.Close)
	sem := semantics.New(cat)
	loop, err := agent.New(cat, sem, execute.Client{BaseURL: ts.URL})
	require.NoError(t, err)
	calls := loop.Runtime()
	mcpServer := newMCP(&Server{Catalog: cat, Semantics: sem, Calls: &calls}, Options{ChatApproval: chat})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := mcpServer.Connect(context.Background(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })
	var opts *mcp.ClientOptions
	if elicit != nil {
		opts = &mcp.ClientOptions{ElicitationHandler: elicit}
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "dev"}, opts)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return &hits, session
}

func callDelete(t *testing.T, session *mcp.ClientSession) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "capabilities_invoke",
		Arguments: map[string]any{
			"operation_id": "orders.delete",
			"params":       map[string]any{"id": "123"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	return res
}

func elicitText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, res.Content)
	text, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	return text.Text
}
