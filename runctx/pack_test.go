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

func joinedCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
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
	return cat
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

func TestPackIncludesCallShapeAndRelationField(t *testing.T) {
	cat := joinedCatalog(t)
	sem := semantics.NewDerived(cat)
	pack := runctx.NewBuilder(8192).Build(cat, []runctx.Turn{{Role: "user", Content: "get asset"}}, nil, sem, nil)
	if runctx.ContainsRawSpec(pack.Serialize()) {
		t.Fatal("pack contains the spec")
	}
	if !strings.Contains(pack.Index, "id path required") || !strings.Contains(pack.Index, "teamsId") {
		t.Fatalf("call shape missing:\n%s", pack.Index)
	}
	if strings.Contains(pack.Index, "billing.list") {
		t.Fatalf("unrelated operation in the pack: %s", pack.Index)
	}
	if strings.Index(pack.Index, "assets.get") > strings.Index(pack.Index, "teams.get") {
		t.Fatalf("neighbor outranked the hit:\n%s", pack.Index)
	}
}

func TestTruncateDropsWholeOperations(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{ID: "alpha.read", Description: "read the alpha record fully", Name: "alpha"},
		{ID: "beta.read", Description: "read the beta record fully", Name: "beta"},
	}}
	cat.Finalize()
	sem := semantics.NewDerived(cat)
	full := runctx.NewBuilder(10000).Build(cat, []runctx.Turn{{Role: "user", Content: "read"}}, nil, sem, nil)
	parts := strings.Split(full.Index, "; ")
	if len(parts) < 2 {
		t.Fatalf("index: %s", full.Index)
	}
	overhead := full.Bytes - len(full.Index)
	pack := runctx.NewBuilder(overhead+len(parts[0])).Build(cat, []runctx.Turn{{Role: "user", Content: "read"}}, nil, sem, nil)
	if !pack.Truncated || pack.Index != parts[0] {
		t.Fatalf("truncated=%v index=%q want %q", pack.Truncated, pack.Index, parts[0])
	}
	if !strings.HasPrefix(pack.Index, "alpha.read") && !strings.HasPrefix(pack.Index, "beta.read") {
		t.Fatalf("cut an id: %q", pack.Index)
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
