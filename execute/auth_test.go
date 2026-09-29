package execute_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/telemetry"
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
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Stop(context.Background())
	_, err = execute.Client{BaseURL: ts.URL, Auth: map[string]string{"bearerAuth": secret}}.Invoke(context.Background(), op, map[string]string{"id": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "Bearer "+secret {
		t.Fatalf("authorization: %q", got)
	}
	text := spanText(t, rec)
	if strings.Contains(text, secret) || strings.Contains(text, "Authorization") {
		t.Fatalf("auth leaked onto the span:\n%s", text)
	}
}

func TestUnsetBearerDoesNotCallDo(t *testing.T) {
	cat := loadSpec(t, bearerSpec)
	op := cat.ByID("assets.get")
	trip := &failTrip{}
	_, err := execute.Invoke(context.Background(), execute.Config{
		BaseURL: "http://127.0.0.1:9",
		Client:  &http.Client{Transport: trip},
	}, op, map[string]string{"id": "1"})
	if err == nil || !strings.Contains(err.Error(), "bearerAuth is unset") {
		t.Fatalf("err: %v", err)
	}
	if trip.called {
		t.Fatal("Do was called")
	}
}

func TestNoSchemeSendsNoAuthorization(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	op := cat.ByID("assets.get")
	if len(op.Auth) != 0 {
		t.Fatalf("auth: %+v", op.Auth)
	}
	var got string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	if _, err := (execute.Client{BaseURL: ts.URL, Auth: map[string]string{"bearerAuth": "nope"}}).Invoke(context.Background(), op, map[string]string{"id": "1"}); err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("authorization: %q", got)
	}
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
