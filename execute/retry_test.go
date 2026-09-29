package execute_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
)

func TestIdempotencyKeyAndRetryBound(t *testing.T) {
	var hits atomic.Int32
	var key string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if key == "" {
			key = r.Header.Get("Idempotency-Key")
		} else if r.Header.Get("Idempotency-Key") != key {
			t.Errorf("key changed: %q", r.Header.Get("Idempotency-Key"))
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer ts.Close()
	op := &catalog.Operation{
		ID: "assets.create", Method: http.MethodPost, PathTemplate: "/assets",
		Idempotency: "key", Retry: "2",
		Params: []catalog.Param{{Name: "body", In: "body", Required: true}},
	}
	resp, err := execute.Invoke(context.Background(), execute.Config{BaseURL: ts.URL}, op, map[string]string{"body": `{"name":"a"}`})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if hits.Load() != 3 || key == "" {
		t.Fatalf("hits=%d key=%q", hits.Load(), key)
	}
}

func TestRetryNeverAndDestructiveWithoutKeyDoNotRetry(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Idempotency-Key") != "" {
			t.Errorf("unexpected key")
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer ts.Close()

	get := &catalog.Operation{ID: "assets.get", Method: http.MethodGet, PathTemplate: "/assets", Retry: "never"}
	resp, err := execute.Invoke(context.Background(), execute.Config{BaseURL: ts.URL}, get, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if hits.Load() != 1 {
		t.Fatalf("retry never hits=%d", hits.Load())
	}

	del := &catalog.Operation{
		ID: "assets.delete", Method: http.MethodDelete, PathTemplate: "/assets/{id}", Retry: "2",
		Params: []catalog.Param{{Name: "id", In: "path", Required: true}},
	}
	resp, err = execute.Invoke(context.Background(), execute.Config{BaseURL: ts.URL}, del, map[string]string{"id": "1"})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if hits.Load() != 2 {
		t.Fatalf("destructive hits=%d", hits.Load())
	}
}
