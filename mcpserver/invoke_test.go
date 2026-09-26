package mcpserver_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/mcpserver"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/semantics"
)

func TestInvokeDeleteRequiresApprovalBeforeHTTP(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	sem := semantics.NewDerived(cat)
	srv := &mcpserver.Server{
		Catalog:   cat,
		Semantics: sem,
		Agent:     agent.New(cat, sem, execute.Client{BaseURL: ts.URL}),
	}
	ctx := context.Background()
	first, err := srv.Invoke(ctx, "assets.delete", map[string]string{"id": "123"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "confirmation_required" {
		t.Fatalf("expected confirmation_required, got %q", first.Status)
	}
	if hits.Load() != 0 {
		t.Fatalf("HTTP should not run without approval, hits=%d", hits.Load())
	}
	second, err := srv.Invoke(ctx, "assets.delete", map[string]string{"id": "123"}, first.ApprovalID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != "ok" {
		t.Fatalf("expected ok, got %q", second.Status)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected one HTTP call, hits=%d", hits.Load())
	}
}
