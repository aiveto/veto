package catalog_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aiveto/veto/catalog"
)

func TestSurfaceRegressions(t *testing.T) {
	base := map[string]catalog.OpFact{
		"customers.get": {Referenced: true},
		"orders.delete": {Destructive: true, Confirmation: true},
		"orders.get":    {Referenced: true, Confirmation: false},
	}
	changed := map[string]catalog.OpFact{
		"orders.delete": {Destructive: true, Confirmation: false},
		"orders.get":    {Referenced: true},
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
				"operation customers.get referenced by a relation or link was removed",
				"operation orders.delete lost confirmation",
			},
		},
		{
			name:  "intentional confirmation change",
			next:  changed,
			allow: map[string]bool{"orders.delete": true},
			want:  []string{"operation customers.get referenced by a relation or link was removed"},
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
			{ID: "orders.get"},
			{ID: "orders.delete", Kind: catalog.KindDelete, SideEffect: catalog.SideEffectDestructive, RequiresConfirmation: true},
			{ID: "customers.get"},
		},
		Links: []catalog.OpLink{{From: "orders.get", To: "customers.get", Note: "Order.customerId"}},
	}
	cat.Finalize()
	facts := catalog.Facts(cat)
	assert.True(t, facts["customers.get"].Referenced)
	assert.True(t, facts["orders.get"].Referenced)
	del := facts["orders.delete"]
	assert.True(t, del.Destructive)
	assert.True(t, del.Confirmation)
	assert.False(t, del.Referenced)
}
