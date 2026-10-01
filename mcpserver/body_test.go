package mcpserver

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/openapi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvokeSendsObjectOrStringBody(t *testing.T) {
	cases := []struct {
		name string
		body any
		want string
	}{
		{name: "object", body: map[string]any{"name": "ada"}, want: `{"name":"ada"}`},
		{name: "string", body: `{"name":"ada"}`, want: `{"name":"ada"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody, gotType string
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				gotBody = string(b)
				gotType = r.Header.Get("Content-Type")
				w.WriteHeader(http.StatusCreated)
			}))
			defer ts.Close()

			path := filepath.Join(t.TempDir(), "spec.yaml")
			require.NoError(t, os.WriteFile(path, []byte(versionBodySpec), 0o644))
			cat, err := openapi.Load(context.Background(), path)
			require.NoError(t, err)
			loop, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
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
			defer session.Close()

			_, err = session.CallTool(ctx, &mcp.CallToolParams{
				Name: "capabilities_invoke",
				Arguments: map[string]any{
					"operation_id": "customers.create",
					"params":       map[string]any{"body": tc.body},
				},
			})
			require.NoError(t, err)
			assert.Equal(t, tc.want, gotBody)
			assert.Equal(t, "application/json;v=3", gotType)
		})
	}
}

const versionBodySpec = `openapi: 3.0.3
info:
  title: Customers
  version: "3"
servers:
  - url: http://127.0.0.1:9
paths:
  /customers:
    post:
      operationId: customers.create
      requestBody:
        required: true
        content:
          "application/json;v=3":
            schema:
              type: object
      responses:
        "201":
          description: created
`
