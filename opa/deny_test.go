package opa

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const denyRego = `package veto

import rego.v1

default decision := "allow"
default reason := ""

decision := "deny" if {
	input.method == "DELETE"
	input.path == "/orders/{id}"
	input.side_effect == "destructive"
	input.resource_group == "orders"
	input.caller == "ada"
	input.environment == "prod"
	input.operation == "orders.delete"
	input.auth_scheme == ["bearerAuth"]
	input.permissions == ["orders.delete"]
	input.tags == ["orders"]
	input.params.id == "123"
}

decision := "deny" if {
	input.caller == "mallory"
}
`

func TestMethodOrCallerDenySkipsHTTP(t *testing.T) {
	path := writeRego(t, denyRego)
	eng, err := New(context.Background(), path, "", policy.Builtin{})
	require.NoError(t, err)
	eng.Environment = "prod"
	eng.Principal = "from-config"

	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(ts.Close)

	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{
			ID:                   "orders.delete",
			Method:               http.MethodDelete,
			PathTemplate:         "/orders/{id}",
			SideEffect:           catalog.SideEffectDestructive,
			Permissions:          []string{"orders.delete"},
			Group:                "orders",
			Tags:                 []string{"orders"},
			Auth:                 []catalog.Auth{{Name: "bearerAuth", Kind: "bearer"}},
			RequiresConfirmation: true,
			Params:               []catalog.Param{{Name: "id", In: "path", Required: true}},
		},
		{
			ID:           "orders.list",
			Method:       http.MethodGet,
			PathTemplate: "/orders",
			SideEffect:   catalog.SideEffectNone,
			Group:        "orders",
		},
		{
			ID:                   "orders.get",
			Method:               http.MethodGet,
			PathTemplate:         "/orders/{id}",
			SideEffect:           catalog.SideEffectNone,
			Group:                "orders",
			RequiresConfirmation: true,
			Params:               []catalog.Param{{Name: "id", In: "path", Required: true}},
		},
	}}
	cat.Finalize()
	rt := runtime.Runtime{
		Catalog: cat,
		Policy:  eng,
		State:   policy.NewState(),
		Exec:    execute.Client{BaseURL: ts.URL},
	}

	denied, err := rt.Invoke(context.Background(), runtime.Request{
		Operation: "orders.delete",
		Arguments: runtime.FromStrings(map[string]string{"id": "123"}),
		Caller:    "ada",
	})
	require.NoError(t, err)
	assert.Equal(t, "denied", denied.Status)
	assert.Equal(t, int32(0), hits.Load())

	mallory, err := rt.Invoke(context.Background(), runtime.Request{
		Operation: "orders.list",
		Caller:    "mallory",
	})
	require.NoError(t, err)
	assert.Equal(t, "denied", mallory.Status)
	assert.Equal(t, int32(0), hits.Load())

	pending, err := rt.Invoke(context.Background(), runtime.Request{
		Operation: "orders.get",
		Arguments: runtime.FromStrings(map[string]string{"id": "123"}),
		Caller:    "ada",
	})
	require.NoError(t, err)
	assert.Equal(t, "confirmation_required", pending.Status)
	assert.Equal(t, int32(0), hits.Load())

	again := runtime.Request{
		Operation: "orders.get",
		Arguments: runtime.FromStrings(map[string]string{"id": "123"}),
		Caller:    "ada",
		Approval:  pending.ApprovalID,
	}
	_, err = rt.Invoke(context.Background(), again)
	require.Error(t, err)
	assert.Equal(t, int32(0), hits.Load())

	approved, err := rt.State.Approve(t.Context(), pending.ApprovalID)
	require.NoError(t, err)
	again.Approval = approved
	sent, err := rt.Invoke(context.Background(), again)
	require.NoError(t, err)
	assert.Equal(t, "ok", sent.Status)
	assert.Equal(t, int32(1), hits.Load())
}

func writeRego(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.rego")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o600))
	return path
}
