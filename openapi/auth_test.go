package openapi_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/openapi"
)

func TestBearerSchemeAndExplicitOptOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.yaml")
	if err := os.WriteFile(path, []byte(authSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := openapi.Load(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	create := cat.ByID("assets.create")
	if create == nil || len(create.Auth) != 1 || create.Auth[0].Name != "bearerAuth" || create.Auth[0].Kind != "bearer" || create.Auth[0].Header != "Authorization" {
		t.Fatalf("create auth: %+v", create)
	}
	if got := cat.ByID("assets.health"); got == nil || len(got.Auth) != 0 {
		t.Fatalf("health auth: %+v", got)
	}
}

const authSpec = `openapi: 3.0.3
info:
  title: Assets
  version: "1"
servers:
  - url: http://127.0.0.1:8080
security:
  - bearerAuth: []
paths:
  /assets:
    post:
      operationId: assets.create
      responses:
        "201":
          description: created
  /health:
    get:
      operationId: assets.health
      security: []
      responses:
        "200":
          description: ok
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
`
