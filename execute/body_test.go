package execute_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONBodySetsContentHeaders(t *testing.T) {
	cat := loadSpec(t, bodySpec)
	op := cat.ByID("assets.create")
	body, ok := op.BodyParam()
	require.True(t, ok)
	assert.Equal(t, "body", body.Name)
	assert.Equal(t, "body", body.In)
	assert.True(t, body.Required)
	assert.NotContains(t, body.Schema, "$ref")
	assert.Contains(t, body.Schema, `"name"`)

	const raw = `{"name":"kit"}`
	var gotBody, gotType, gotLen string
	var gotCL int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotType = r.Header.Get("Content-Type")
		gotLen = r.Header.Get("Content-Length")
		gotCL = r.ContentLength
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	resp, err := execute.InvokeResponse(context.Background(), execute.Config{BaseURL: ts.URL}, op, map[string]string{"body": raw})
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, raw, gotBody)
	assert.Equal(t, "application/json", gotType)
	assert.Equal(t, int64(len(raw)), gotCL)
	assert.Equal(t, "14", gotLen)
}

func TestMissingRequiredInputDoesNotCallDo(t *testing.T) {
	cases := []struct {
		name   string
		spec   string
		op     string
		params map[string]string
		want   string
	}{
		{name: "unset bearer", spec: bearerSpec, op: "assets.get", params: map[string]string{"id": "1"}, want: "bearerAuth is unset"},
		{name: "empty body", spec: bodySpec, op: "assets.create", params: map[string]string{"_body": `{"name":"kit"}`}, want: "body required"},
		{name: "empty path", spec: bodySpec, op: "assets.get", params: map[string]string{"id": "  "}, want: "id required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := loadSpec(t, tc.spec).ByID(tc.op)
			require.NotNil(t, op)
			trip := &failTrip{}
			_, err := execute.InvokeResponse(context.Background(), execute.Config{
				BaseURL: "http://127.0.0.1:9",
				Client:  &http.Client{Transport: trip},
			}, op, tc.params)
			assert.ErrorContains(t, err, tc.want)
			assert.False(t, trip.called)
		})
	}
}

func TestNoBodySchemaOmitsContentType(t *testing.T) {
	cat := loadSpec(t, bodySpec)
	op := cat.ByID("assets.get")
	_, ok := op.BodyParam()
	assert.False(t, ok)
	var gotType string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	resp, err := execute.InvokeResponse(context.Background(), execute.Config{BaseURL: ts.URL}, op, map[string]string{"id": "1"})
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Empty(t, gotType)
}

type failTrip struct{ called bool }

func (f *failTrip) RoundTrip(*http.Request) (*http.Response, error) {
	f.called = true
	return nil, http.ErrAbortHandler
}

func loadSpec(t *testing.T, spec string) *catalog.Catalog {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(spec), 0o644))
	cat, err := openapi.Load(context.Background(), path)
	require.NoError(t, err)
	return cat
}

const bodySpec = `openapi: 3.0.3
info:
  title: Assets
  version: "1"
servers:
  - url: http://127.0.0.1:8080
paths:
  /assets:
    post:
      operationId: assets.create
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/Asset"
      responses:
        "201":
          description: created
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
  schemas:
    Asset:
      type: object
      required: [name]
      properties:
        name:
          type: string
`
