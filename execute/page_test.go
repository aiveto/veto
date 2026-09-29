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
)

func TestPageFollowCollectsAndDefaultStaysOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.yaml")
	if err := os.WriteFile(path, []byte(pageSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := openapi.Load(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	op := cat.ByID("assets.list")
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
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 || one.Body != `{"items":[{"id":"1"}],"next":"b"}` {
		t.Fatalf("one page hits=%d body=%s", hits.Load(), one.Body)
	}

	hits.Store(0)
	many, err := execute.Client{BaseURL: ts.URL, FollowPages: 5}.InvokeHTTPResult(context.Background(), op, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 || many.Body != `[{"id":"1"},{"id":"2"}]` {
		t.Fatalf("follow hits=%d body=%s", hits.Load(), many.Body)
	}
}

const pageSpec = `openapi: 3.0.3
info:
  title: Assets
  version: "1"
servers:
  - url: http://127.0.0.1:9
paths:
  /assets:
    get:
      operationId: assets.list
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
              operationId: assets.list
              parameters:
                cursor: $response.body#/next
`
