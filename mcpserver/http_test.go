package mcpserver_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/mcpserver"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/semantics"
	"github.com/aiveto/veto/telemetry"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPTwoCallersDoNotShareApprovalsOrTokens(t *testing.T) {
	const (
		adaSecret   = "caller-secret-ada"
		graceSecret = "caller-secret-grace"
		userAda     = "user-token-ada"
		userGrace   = "user-token-grace"
	)
	var hits atomic.Int32
	var mu sync.Mutex
	var auths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	cat := loadSpec(t, upstreamSpec)
	sem := semantics.NewDerived(cat)
	loop, err := agent.New(cat, sem, execute.Client{
		BaseURL: upstream.URL,
		Creds: auth.New(auth.Options{Schemes: []auth.Scheme{{
			Name: "userAuth", Source: "invoke",
		}}}),
	})
	require.NoError(t, err)
	loop.State.SetNonceDir(t.TempDir())
	require.NoError(t, loop.State.SetSigner([]byte("approval-secret"), 0))
	calls := loop.Runtime()
	handler, err := mcpserver.Handler(&mcpserver.Server{
		Catalog: cat, Semantics: sem, Calls: &calls,
	}, mcpserver.Options{}, []mcpserver.Identity{
		{ID: "ada", Token: adaSecret},
		{ID: "grace", Token: graceSecret},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bound, err := mcpserver.Listen(ctx, "127.0.0.1:0", handler)
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(bound)
	require.NoError(t, err)
	assert.NotEqual(t, "7433", port)
	assert.NotEqual(t, mcpserver.DefaultAddr, bound)
	endpoint := "http://" + bound

	rec, err := telemetry.Record()
	require.NoError(t, err)
	defer func() { require.NoError(t, rec.Stop(context.Background())) }()

	invokeBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"capabilities_invoke","arguments":{"operation_id":"orders.delete","params":{"id":"123"}}}}`
	status, body := postMCP(t, endpoint, invokeBody, "")
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.NotContains(t, body, adaSecret)
	assert.Equal(t, int32(0), hits.Load())
	status, body = postMCP(t, endpoint, invokeBody, "not-a-caller")
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.NotContains(t, body, "not-a-caller")
	assert.NotContains(t, body, adaSecret)
	assert.Equal(t, int32(0), hits.Load())

	adaRT := &callerTransport{base: http.DefaultTransport, token: adaSecret}
	graceRT := &callerTransport{base: http.DefaultTransport, token: graceSecret}
	ada := dialMCP(t, endpoint, adaRT)
	grace := dialMCP(t, endpoint, graceRT)
	listed, err := ada.ListTools(context.Background(), nil)
	require.NoError(t, err)
	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	assert.ElementsMatch(t, []string{"capabilities_search", "capabilities_describe", "capabilities_invoke"}, names)

	previewText := callTool(t, ada, map[string]any{
		"operation_id": "orders.delete",
		"params":       map[string]any{"id": "123"},
		"preview":      true,
	})
	assert.Contains(t, previewText, `"approval_required":true`)
	assert.NotContains(t, previewText, adaSecret)
	assert.Equal(t, int32(0), hits.Load())

	pendingText := callTool(t, ada, map[string]any{
		"operation_id": "orders.delete",
		"params":       map[string]any{"id": "123"},
	})
	pending := decodeInvoke(t, pendingText)
	assert.Equal(t, "confirmation_required", pending.Status)
	assert.NotEmpty(t, pending.ApprovalID)
	assert.Equal(t, int32(0), hits.Load())

	again := callTool(t, ada, map[string]any{
		"operation_id": "orders.delete",
		"params":       map[string]any{"id": "123"},
		"approval_id":  pending.ApprovalID,
	})
	assert.Contains(t, again, "invalid approval")
	assert.Equal(t, int32(0), hits.Load())

	stolen := callTool(t, grace, map[string]any{
		"operation_id": "orders.delete",
		"params":       map[string]any{"id": "123"},
		"approval_id":  pending.ApprovalID,
	})
	assert.Contains(t, stolen, "invalid approval")
	assert.NotContains(t, stolen, adaSecret)
	assert.NotContains(t, stolen, pending.ApprovalID)
	assert.Equal(t, int32(0), hits.Load())

	gracePending := decodeInvoke(t, callTool(t, grace, map[string]any{
		"operation_id": "orders.delete",
		"params":       map[string]any{"id": "123"},
	}))
	assert.Equal(t, "confirmation_required", gracePending.Status)
	assert.NotEqual(t, pending.ApprovalID, gracePending.ApprovalID)

	approved, err := loop.State.Approve(pending.ApprovalID)
	require.NoError(t, err)
	assert.NotEqual(t, pending.ApprovalID, approved)
	assert.True(t, strings.HasPrefix(approved, "v1."))
	cross := callTool(t, grace, map[string]any{
		"operation_id": "orders.delete",
		"params":       map[string]any{"id": "123"},
		"approval_id":  approved,
		"token":        userGrace,
	})
	assert.Contains(t, cross, "invalid approval")
	assert.NotContains(t, cross, approved)
	assert.NotContains(t, cross, userAda)
	assert.NotContains(t, cross, adaSecret)
	assert.Equal(t, int32(0), hits.Load())

	adaGet := callTool(t, ada, map[string]any{
		"operation_id": "orders.get",
		"params":       map[string]any{"id": "123"},
		"token":        userAda,
	})
	assert.Contains(t, adaGet, `"status":"ok"`)
	assert.NotContains(t, adaGet, userAda)
	assert.NotContains(t, adaGet, graceSecret)
	graceGet := callTool(t, grace, map[string]any{
		"operation_id": "orders.get",
		"params":       map[string]any{"id": "123"},
		"token":        userGrace,
	})
	assert.Contains(t, graceGet, `"status":"ok"`)
	assert.NotContains(t, graceGet, userAda)
	assert.NotContains(t, graceGet, userGrace)
	assert.NotContains(t, graceGet, adaSecret)
	mu.Lock()
	gotAuths := append([]string(nil), auths...)
	mu.Unlock()
	assert.Equal(t, []string{"Bearer " + userAda, "Bearer " + userGrace}, gotAuths)

	ran := decodeInvoke(t, callTool(t, ada, map[string]any{
		"operation_id": "orders.delete",
		"params":       map[string]any{"id": "123"},
		"approval_id":  approved,
	}))
	assert.Equal(t, "ok", ran.Status)
	assert.Equal(t, int32(3), hits.Load())

	sid, _ := adaRT.session.Load().(string)
	require.NotEmpty(t, sid)
	status, body = postMCP(t, endpoint, invokeBody, graceSecret, sid)
	assert.Equal(t, http.StatusForbidden, status)
	assert.NotContains(t, body, adaSecret)
	assert.NotContains(t, body, graceSecret)
	assert.Equal(t, int32(3), hits.Load())

	for _, sp := range rec.Spans() {
		blob := sp.Name
		var blobSb210 strings.Builder
		for k, v := range sp.Attrs {
			blobSb210.WriteString(" " + k + "=" + v)
		}
		blob += blobSb210.String()
		assert.NotContains(t, blob, adaSecret)
		assert.NotContains(t, blob, graceSecret)
		assert.NotContains(t, blob, userAda)
		assert.NotContains(t, blob, userGrace)
	}
}

func TestIdentitiesReadNamedEnvVars(t *testing.T) {
	t.Setenv("ADA_CALLER_TOKEN", "secret-ada")
	t.Setenv("GRACE_CALLER_TOKEN", "secret-grace")
	got, err := mcpserver.Identities(map[string]string{
		"ada":   "ADA_CALLER_TOKEN",
		"grace": "GRACE_CALLER_TOKEN",
	}, os.Getenv)
	require.NoError(t, err)
	assert.ElementsMatch(t, []mcpserver.Identity{
		{ID: "ada", Token: "secret-ada"},
		{ID: "grace", Token: "secret-grace"},
	}, got)
	_, err = mcpserver.Identities(map[string]string{"ada": "MISSING_CALLER_TOKEN"}, os.Getenv)
	require.ErrorContains(t, err, "unset")
	assert.NotContains(t, err.Error(), "secret-ada")
}

type callerTransport struct {
	base    http.RoundTripper
	token   string
	session atomic.Value
}

func (c *callerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set(mcpserver.CallerHeader, c.token)
	base := c.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err == nil && resp != nil {
		if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
			c.session.Store(sid)
		}
	}
	return resp, err
}

func dialMCP(t *testing.T, endpoint string, rt http.RoundTripper) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "veto-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           &http.Client{Transport: rt},
		DisableStandaloneSSE: true,
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callTool(t *testing.T, session *mcp.ClientSession, args map[string]any) string {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "capabilities_invoke",
		Arguments: args,
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotEmpty(t, res.Content)
	text, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	return text.Text
}

func decodeInvoke(t *testing.T, text string) mcpserver.InvokeResult {
	t.Helper()
	var out mcpserver.InvokeResult
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	return out
}

func postMCP(t *testing.T, endpoint, body, caller string, session ...string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, endpoint, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if caller != "" {
		req.Header.Set(mcpserver.CallerHeader, caller)
	}
	if len(session) > 0 && session[0] != "" {
		req.Header.Set("Mcp-Session-Id", session[0])
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	require.NoError(t, err)
	require.NoError(t, closeErr)
	return resp.StatusCode, string(raw)
}

func loadSpec(t *testing.T, spec string) *catalog.Catalog {
	t.Helper()
	path := filepath.Join(t.TempDir(), "openapi.yaml")
	require.NoError(t, os.WriteFile(path, []byte(spec), 0o600))
	cat, err := openapi.Load(context.Background(), path)
	require.NoError(t, err)
	return cat
}

const upstreamSpec = `openapi: 3.0.3
info:
  title: Orders
  version: "1"
servers:
  - url: http://127.0.0.1:9
paths:
  /orders/{id}:
    get:
      operationId: orders.get
      summary: Get order
      security:
        - userAuth: []
      parameters:
        - name: id
          in: path
          required: true
          schema: {type: string}
      responses:
        "200": {description: ok}
    delete:
      operationId: orders.delete
      summary: Delete order
      parameters:
        - name: id
          in: path
          required: true
          schema: {type: string}
      responses:
        "204": {description: deleted}
components:
  securitySchemes:
    userAuth:
      type: http
      scheme: bearer
`
