package catalog_test

import (
	"context"
	"strings"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/openapi"
)

func TestRelationJoinsAssetsToTeams(t *testing.T) {
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
	for _, id := range cat.Graph.Related("assets.get") {
		if id == "teams.get" {
			t.Fatal("teamsId created an edge before a relation was declared")
		}
	}
	rels, err := catalog.LoadRelations("../testdata/relations.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.ApplyRelations(cat, rels); err != nil {
		t.Fatal(err)
	}
	got := cat.Graph.Related("assets.get")
	if len(got) == 0 || !contains(got, "teams.get") {
		t.Fatalf("related: %v", got)
	}
	var note string
	for _, e := range cat.Graph.Edges {
		if e.Kind == catalog.EdgeRelates && e.From == "assets.get" && e.To == "teams.get" {
			note = e.Note
		}
	}
	if note != "Holding.teamsId" {
		t.Fatalf("note %q", note)
	}
	if assets.ByID("assets.get").BaseURL == teams.ByID("teams.get").BaseURL {
		t.Fatal("each API keeps its own server URL")
	}
}

func TestRelationRejectsUnusedSchemaAndMissingTarget(t *testing.T) {
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
	err = catalog.ApplyRelations(cat, []catalog.Relation{{Schema: "Missing", Field: "id", To: "teams.get"}})
	if err == nil || !strings.Contains(err.Error(), "not used") {
		t.Fatalf("unused schema: %v", err)
	}
	err = catalog.ApplyRelations(cat, []catalog.Relation{{Schema: "Holding", Field: "teamsId", To: "missing.get"}})
	if err == nil || !strings.Contains(err.Error(), "unknown operation") {
		t.Fatalf("missing target: %v", err)
	}
}

func TestDuplicateOperationRefused(t *testing.T) {
	assets, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = catalog.Merge(assets, assets)
	if err == nil || !strings.Contains(err.Error(), "duplicate operation") {
		t.Fatalf("expected duplicate, got %v", err)
	}
}

func contains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
