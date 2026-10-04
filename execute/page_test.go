package execute_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvokeSendsOnePage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(pageSpec), 0o600))
	cat, err := openapi.Load(context.Background(), path)
	require.NoError(t, err)
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"items":[{"id":"1"}],"next":"b"}`))
	}))
	defer ts.Close()
	got, err := execute.Client{BaseURL: ts.URL}.InvokeHTTPResult(context.Background(), cat.ByID("orders.list"), nil)
	require.NoError(t, err)
	assert.Equal(t, int32(1), hits.Load())
	assert.JSONEq(t, `{"items":[{"id":"1"}],"next":"b"}`, got.Body)
}

const pageSpec = `openapi: 3.0.3
info:
  title: Orders
  version: "1"
servers:
  - url: http://127.0.0.1:9
paths:
  /orders:
    get:
      operationId: orders.list
      parameters:
        - name: cursor
          in: query
          schema:
            type: string
      responses:
        "200":
          description: page
          content:
            application/json:
              schema:
                type: object
                properties:
                  items:
                    type: array
                    items:
                      type: object
                  next:
                    type: string
          links:
            next:
              operationId: orders.list
              parameters:
                cursor: $response.body#/next
`
