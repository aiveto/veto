package execute_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/openapi"
)

func TestJSONBodySetsContentHeaders(t *testing.T) {
	cat := loadSpec(t, bodySpec)
	op := cat.ByID("assets.create")
	body, ok := op.BodyParam()
	if !ok || body.In != "body" || body.Name != "body" || !body.Required {
		t.Fatalf("body param: %+v ok=%v", body, ok)
	}
	if strings.Contains(body.Schema, "$ref") || !strings.Contains(body.Schema, `"name"`) {
		t.Fatalf("schema: %s", body.Schema)
	}

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

	resp, err := execute.Invoke(context.Background(), execute.Config{BaseURL: ts.URL}, op, map[string]string{"body": raw})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotBody != raw || gotType != "application/json" || gotCL != int64(len(raw)) || gotLen != "14" {
		t.Fatalf("body=%q type=%q len=%q cl=%d", gotBody, gotType, gotLen, gotCL)
	}
}

func TestRequiredEmptyBodyDoesNotCallDo(t *testing.T) {
	cat := loadSpec(t, bodySpec)
	op := cat.ByID("assets.create")
	trip := &failTrip{}
	client := &http.Client{Transport: trip}
	_, err := execute.Invoke(context.Background(), execute.Config{BaseURL: "http://127.0.0.1:9", Client: client}, op, map[string]string{"_body": `{"name":"kit"}`})
	if err == nil || !strings.Contains(err.Error(), "body required") {
		t.Fatalf("err: %v", err)
	}
	if trip.called {
		t.Fatal("Do was called")
	}
}

func TestNoBodySchemaOmitsContentType(t *testing.T) {
	cat := loadSpec(t, bodySpec)
	op := cat.ByID("assets.get")
	if _, ok := op.BodyParam(); ok {
		t.Fatal("get should not have a body param")
	}
	var gotType string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	resp, err := execute.Invoke(context.Background(), execute.Config{BaseURL: ts.URL}, op, map[string]string{"id": "1"})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotType != "" {
		t.Fatalf("content-type %q", gotType)
	}
}

func TestEmptyPathParamDoesNotCallDo(t *testing.T) {
	cat := loadSpec(t, bodySpec)
	op := cat.ByID("assets.get")
	trip := &failTrip{}
	_, err := execute.Invoke(context.Background(), execute.Config{
		BaseURL: "http://127.0.0.1:9",
		Client:  &http.Client{Transport: trip},
	}, op, map[string]string{"id": "  "})
	if err == nil || !strings.Contains(err.Error(), "id required") {
		t.Fatalf("err: %v", err)
	}
	if trip.called {
		t.Fatal("Do was called")
	}
}

type failTrip struct{ called bool }

func (f *failTrip) RoundTrip(*http.Request) (*http.Response, error) {
	f.called = true
	return nil, http.ErrAbortHandler
}

func loadSpec(t *testing.T, spec string) *catalog.Catalog {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	if err := os.WriteFile(path, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := openapi.Load(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
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
