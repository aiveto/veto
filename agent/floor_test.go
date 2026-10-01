package agent

import (
	"context"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/policy"
	httpresult "github.com/aiveto/veto/result"
)

func TestAssignedPolicyKeepsThePermissionFloor(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	if err != nil {
		t.Fatal(err)
	}
	op := cat.ByID("orders.delete")
	op.Permissions = []string{"order.delete"}
	op.RequiresConfirmation = false
	var hits int
	loop, err := New(cat, nil, floorExec{hits: &hits})
	if err != nil {
		t.Fatal(err)
	}
	if loop.base == nil {
		t.Fatal("base is nil")
	}
	loop.base = policy.Builtin{Allow: map[string]bool{}}
	loop.Policy = floorAllow{}
	out, err := loop.Invoke(context.Background(), "orders.delete", map[string]string{"id": "123"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "denied" {
		t.Fatalf("status %s", out.Status)
	}
	if hits != 0 {
		t.Fatalf("hits %d", hits)
	}
}

func TestPolicyFieldStillAppliesTheFloor(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var hits int
	loop := &Loop{
		Catalog: cat,
		Policy:  floorAllow{},
		State:   policy.NewState(),
		Exec:    floorExec{hits: &hits},
	}
	out, err := loop.Invoke(context.Background(), "orders.delete", map[string]string{"id": "123"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if loop.base == nil {
		t.Fatal("base stayed nil")
	}
	if out.Status != "confirmation_required" {
		t.Fatalf("status %s", out.Status)
	}
	if hits != 0 {
		t.Fatalf("hits %d", hits)
	}
}

type floorAllow struct{}

func (floorAllow) Check(context.Context, *catalog.Operation) (policy.Decision, error) {
	return policy.DecisionAllow, nil
}

type floorExec struct{ hits *int }

func (e floorExec) InvokeHTTPResult(context.Context, *catalog.Operation, map[string]string) (httpresult.HTTPResult, error) {
	*e.hits++
	return httpresult.HTTPResult{Status: 200, Code: "ok"}, nil
}
