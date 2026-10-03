package opa

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/result"
	"github.com/aiveto/veto/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const confirmRego = `package veto

import rego.v1

default decision := "confirmation"
default reason := ""

decision := "deny" if {
	input.operation == "orders.blocked"
}
`

func TestConfirmationDoesNotGrantAMissingPermission(t *testing.T) {
	path := writeRego(t, confirmRego)
	empty := policy.Builtin{Allow: map[string]bool{}}
	eng, err := New(context.Background(), path, "", empty)
	require.NoError(t, err)

	op := &catalog.Operation{
		ID:           "orders.get",
		Method:       http.MethodGet,
		PathTemplate: "/orders/{id}",
		Permissions:  []string{"orders.read"},
		Params:       []catalog.Param{{Name: "id", In: "path", Required: true}},
	}
	ctx := policy.WithInput(context.Background(), policy.Input{Params: map[string]string{"id": "123"}, Caller: "ada"})
	got, err := eng.Check(ctx, op)
	require.NoError(t, err)
	assert.Equal(t, policy.DecisionDeny, got)

	var hits atomic.Int32
	cat := &catalog.Catalog{Operations: []catalog.Operation{*op}}
	cat.Finalize()
	args := runtime.FromStrings(map[string]string{"id": "123"})
	rt := runtime.Runtime{
		Catalog: cat,
		Policy:  eng,
		Base:    eng,
		State:   policy.NewState(),
		Exec:    hitExec{hits: &hits},
	}
	first, err := rt.Invoke(context.Background(), runtime.Request{Operation: "orders.get", Arguments: args, Caller: "ada"})
	require.NoError(t, err)
	assert.Equal(t, "denied", first.Status)
	assert.Equal(t, int32(0), hits.Load())

	pending, err := rt.State.RequestFor(t.Context(), "ada", "orders.get", map[string]string{"id": "123"})
	require.NoError(t, err)
	approved, err := rt.State.Approve(t.Context(), pending)
	require.NoError(t, err)
	second, err := rt.Invoke(context.Background(), runtime.Request{
		Operation: "orders.get",
		Arguments: args,
		Caller:    "ada",
		Approval:  approved,
	})
	require.NoError(t, err)
	assert.Equal(t, "denied", second.Status)
	assert.Equal(t, int32(0), hits.Load())

	rt.Base = empty
	third, err := rt.Invoke(context.Background(), runtime.Request{Operation: "orders.get", Arguments: args, Caller: "ada"})
	require.NoError(t, err)
	assert.Equal(t, "denied", third.Status)
	assert.Equal(t, int32(0), hits.Load())
}

func TestGrantedPermissionStillConfirmsOnce(t *testing.T) {
	path := writeRego(t, confirmRego)
	allow := policy.Builtin{Allow: map[string]bool{"orders.read": true}}
	eng, err := New(context.Background(), path, "", allow)
	require.NoError(t, err)
	var hits atomic.Int32
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID:           "orders.get",
		Method:       http.MethodGet,
		PathTemplate: "/orders/{id}",
		Permissions:  []string{"orders.read"},
		Params:       []catalog.Param{{Name: "id", In: "path", Required: true}},
	}}}
	cat.Finalize()
	args := runtime.FromStrings(map[string]string{"id": "123"})
	rt := runtime.Runtime{
		Catalog: cat,
		Policy:  eng,
		Base:    allow,
		State:   policy.NewState(),
		Exec:    hitExec{hits: &hits},
	}
	pending, err := rt.Invoke(context.Background(), runtime.Request{Operation: "orders.get", Arguments: args, Caller: "ada"})
	require.NoError(t, err)
	assert.Equal(t, "confirmation_required", pending.Status)
	assert.Equal(t, int32(0), hits.Load())
	approved, err := rt.State.Approve(t.Context(), pending.ApprovalID)
	require.NoError(t, err)
	sent, err := rt.Invoke(context.Background(), runtime.Request{
		Operation: "orders.get",
		Arguments: args,
		Caller:    "ada",
		Approval:  approved,
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", sent.Status)
	assert.Equal(t, int32(1), hits.Load())
	_, err = rt.Invoke(context.Background(), runtime.Request{
		Operation: "orders.get",
		Arguments: args,
		Caller:    "ada",
		Approval:  approved,
	})
	require.Error(t, err)
	assert.Equal(t, int32(1), hits.Load())
}

type hitExec struct{ hits *atomic.Int32 }

func (e hitExec) InvokeHTTPResult(context.Context, *catalog.Operation, map[string]string) (result.HTTPResult, error) {
	e.hits.Add(1)
	return result.HTTPResult{Status: http.StatusNoContent, Code: "ok"}, nil
}
