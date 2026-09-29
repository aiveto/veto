package catalog_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aiveto/veto/catalog"
)

func TestSurfaceRegressions(t *testing.T) {
	base := map[string]catalog.OpFact{
		"teams.get":     {Referenced: true},
		"assets.delete": {Destructive: true, Confirmation: true},
		"assets.get":    {Referenced: true, Confirmation: false},
	}
	changed := map[string]catalog.OpFact{
		"assets.delete": {Destructive: true, Confirmation: false},
		"assets.get":    {Referenced: true},
	}
	cases := []struct {
		name  string
		next  map[string]catalog.OpFact
		allow map[string]bool
		want  []string
	}{
		{
			name: "lost confirmation and removed join",
			next: changed,
			want: []string{
				"operation assets.delete lost confirmation",
				"operation teams.get referenced by a relation or link was removed",
			},
		},
		{
			name:  "intentional confirmation change",
			next:  changed,
			allow: map[string]bool{"assets.delete": true},
			want:  []string{"operation teams.get referenced by a relation or link was removed"},
		},
		{name: "unchanged", next: base},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, catalog.SurfaceRegressions(base, tc.next, tc.allow))
		})
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
	assert.True(t, facts["teams.get"].Referenced)
	assert.True(t, facts["assets.get"].Referenced)
	del := facts["assets.delete"]
	assert.True(t, del.Destructive)
	assert.True(t, del.Confirmation)
	assert.False(t, del.Referenced)
}
