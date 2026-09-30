package openapi_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBearerSchemeAndExplicitOptOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(authSpec), 0o644))
	cat, err := openapi.Load(context.Background(), path)
	require.NoError(t, err)
	create := cat.ByID("orders.create")
	require.NotNil(t, create)
	require.Len(t, create.Auth, 1)
	assert.Equal(t, "bearerAuth", create.Auth[0].Name)
	assert.Equal(t, "bearer", create.Auth[0].Kind)
	assert.Equal(t, "Authorization", create.Auth[0].Header)
	health := cat.ByID("orders.health")
	require.NotNil(t, health)
	assert.Empty(t, health.Auth)
	assert.Empty(t, health.Requirements)
}

func TestAPIKeyAndOAuthSchemes(t *testing.T) {
	cat := loadAuth(t, schemeSpec)
	both := cat.ByID("orders.create")
	require.Len(t, both.Auth, 2)
	assert.Equal(t, "apiKey", both.Auth[0].Kind)
	assert.Equal(t, "X-Api-Key", both.Auth[0].Header)
	assert.Equal(t, "oauth2", both.Auth[1].Kind)
	assert.Equal(t, "Authorization", both.Auth[1].Header)
	assert.Equal(t, []string{"orders.read"}, both.Auth[1].Scopes)

	query := cat.ByID("orders.search")
	require.Len(t, query.Auth, 1)
	assert.Equal(t, "apiKey", query.Auth[0].Kind)
	assert.Equal(t, "api_key", query.Auth[0].Query)
	assert.Empty(t, query.Auth[0].Header)

	either := cat.ByID("orders.either")
	require.Len(t, either.Requirements, 2)
	assert.Equal(t, "userAuth", either.Requirements[0][0].Name)
	assert.Equal(t, "apiAuth", either.Requirements[1][0].Name)

	listed := cat.ByID("approvals.list")
	require.Len(t, listed.Auth, 1)
	assert.Equal(t, "X-User-Token", listed.Auth[0].UserHeader)
}

func loadAuth(t *testing.T, spec string) *catalog.Catalog {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(spec), 0o644))
	cat, err := openapi.Load(context.Background(), path)
	require.NoError(t, err)
	return cat
}

const authSpec = `openapi: 3.0.3
info:
  title: Orders
  version: "1"
servers:
  - url: http://127.0.0.1:8080
security:
  - bearerAuth: []
paths:
  /orders:
    post:
      operationId: orders.create
      responses:
        "201":
          description: created
  /health:
    get:
      operationId: orders.health
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

const schemeSpec = `openapi: 3.0.3
info:
  title: Orders
  version: "1"
servers:
  - url: http://127.0.0.1:9
paths:
  /orders:
    post:
      operationId: orders.create
      security:
        - apiAuth: []
          userAuth: [orders.read]
      responses:
        "201":
          description: created
  /orders/search:
    get:
      operationId: orders.search
      security:
        - queryAuth: []
      responses:
        "200":
          description: ok
  /orders/either:
    get:
      operationId: orders.either
      security:
        - userAuth: []
        - apiAuth: []
      responses:
        "200":
          description: ok
  /approvals:
    get:
      operationId: approvals.list
      security:
        - userAuth: []
      responses:
        "200":
          description: ok
components:
  securitySchemes:
    apiAuth:
      type: apiKey
      in: header
      name: X-Api-Key
    queryAuth:
      type: apiKey
      in: query
      name: api_key
    userAuth:
      type: oauth2
      x-user-token-header: X-User-Token
      flows:
        authorizationCode:
          authorizationUrl: https://idp.example/authorize
          tokenUrl: https://idp.example/oauth/token
          scopes:
            orders.read: read orders
`
