package capability_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/aiveto/veto/capability"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/semantics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescribeIncludesLinkAndSchema(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	srv := &capability.Server{Catalog: cat, Semantics: semantics.New(cat)}
	b, err := srv.Describe("orders.list")
	require.NoError(t, err)
	text := string(b)
	assert.Contains(t, text, "orders.get")
	assert.Contains(t, text, "Order")
}

func TestDescribeMatchesThePack(t *testing.T) {
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
	sem := semantics.New(cat)
	op := cat.ByID("orders.get")
	line := runctx.OperationLine(cat, *op, sem.Note(op.ID).Text())
	pack := runctx.NewBuilder(8192).Build(cat, nil, op, sem, nil)
	assert.Contains(t, pack.Index, line)
	srv := &capability.Server{Catalog: cat, Semantics: sem}
	b, err := srv.Describe("orders.get")
	require.NoError(t, err)
	text := string(b)
	assert.Contains(t, text, "id path required")
	assert.Contains(t, text, "customerId")
	assert.Contains(t, text, "Order.customerId identifies customers.get")
}

func TestSearchHitIsTheCallLine(t *testing.T) {
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
	sem := semantics.New(cat)
	srv := &capability.Server{Catalog: cat, Semantics: sem}

	get := hitByID(t, srv.Search("orders.get", 0, 8), "orders.get")
	op := cat.ByID("orders.get")
	assert.Equal(t, runctx.OperationLine(cat, *op, sem.Note(op.ID).Text()), get.Call)
	assert.Contains(t, get.Related, "customers.get")
	assert.False(t, get.Confirmation)

	del := hitByID(t, srv.Search("retire", 0, 8), "orders.delete")
	assert.Contains(t, del.Call, "id path required")
	assert.True(t, del.Confirmation)

	raw, err := json.Marshal(srv.Search("orders.get", 0, 8))
	require.NoError(t, err)
	text := string(raw)
	assert.NotContains(t, text, "PathTemplate")
	assert.NotContains(t, text, "BaseURL")
	assert.NotContains(t, text, `"Auth"`)
	described, err := srv.Describe("orders.get")
	require.NoError(t, err)
	assert.Contains(t, string(described), "PathTemplate")
}

func hitByID(t *testing.T, hits []capability.SearchHit, id string) capability.SearchHit {
	t.Helper()
	for _, h := range hits {
		if h.ID == id {
			return h
		}
	}
	t.Fatalf("missing %s", id)
	return capability.SearchHit{}
}
