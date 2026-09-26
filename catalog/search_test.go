package catalog_test

import (
	"context"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/semantics"
)

func TestSearchRetireFindsDelete(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	base := semantics.NewDerived(cat)
	sem, err := semantics.LoadOverlay("../testdata/semantics.yaml", base)
	if err != nil {
		t.Fatal(err)
	}
	matches := catalog.Search(cat, "retire", sem.AllSynonyms())
	if len(matches) == 0 {
		t.Fatal("expected matches for retire")
	}
	if matches[0].Operation.ID != "assets.delete" {
		t.Fatalf("expected assets.delete, got %s", matches[0].Operation.ID)
	}
}

func TestSearchSchemaNameFindsOperations(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	matches := catalog.Search(cat, "Holding", nil)
	got := map[string]bool{}
	for _, m := range matches {
		got[m.Operation.ID] = true
	}
	if !got["assets.list"] || !got["assets.get"] || got["assets.delete"] {
		t.Fatalf("schema search: %v", got)
	}
}

func TestSearchFollowsDeclaredRelation(t *testing.T) {
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
	before := catalog.Search(cat, "teamsId", nil)
	if len(before) != 0 {
		t.Fatalf("field name matched before a relation: %v", before)
	}
	rels, err := catalog.LoadRelations("../testdata/relations.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.ApplyRelations(cat, rels); err != nil {
		t.Fatal(err)
	}
	matches := catalog.Search(cat, "teamsId", nil)
	var found bool
	for _, m := range matches {
		if m.Operation.ID != "assets.get" {
			continue
		}
		found = true
		ok := false
		for _, id := range m.Related {
			if id == "teams.get" {
				ok = true
			}
		}
		if !ok {
			t.Fatalf("related: %v", m.Related)
		}
	}
	if !found {
		t.Fatalf("assets.get missing: %v", matches)
	}
}
