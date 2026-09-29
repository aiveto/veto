package openapi_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiveto/veto/openapi"
)

func TestUnresolvedLinkFailsLoad(t *testing.T) {
	dir := t.TempDir()
	badID := filepath.Join(dir, "bad-id.yaml")
	if err := os.WriteFile(badID, []byte(specWithLink("operationId: assets.missing")), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := openapi.Load(context.Background(), badID)
	if err == nil || !strings.Contains(err.Error(), "assets.missing") {
		t.Fatalf("operationId: %v", err)
	}

	external := filepath.Join(dir, "external.yaml")
	ref := `operationRef: "https://example.com/spec.yaml#/paths/~1assets~1{id}/get"`
	if err := os.WriteFile(external, []byte(specWithLink(ref)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = openapi.Load(context.Background(), external)
	if err == nil || !strings.Contains(err.Error(), "outside this document") {
		t.Fatalf("external ref: %v", err)
	}

	shape := filepath.Join(dir, "shape.yaml")
	if err := os.WriteFile(shape, []byte(specWithLink(`operationRef: "#/components/schemas/Holding"`)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = openapi.Load(context.Background(), shape)
	if err == nil || !strings.Contains(err.Error(), "not a path operation") {
		t.Fatalf("shape: %v", err)
	}
}

func TestWebhooksAndCallbacksFailLoad(t *testing.T) {
	dir := t.TempDir()
	webhook := filepath.Join(dir, "webhook.yaml")
	if err := os.WriteFile(webhook, []byte(webhookSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := openapi.Load(context.Background(), webhook)
	if err == nil || !strings.Contains(err.Error(), "webhooks") {
		t.Fatalf("webhooks: %v", err)
	}

	callback := filepath.Join(dir, "callback.yaml")
	if err := os.WriteFile(callback, []byte(callbackSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = openapi.Load(context.Background(), callback)
	if err == nil || !strings.Contains(err.Error(), "callbacks") {
		t.Fatalf("callbacks: %v", err)
	}
}

func TestOpenAPI31DocumentLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ping.yaml")
	if err := os.WriteFile(path, []byte(openAPI31), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := openapi.Load(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if cat.ByID("ping") == nil {
		t.Fatalf("operations: %s", cat.IndexLine())
	}
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
  /assets:
    post:
      operationId: assets.create
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
	return `openapi: 3.0.3
info:
  title: t
  version: "1"
paths:
  /assets/{id}:
    get:
      operationId: assets.get
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
              ` + link + "\n"
}
