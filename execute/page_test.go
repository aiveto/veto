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

func TestPageFollowCollectsAndDefaultStaysOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(pageSpec), 0o644))
	cat, err := openapi.Load(context.Background(), path)
	require.NoError(t, err)
	op := cat.ByID("orders.list")
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("cursor") == "b" {
			_, _ = w.Write([]byte(`{"items":[{"id":"2"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"1"}],"next":"b"}`))
	}))
	defer ts.Close()

	one, err := execute.Client{BaseURL: ts.URL}.InvokeHTTPResult(context.Background(), op, nil)
	require.NoError(t, err)
	assert.Equal(t, int32(1), hits.Load())
	assert.Equal(t, `{"items":[{"id":"1"}],"next":"b"}`, one.Body)

	hits.Store(0)
	many, err := execute.Client{BaseURL: ts.URL, FollowPages: 5}.InvokeHTTPResult(context.Background(), op, nil)
	require.NoError(t, err)
	assert.Equal(t, int32(2), hits.Load())
	assert.Equal(t, `[{"id":"1"},{"id":"2"}]`, many.Body)
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
