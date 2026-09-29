package catalog_test

import (
	"testing"

	"github.com/aiveto/veto/catalog"
)

func TestSurfaceRegressions(t *testing.T) {
	base := map[string]catalog.OpFact{
		"teams.get":     {Referenced: true},
		"assets.delete": {Destructive: true, Confirmation: true},
		"assets.get":    {Referenced: true, Confirmation: false},
	}
	next := map[string]catalog.OpFact{
		"assets.delete": {Destructive: true, Confirmation: false},
		"assets.get":    {Referenced: true},
	}
	got := catalog.SurfaceRegressions(base, next, nil)
	want := []string{
		"operation assets.delete lost confirmation",
		"operation teams.get referenced by a relation or link was removed",
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("regressions: %v", got)
	}
	allowed := catalog.SurfaceRegressions(base, next, map[string]bool{"assets.delete": true})
	if len(allowed) != 1 || allowed[0] != want[1] {
		t.Fatalf("intentional confirmation change: %v", allowed)
	}
	same := catalog.SurfaceRegressions(base, base, nil)
	if len(same) != 0 {
		t.Fatalf("unchanged surface: %v", same)
	}
}

func TestFactsMarkJoinedAndDestructiveOperations(t *testing.T) {
	cat := &catalog.Catalog{
		Operations: []catalog.Operation{
			{ID: "assets.get"},
			{ID: "assets.delete", Kind: catalog.KindDelete, SideEffect: catalog.SideEffectDestructive, RequiresConfirmation: true},
			{ID: "teams.get"},
		},
		Links: []catalog.OpLink{{From: "assets.get", To: "teams.get", Note: "Holding.teamsId"}},
	}
	cat.Finalize()
	facts := catalog.Facts(cat)
	if !facts["teams.get"].Referenced || !facts["assets.get"].Referenced {
		t.Fatalf("join endpoints: %+v", facts)
	}
	if !facts["assets.delete"].Destructive || !facts["assets.delete"].Confirmation || facts["assets.delete"].Referenced {
		t.Fatalf("delete: %+v", facts["assets.delete"])
	}
}
