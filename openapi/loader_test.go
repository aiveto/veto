package openapi_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
			require.NoError(t, os.WriteFile(path, []byte(tc.body), 0o644))
			_, err := openapi.Load(context.Background(), path)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestOpenAPI31DocumentLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ping.yaml")
	require.NoError(t, os.WriteFile(path, []byte(openAPI31), 0o644))
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
