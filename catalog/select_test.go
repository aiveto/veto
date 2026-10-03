package catalog_test

import (
	"net/http"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectionKeeps(t *testing.T) {
	get := catalog.Operation{ID: "orders.get", Method: http.MethodGet, PathTemplate: "/orders/{id}", Tags: []string{"orders"}}
	del := catalog.Operation{ID: "orders.delete", Method: http.MethodDelete, PathTemplate: "/orders/{id}", Tags: []string{"orders"}}
	archive := catalog.Operation{ID: "archive.list", Method: http.MethodGet, PathTemplate: "/ordersarchive"}
	cases := []struct {
		name string
		sel  catalog.Selection
		op   catalog.Operation
		want bool
	}{
		{name: "zero value keeps a delete", op: del, want: true},
		{name: "read only drops a delete", sel: catalog.Selection{ReadOnly: true}, op: del, want: false},
		{name: "read only keeps a get", sel: catalog.Selection{ReadOnly: true}, op: get, want: true},
		{name: "tag keeps", sel: catalog.Selection{Tags: []string{"orders"}}, op: get, want: true},
		{name: "other tag drops", sel: catalog.Selection{Tags: []string{"billing"}}, op: get, want: false},
		{name: "path prefix keeps", sel: catalog.Selection{Paths: []string{"/orders"}}, op: get, want: true},
		{name: "path prefix stops at a segment", sel: catalog.Selection{Paths: []string{"/orders"}}, op: archive, want: false},
		{name: "read only with a tag still drops a delete", sel: catalog.Selection{ReadOnly: true, Tags: []string{"orders"}}, op: del, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.sel.Keeps(tc.op))
		})
	}
}

func TestSelectDropsTheOperationAndItsRelation(t *testing.T) {
	cat := &catalog.Catalog{
		Operations: []catalog.Operation{
			{ID: "orders.get", Method: http.MethodGet, PathTemplate: "/orders/{id}"},
			{ID: "customers.get", Method: http.MethodGet, PathTemplate: "/customers/{id}"},
		},
		Uses: []catalog.SchemaUse{{OperationID: "orders.get", Name: "Order"}},
	}
	require.NoError(t, catalog.ApplyRelations(cat, []catalog.Relation{{Schema: "Order", Field: "customerId", To: "customers.get"}}))
	require.Contains(t, cat.Graph.Related("orders.get"), "customers.get")

	cat.Select(catalog.Selection{Paths: []string{"/orders"}})

	assert.Nil(t, cat.ByID("customers.get"))
	assert.NotNil(t, cat.ByID("orders.get"))
	assert.Empty(t, cat.Graph.Related("orders.get"))
	assert.Empty(t, cat.Joins())
}
