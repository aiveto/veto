package runctx_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/semantics"
)

func TestPackOmitsRawSpecAndIncludesDescribed(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := os.ReadFile("../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	sem := semantics.NewDerived(cat)
	op := cat.ByID("assets.delete")
	b := runctx.NewBuilder(4096)
	pack := b.Build(cat, []runctx.Turn{{Role: "user", Content: "delete 123"}}, op, sem, nil)
	ser := pack.Serialize()
	if runctx.ContainsRawSpec(ser) {
		t.Fatal("pack must not contain openapi markers")
	}
	if runctx.ContainsRawSpec(string(spec)) {
		// sanity
	} else {
		t.Fatal("fixture should look like openapi")
	}
	if pack.DescribedOperationID != "assets.delete" {
		t.Fatalf("expected described operation, got %q", pack.DescribedOperationID)
	}
}

func TestPackWalksDeclaredRelation(t *testing.T) {
	assets, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	teams, err := openapi.Load(context.Background(), "../testdata/teams.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Merge(assets, teams)
	if err != nil {
		t.Fatal(err)
	}
	rels, err := catalog.LoadRelations("../testdata/relations.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.ApplyRelations(cat, rels); err != nil {
		t.Fatal(err)
	}
	sem := semantics.NewDerived(cat)
	pack := runctx.NewBuilder(8192).Build(cat, nil, cat.ByID("assets.get"), sem, nil)
	ser := pack.Serialize()
	if runctx.ContainsRawSpec(ser) {
		t.Fatal("pack contains the spec")
	}
	if !strings.Contains(ser, "related: teams.get Holding.teamsId") {
		t.Fatalf("pack did not walk the relation:\n%s", ser)
	}
	if !strings.Contains(ser, "Holding.teamsId identifies teams.get") {
		t.Fatalf("pack missing relation sentence:\n%s", ser)
	}
}

func TestPackKeepsSearchHitsAndDropsTheRest(t *testing.T) {
	assets, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	teams, err := openapi.Load(context.Background(), "../testdata/teams.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Merge(assets, teams)
	if err != nil {
		t.Fatal(err)
	}
	cat.Operations = append(cat.Operations, catalog.Operation{
		ID: "billing.list", Description: "List invoices", Group: "billing", Name: "List invoices",
	})
	cat.Finalize()
	rels, err := catalog.LoadRelations("../testdata/relations.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.ApplyRelations(cat, rels); err != nil {
		t.Fatal(err)
	}
	sem := semantics.NewDerived(cat)
	turns := []runctx.Turn{{Role: "user", Content: "delete asset 123"}}
	pack := runctx.NewBuilder(8192).Build(cat, turns, nil, sem, nil)
	if strings.Contains(pack.Index, "billing.list") {
		t.Fatalf("unrelated operation in the pack: %s", pack.Index)
	}
	if !strings.Contains(pack.Index, "assets.delete") {
		t.Fatalf("delete missing from the pack: %s", pack.Index)
	}
	if pack.Index == cat.IndexLine() {
		t.Fatal("pack listed every operation")
	}
}
