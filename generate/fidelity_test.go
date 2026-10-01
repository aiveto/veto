package generate_test

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/generate"
	"github.com/stretchr/testify/require"
)

func TestGeneratedClientKeepsExposureAuthAndIdempotency(t *testing.T) {
	bearer := catalog.Auth{Name: "bearerAuth", Header: "Authorization", Kind: "bearer"}
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{ID: "orders.hidden", Method: http.MethodGet, PathTemplate: "/hidden", Exposure: catalog.ExposureDiscovery},
		{ID: "orders.secure", Method: http.MethodGet, PathTemplate: "/secure", Auth: []catalog.Auth{bearer}, Requirements: [][]catalog.Auth{{bearer}}},
		{
			ID: "orders.delete", Method: http.MethodDelete, PathTemplate: "/orders/{id}",
			RequiresConfirmation: true, Permissions: []string{"orders.delete"},
			Params: []catalog.Param{{Name: "id", In: "path", Required: true}},
		},
		{ID: "orders.create", Method: http.MethodPost, PathTemplate: "/orders", Idempotency: "key", Retry: "1"},
	}}
	cat.Finalize()
	dir := t.TempDir()
	const module = "example.com/fidelity"
	require.NoError(t, generate.Write(dir, module, cat))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sdk", "fidelity_test.go"), []byte(fidelityProbe), 0o600))
	root, err := filepath.Abs("..")
	require.NoError(t, err)
	replace := exec.CommandContext(t.Context(), "go", "mod", "edit", "-replace", "github.com/aiveto/veto="+root)
	replace.Dir = dir
	out, err := replace.CombinedOutput()
	require.NoError(t, err, string(out))
	tidy := exec.CommandContext(t.Context(), "go", "mod", "tidy")
	tidy.Dir = dir
	out, err = tidy.CombinedOutput()
	require.NoError(t, err, string(out))
	run := exec.CommandContext(t.Context(), "go", "test", "-count=1", "./sdk")
	run.Dir = dir
	out, err = run.CombinedOutput()
	require.NoError(t, err, string(out))
}

const fidelityProbe = `package sdk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/policy"
)

func TestGeneratedCatalogMatchesTheRuntime(t *testing.T) {
	var posts atomic.Int32
	var other atomic.Int32
	var missingKey atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/orders" {
			n := posts.Add(1)
			if r.Header.Get("Idempotency-Key") == "" {
				missingKey.Store(true)
			}
			if n == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		other.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c, err := New(srv.URL, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	hidden := c.Calls.Catalog.ByID("orders.hidden")
	if hidden == nil || hidden.Exposure != "discovery-only" {
		t.Fatalf("hidden %+v", hidden)
	}
	secure := c.Calls.Catalog.ByID("orders.secure")
	if secure == nil || len(secure.Auth) != 1 || secure.Auth[0].Name != "bearerAuth" || secure.Auth[0].Kind != "bearer" || secure.Auth[0].Header != "Authorization" {
		t.Fatalf("auth %+v", secure)
	}
	if len(secure.Requirements) != 1 || len(secure.Requirements[0]) != 1 || secure.Requirements[0][0].Name != "bearerAuth" {
		t.Fatalf("requirements %+v", secure.Requirements)
	}
	create := c.Calls.Catalog.ByID("orders.create")
	if create == nil || create.Idempotency != "key" || create.Retry != "1" {
		t.Fatalf("create %+v", create)
	}
	del := c.Calls.Catalog.ByID("orders.delete")
	if del == nil || !del.RequiresConfirmation || len(del.Permissions) != 1 || del.Permissions[0] != "orders.delete" {
		t.Fatalf("delete %+v", del)
	}

	got, err := c.OrdersHidden(context.Background(), "")
	if err == nil || got.Code != "not_callable" {
		t.Fatalf("hidden status %s err %v", got.Status, err)
	}
	if other.Load() != 0 || posts.Load() != 0 {
		t.Fatalf("discovery called upstream posts=%d other=%d", posts.Load(), other.Load())
	}

	got, err = c.OrdersSecure(context.Background(), "")
	if err == nil {
		t.Fatalf("secure status %s", got.Status)
	}
	if other.Load() != 0 || posts.Load() != 0 {
		t.Fatalf("missing credential called upstream")
	}

	got, err = c.OrdersDelete(context.Background(), "123", "")
	if err != nil || got.Status != "confirmation_required" {
		t.Fatalf("confirm status %s err %v", got.Status, err)
	}
	if other.Load() != 0 || posts.Load() != 0 {
		t.Fatalf("confirmation called upstream")
	}

	c.Calls.Policy = policy.Builtin{Allow: map[string]bool{}}
	got, err = c.OrdersDelete(context.Background(), "123", "")
	if err != nil || got.Status != "denied" {
		t.Fatalf("deny status %s err %v", got.Status, err)
	}
	if other.Load() != 0 || posts.Load() != 0 {
		t.Fatalf("denied permission called upstream")
	}

	c.Calls.Policy = policy.Builtin{}
	got, err = c.OrdersCreate(context.Background(), "")
	if err != nil || got.Status != "ok" {
		t.Fatalf("create status %s err %v", got.Status, err)
	}
	if posts.Load() != 2 || missingKey.Load() {
		t.Fatalf("posts %d missing key %v", posts.Load(), missingKey.Load())
	}
	if other.Load() != 0 {
		t.Fatalf("unexpected calls %d", other.Load())
	}
}
`
