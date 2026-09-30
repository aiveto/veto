package catalog_test

import (
	"context"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJoinsPrintTheRelation(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{ID: "orders.get", Name: "get"},
		{ID: "customers.get", Name: "customer"},
	}, Links: []catalog.OpLink{{From: "orders.get", To: "customers.get", Note: "Order.customerId"}}}
	cat.Finalize()
	assert.Equal(t, []string{"orders.get --[Order.customerId]--> customers.get"}, cat.Joins())
}

func TestGraphLinksOperationsAndSchemas(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	assert.Equal(t, []string{"orders.get"}, cat.Graph.Related("orders.list"))
	assert.Equal(t, []string{"orders.delete"}, cat.Graph.Related("orders.get"))
	assert.Equal(t, []string{"Order"}, cat.Graph.Schemas("orders.get"))
	assert.Contains(t, cat.Graph.OperationsForResource("orders"), "orders.delete")
}
