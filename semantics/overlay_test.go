package semantics

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/catalog"
)

func TestOverlayMissingKeyKeepsDerivedSentence(t *testing.T) {
	cat := &catalog.Catalog{
		Operations: []catalog.Operation{
			{ID: "assets.get", Name: "getAsset", Description: "Fetch one asset"},
			{ID: "assets.delete", Name: "deleteAsset", Description: "Remove an asset"},
			{ID: "teams.get", Name: "getTeam", Description: "Fetch one team"},
		},
		Uses: []catalog.SchemaUse{{OperationID: "assets.get", Name: "Holding"}},
	}
	if err := catalog.ApplyRelations(cat, []catalog.Relation{{
		Schema: "Holding", Field: "teamsId", To: "teams.get",
	}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "semantics.yaml")
	body := "- operation: assets.delete\n  synonyms:\n    - retire\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	over, err := ParseOverlay(data, NewDerived(cat))
	if err != nil {
		t.Fatal(err)
	}
	patched := over.Note("assets.delete")
	if patched.Sentence != "Remove an asset" {
		t.Fatalf("missing sentence key: %q", patched.Sentence)
	}
	missing := over.Note("assets.get")
	if missing.Sentence != "Fetch one asset" {
		t.Fatalf("operation absent from the file: %q", missing.Sentence)
	}
	if missing.Relation != "Holding.teamsId identifies teams.get" {
		t.Fatalf("relation: %q", missing.Relation)
	}
}
