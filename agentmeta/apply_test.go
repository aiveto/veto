package agentmeta_test

import (
	"context"
	"testing"

	"github.com/aiveto/veto/agentmeta"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/policy"
)

func TestOverlaySetsPermissionsAndCanRequireConfirmation(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f, err := agentmeta.Load("../testdata/agent.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := agentmeta.Apply(cat, f); err != nil {
		t.Fatal(err)
	}
	del := cat.ByID("assets.delete")
	if del.Exposure != "discovery-only" || len(del.Permissions) != 1 || del.Permissions[0] != "asset.delete" {
		t.Fatalf("delete overlay: %+v", del)
	}
	if !del.RequiresConfirmation {
		t.Fatal("delete still requires confirmation when the file is silent")
	}

	yes := true
	no := false
	destructive := "destructive"
	read := cat.ByID("assets.get")
	if err := agentmeta.Apply(cat, agentmeta.File{Operations: []agentmeta.Entry{{
		Operation:    "assets.get",
		Confirmation: &yes,
	}}}); err != nil {
		t.Fatal(err)
	}
	d, err := policy.Builtin{}.Check(context.Background(), read)
	if err != nil || d != policy.DecisionConfirmationNeeded {
		t.Fatalf("get confirmation: %s %v", d, err)
	}
	if err := agentmeta.Apply(cat, agentmeta.File{Operations: []agentmeta.Entry{{
		Operation:    "assets.delete",
		Confirmation: &no,
	}}}); err != nil {
		t.Fatal(err)
	}
	if del.SideEffect != "destructive" {
		t.Fatalf("side effect changed: %s", del.SideEffect)
	}
	d, err = policy.Builtin{}.Check(context.Background(), del)
	if err != nil || d != policy.DecisionAllow {
		t.Fatalf("confirmation false still stopped the delete: %s %v", d, err)
	}

	list := cat.ByID("assets.list")
	if err := agentmeta.Apply(cat, agentmeta.File{Operations: []agentmeta.Entry{{
		Operation:  "assets.list",
		SideEffect: &destructive,
	}}}); err != nil {
		t.Fatal(err)
	}
	d, err = policy.Builtin{}.Check(context.Background(), list)
	if err != nil || d != policy.DecisionConfirmationNeeded {
		t.Fatalf("destructive side effect did not set confirmation: %s %v", d, err)
	}
}

func TestUnknownOperationRefused(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	err = agentmeta.Apply(cat, agentmeta.File{Operations: []agentmeta.Entry{{Operation: "missing"}}})
	if err == nil {
		t.Fatal("expected unknown operation")
	}
}
