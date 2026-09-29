package catalog_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/semantics"
)

func TestSearchCapsAndPrefersExactID(t *testing.T) {
	ops := []catalog.Operation{{ID: "item.get", Name: "Get item", Description: "fetch one"}}
	for i := 0; i < 20; i++ {
		ops = append(ops, catalog.Operation{
			ID:          fmt.Sprintf("widget.%02d", i),
			Description: "mentions item.get once",
			Name:        "mention",
		})
	}
	cat := &catalog.Catalog{Operations: ops}
	cat.Finalize()
	matches := catalog.Search(cat, "item.get", nil)
	if len(matches) == 0 || len(matches) > 8 || matches[0].Operation.ID != "item.get" {
		t.Fatalf("hits: %d first=%v", len(matches), matches)
	}
}

func TestTagsAndPathNounAreSearchable(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID: "widgets.ping", Group: "widgets", Tags: []string{"retire"}, Name: "Ping", Description: "Ping",
	}}}
	cat.Finalize()
	sem := semantics.NewDerived(cat)
	var sawRetire, sawNoun bool
	for _, s := range sem.AllSynonyms()["widgets.ping"] {
		if s == "retire" {
			sawRetire = true
		}
		if s == "widgets" {
			sawNoun = true
		}
	}
	if !sawRetire || !sawNoun {
		t.Fatalf("synonyms: %v", sem.AllSynonyms()["widgets.ping"])
	}
	matches := catalog.Search(cat, "retire", sem.AllSynonyms())
	if len(matches) != 1 || matches[0].Operation.ID != "widgets.ping" {
		t.Fatalf("tag search: %v", matches)
	}
}

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
