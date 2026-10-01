package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/openapi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreviewToolSkipsUpstreamAndTokenURL(t *testing.T) {
	var tokenHits, upstreamHits atomic.Int32
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenHits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "preview-access-token", "expires_in": 3600})
	}))
	defer tokenSrv.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer up.Close()

	const secret = "preview-client-secret"
	t.Setenv("PREVIEW_SECRET", secret)
	path := filepath.Join(t.TempDir(), "api.yaml")
	spec := fmt.Sprintf(`openapi: 3.0.3
info: {title: preview, version: "1"}
servers:
  - url: %s
paths:
  /orders:
    post:
      operationId: orders.create
      summary: Create one order for a customer
      requestBody:
        required: true
        content:
          application/json:
            schema: {type: object}
      responses:
        "201": {description: created}
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
security:
  - bearerAuth: []
`, up.URL)
	require.NoError(t, os.WriteFile(path, []byte(spec), 0o644))
	cat, err := openapi.Load(context.Background(), path)
	require.NoError(t, err)
	creds := auth.New(auth.Options{
		Schemes: []auth.Scheme{{
			Name:            "bearerAuth",
			Source:          "client_credentials",
			TokenURL:        tokenSrv.URL,
			ClientID:        "job",
			ClientSecretEnv: "PREVIEW_SECRET",
		}},
		Dir:  t.TempDir(),
		HTTP: tokenSrv.Client(),
	})
	loop, err := agent.New(cat, nil, execute.Client{BaseURL: up.URL, Creds: creds})
	require.NoError(t, err)
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "veto", Version: "0.1.0"}, nil)
	calls := loop.Runtime()
	register(server, &Server{Catalog: cat, Calls: &calls}, Options{})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err = server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "0.1.0"}, nil).Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, session.Close()) })

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "capabilities_invoke",
		Arguments: map[string]any{
			"operation_id": "orders.create",
			"preview":      true,
			"token":        "invoke-user-token",
			"params": map[string]any{
				"body": map[string]any{"name": "ada", "password": "body-secret"},
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.False(t, res.IsError)
	text := toolText(t, res)
	assert.Contains(t, text, `"decision":"allow"`)
	assert.Contains(t, text, `"approval_required":false`)
	assert.Contains(t, text, "ada")
	assert.NotContains(t, text, "body-secret")
	assert.NotContains(t, text, secret)
	assert.NotContains(t, text, "preview-access-token")
	assert.NotContains(t, text, "invoke-user-token")
	assert.NotContains(t, text, tokenSrv.URL)
	assert.Equal(t, int32(0), tokenHits.Load())
	assert.Equal(t, int32(0), upstreamHits.Load())

	_, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name: "capabilities_invoke",
		Arguments: map[string]any{
			"operation_id": "orders.create",
			"params": map[string]any{
				"body": map[string]any{"name": "ada"},
			},
		},
	})
	require.NoError(t, err)
	assert.Greater(t, tokenHits.Load(), int32(0))
	assert.Greater(t, upstreamHits.Load(), int32(0))
}

func toolText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var buf []byte
	for _, c := range res.Content {
		text, ok := c.(*mcp.TextContent)
		require.True(t, ok)
		buf = append(buf, text.Text...)
	}
	return string(buf)
}
