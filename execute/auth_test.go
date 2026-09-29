package execute_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBearerHeaderIsSentAndKeptOffTheSpan(t *testing.T) {
	cat := loadSpec(t, bearerSpec)
	op := cat.ByID("assets.get")
	const secret = "s3cret-token"
	var got string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	rec, err := telemetry.Record()
	require.NoError(t, err)
	defer rec.Stop(context.Background())
	_, err = execute.Client{BaseURL: ts.URL, Auth: map[string]string{"bearerAuth": secret}}.InvokeHTTPResult(context.Background(), op, map[string]string{"id": "1"})
	require.NoError(t, err)
	assert.Equal(t, "Bearer "+secret, got)
	text := spanText(t, rec)
	assert.NotContains(t, text, secret)
	assert.NotContains(t, text, "Authorization")
}

func TestNoSchemeSendsNoAuthorization(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	require.NoError(t, err)
	op := cat.ByID("assets.get")
	require.Empty(t, op.Auth)
	var got string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	_, err = (execute.Client{BaseURL: ts.URL, Auth: map[string]string{"bearerAuth": "nope"}}).InvokeHTTPResult(context.Background(), op, map[string]string{"id": "1"})
	require.NoError(t, err)
	assert.Empty(t, got)
}

const bearerSpec = `openapi: 3.0.3
info:
  title: Assets
  version: "1"
servers:
  - url: http://127.0.0.1:9
security:
  - bearerAuth: []
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
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
`
