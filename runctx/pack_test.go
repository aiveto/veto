package runctx_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/semantics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackOmitsRawSpecAndIncludesDescribed(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	sem := semantics.NewDerived(cat)
	op := cat.ByID("orders.delete")
	pack := runctx.NewBuilder(4096).Build(cat, []runctx.Turn{{Role: "user", Content: "delete 123"}}, op, sem, nil)
	assert.False(t, runctx.ContainsRawSpec(pack.Serialize()))
	assert.Equal(t, "orders.delete", pack.DescribedOperationID)
}

func joinedCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	orders, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	customers, err := openapi.Load(context.Background(), "../testdata/customers.yaml")
	require.NoError(t, err)
	cat, err := catalog.Merge(orders, customers)
	require.NoError(t, err)
	relData, err := os.ReadFile("../testdata/relations.yaml")
	require.NoError(t, err)
	rels, err := catalog.ParseRelations(relData)
	require.NoError(t, err)
	require.NoError(t, catalog.ApplyRelations(cat, rels))
	return cat
}

func TestPackWalksDeclaredRelation(t *testing.T) {
	orders, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	customers, err := openapi.Load(context.Background(), "../testdata/customers.yaml")
	require.NoError(t, err)
	cat, err := catalog.Merge(orders, customers)
	require.NoError(t, err)
	relData, err := os.ReadFile("../testdata/relations.yaml")
	require.NoError(t, err)
	rels, err := catalog.ParseRelations(relData)
	require.NoError(t, err)
	require.NoError(t, catalog.ApplyRelations(cat, rels))
	sem := semantics.NewDerived(cat)
	pack := runctx.NewBuilder(8192).Build(cat, nil, cat.ByID("orders.get"), sem, nil)
	ser := pack.Serialize()
	assert.Contains(t, ser, "related: customers.get Order.customerId")
	assert.Contains(t, ser, "Order.customerId identifies customers.get")
}

func TestPackIncludesCallShapeAndRelationField(t *testing.T) {
	cat := joinedCatalog(t)
	sem := semantics.NewDerived(cat)
	pack := runctx.NewBuilder(8192).Build(cat, []runctx.Turn{{Role: "user", Content: "get order"}}, nil, sem, nil)
	assert.Contains(t, pack.Index, "id path required")
	assert.Contains(t, pack.Index, "customerId")
	assert.NotContains(t, pack.Index, "billing.list")
	assert.Less(t, strings.Index(pack.Index, "orders.get"), strings.Index(pack.Index, "customers.get"))
}

func TestTruncateDropsWholeOperations(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{ID: "alpha.read", Description: "read the alpha record fully", Name: "alpha"},
		{ID: "beta.read", Description: "read the beta record fully", Name: "beta"},
	}}
	cat.Finalize()
	sem := semantics.NewDerived(cat)
	full := runctx.NewBuilder(10000).Build(cat, []runctx.Turn{{Role: "user", Content: "read"}}, nil, sem, nil)
	parts := strings.Split(full.Index, "; ")
	require.GreaterOrEqual(t, len(parts), 2)
	overhead := full.Bytes - len(full.Index)
	pack := runctx.NewBuilder(overhead+len(parts[0])).Build(cat, []runctx.Turn{{Role: "user", Content: "read"}}, nil, sem, nil)
	assert.True(t, pack.Truncated)
	assert.Equal(t, parts[0], pack.Index)
	assert.True(t, strings.HasPrefix(pack.Index, "alpha.read") || strings.HasPrefix(pack.Index, "beta.read"))
}

func TestPackKeepsSearchHitsAndDropsTheRest(t *testing.T) {
	orders, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	customers, err := openapi.Load(context.Background(), "../testdata/customers.yaml")
	require.NoError(t, err)
	cat, err := catalog.Merge(orders, customers)
	require.NoError(t, err)
	cat.Operations = append(cat.Operations, catalog.Operation{
		ID: "billing.list", Description: "List invoices", Group: "billing", Name: "List invoices",
	})
	cat.Finalize()
	relData, err := os.ReadFile("../testdata/relations.yaml")
	require.NoError(t, err)
	rels, err := catalog.ParseRelations(relData)
	require.NoError(t, err)
	require.NoError(t, catalog.ApplyRelations(cat, rels))
	sem := semantics.NewDerived(cat)
	turns := []runctx.Turn{{Role: "user", Content: "delete order 123"}}
	pack := runctx.NewBuilder(8192).Build(cat, turns, nil, sem, nil)
	assert.NotContains(t, pack.Index, "billing.list")
	assert.Contains(t, pack.Index, "orders.delete")
	assert.NotEqual(t, cat.IndexLine(), pack.Index)
}
