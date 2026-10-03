package openapi_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiveto/veto/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadRejectsIncompleteDocuments(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "unknown operation id", body: specWithLink("operationId: orders.missing"), want: "orders.missing"},
		{name: "external ref", body: specWithLink(`operationRef: "https://example.com/spec.yaml#/paths/~1orders~1{id}/get"`), want: "outside this document"},
		{name: "ref is not an operation", body: specWithLink(`operationRef: "#/components/schemas/Order"`), want: "not a path operation"},
		{name: "webhooks", body: webhookSpec, want: "webhooks"},
		{name: "callbacks", body: callbackSpec, want: "callbacks"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "spec.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.body), 0o600))
			_, err := openapi.Load(context.Background(), path)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestLoadReadsAnHTTPContract(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(openAPI31))
	}))
	defer srv.Close()
	cat, err := openapi.Load(context.Background(), srv.URL)
	require.NoError(t, err)
	require.NotNil(t, cat.ByID("ping"))
	assert.Equal(t, 1, hits)

	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer missing.Close()
	_, err = openapi.Load(context.Background(), missing.URL)
	require.ErrorContains(t, err, "404")

	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(w, strings.NewReader(strings.Repeat("a", 8<<20+1)))
	}))
	defer huge.Close()
	_, err = openapi.Load(context.Background(), huge.URL)
	assert.ErrorContains(t, err, "larger than")
}

func TestOpenAPI31DocumentLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ping.yaml")
	require.NoError(t, os.WriteFile(path, []byte(openAPI31), 0o600))
	cat, err := openapi.Load(context.Background(), path)
	require.NoError(t, err)
	require.NotNil(t, cat.ByID("ping"))
}

const webhookSpec = `openapi: 3.0.3
info:
  title: t
  version: "1"
paths:
  /ping:
    get:
      operationId: ping
      responses:
        "200":
          description: ok
webhooks:
  onEvent:
    post:
      operationId: onEvent
      responses:
        "200":
          description: ok
`

const callbackSpec = `openapi: 3.0.3
info:
  title: t
  version: "1"
paths:
  /orders:
    post:
      operationId: orders.create
      callbacks:
        onEvent:
          "https://example.com/hook":
            post:
              responses:
                "200":
                  description: ok
      responses:
        "200":
          description: ok
`

const openAPI31 = `openapi: 3.1.0
info:
  title: t
  version: "1"
paths:
  /ping:
    get:
      operationId: ping
      responses:
        "200":
          description: ok
`

func specWithLink(link string) string {
	return fmt.Sprintf(`openapi: 3.0.3
info:
  title: t
  version: "1"
paths:
  /orders/{id}:
    get:
      operationId: orders.get
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
          links:
            next:
              %s
`, link)
}
