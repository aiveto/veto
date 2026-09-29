package catalog_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aiveto/veto/catalog"
)

func TestSurfaceRegressions(t *testing.T) {
	yes := true
	no := false
	base := map[string]catalog.OpFact{
		"customers.get": {Referenced: true, Callable: &no},
		"orders.delete": {Destructive: true, Confirmation: true, Permissions: []string{"order.delete"}},
		"orders.get":    {Referenced: true, Confirmation: false},
	}
	changed := map[string]catalog.OpFact{
		"orders.delete": {Destructive: true, Confirmation: false, Permissions: []string{"order.delete"}},
		"orders.get":    {Referenced: true},
	}
	opened := map[string]catalog.OpFact{
		"customers.get": {Referenced: true, Callable: &yes},
		"orders.delete": {Destructive: true, Confirmation: true, Callable: &yes},
		"orders.get":    {Referenced: true, Callable: &yes},
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
		{
			name: "discovery-only became callable and a permission was removed",
			next: opened,
			want: []string{
				"operation customers.get became callable",
				"operation orders.delete lost permission order.delete",
			},
		},
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
	requireCallable := func(id string, want bool) {
		t.Helper()
		if assert.NotNil(t, facts[id].Callable) {
			assert.Equal(t, want, *facts[id].Callable)
		}
	}
	requireCallable("orders.get", true)
	cat.Operations[1].Exposure = catalog.ExposureDiscovery
	cat.Operations[1].Permissions = []string{"order.delete"}
	cat.Finalize()
	hidden := catalog.Facts(cat)
	if assert.NotNil(t, hidden["orders.delete"].Callable) {
		assert.False(t, *hidden["orders.delete"].Callable)
	}
	assert.Equal(t, []string{"order.delete"}, hidden["orders.delete"].Permissions)
}
