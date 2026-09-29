package openapi_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBearerSchemeAndExplicitOptOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(authSpec), 0o644))
	cat, err := openapi.Load(context.Background(), path)
	require.NoError(t, err)
	create := cat.ByID("assets.create")
	require.NotNil(t, create)
	require.Len(t, create.Auth, 1)
	assert.Equal(t, "bearerAuth", create.Auth[0].Name)
	assert.Equal(t, "bearer", create.Auth[0].Kind)
	assert.Equal(t, "Authorization", create.Auth[0].Header)
	health := cat.ByID("assets.health")
	require.NotNil(t, health)
	assert.Empty(t, health.Auth)
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
