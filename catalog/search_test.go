package catalog_test

import (
	"context"
	"fmt"
	"testing"

	"os"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/semantics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchCapsAndPrefersExactID(t *testing.T) {
	ops := make([]catalog.Operation, 0, 21)
	ops = append(ops, catalog.Operation{ID: "item.get", Name: "Get item", Description: "fetch one"})
	for i := range 20 {
		ops = append(ops, catalog.Operation{
			ID:          fmt.Sprintf("widget.%02d", i),
			Description: "mentions item.get once",
			Name:        "mention",
		})
	}
	cat := &catalog.Catalog{Operations: ops}
	cat.Finalize()
	matches := catalog.Search(cat, "item.get", nil)
	require.NotEmpty(t, matches)
	assert.LessOrEqual(t, len(matches), 8)
	assert.Equal(t, "item.get", matches[0].Operation.ID)
}

func TestTagsAndPathNounAreSearchable(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID: "widgets.ping", Group: "widgets", Tags: []string{"retire"}, Name: "Ping", Description: "Ping",
	}}}
	cat.Finalize()
	sem := semantics.NewDerived(cat)
	var sawRetire, sawNoun bool
	for _, s := range sem.AllSynonyms()["widgets.ping"] {
		if s == "retire" {
			sawRetire = true
		}
		if s == "widgets" {
			sawNoun = true
		}
	}
	assert.True(t, sawRetire)
	assert.True(t, sawNoun)
	matches := catalog.Search(cat, "retire", sem.AllSynonyms())
	require.Len(t, matches, 1)
	assert.Equal(t, "widgets.ping", matches[0].Operation.ID)
}

func TestSearchPageReturnsTheNextWindow(t *testing.T) {
	ops := make([]catalog.Operation, 0, 10)
	for i := range 10 {
		ops = append(ops, catalog.Operation{
			ID:          fmt.Sprintf("item.%02d", i),
			Description: "mentions item.get once",
			Name:        "mention",
		})
	}
	cat := &catalog.Catalog{Operations: ops}
	cat.Finalize()
	first := catalog.SearchPage(cat, "item.get", nil, 0, 3)
	next := catalog.SearchPage(cat, "item.get", nil, 3, 3)
	require.Len(t, first, 3)
	require.Len(t, next, 3)
	assert.NotEqual(t, first[0].Operation.ID, next[0].Operation.ID)
}

func TestSearchRetireFindsDelete(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	base := semantics.NewDerived(cat)
	overlay, err := os.ReadFile("../testdata/semantics.yaml")
	require.NoError(t, err)
	sem, err := semantics.ParseOverlay(overlay, base)
	require.NoError(t, err)
	matches := catalog.Search(cat, "retire", sem.AllSynonyms())
	require.NotEmpty(t, matches)
	assert.Equal(t, "orders.delete", matches[0].Operation.ID)
}

func TestSearchSchemaNameFindsOperations(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	matches := catalog.Search(cat, "Order", nil)
	got := map[string]bool{}
	for _, m := range matches {
		got[m.Operation.ID] = true
	}
	assert.True(t, got["orders.list"])
	assert.True(t, got["orders.get"])

	plain := &catalog.Catalog{
		Operations: []catalog.Operation{
			{ID: "alpha.read", Name: "read"},
			{ID: "beta.remove", Name: "remove"},
		},
		Uses: []catalog.SchemaUse{{OperationID: "alpha.read", Name: "Order"}},
	}
	got = map[string]bool{}
	for _, m := range catalog.Search(plain, "Order", nil) {
		got[m.Operation.ID] = true
	}
	assert.True(t, got["alpha.read"])
	assert.False(t, got["beta.remove"])
}

func TestSearchFollowsDeclaredRelation(t *testing.T) {
	orders, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	customers, err := openapi.Load(context.Background(), "../testdata/customers.yaml")
	require.NoError(t, err)
	cat, err := catalog.Merge(orders, customers)
	require.NoError(t, err)
	before := catalog.Search(cat, "customerId", nil)
	assert.Empty(t, before)
	relData, err := os.ReadFile("../testdata/relations.yaml")
	require.NoError(t, err)
	rels, err := catalog.ParseRelations(relData)
	require.NoError(t, err)
	require.NoError(t, catalog.ApplyRelations(cat, rels))
	matches := catalog.Search(cat, "customerId", nil)
	var found bool
	for _, m := range matches {
		if m.Operation.ID != "orders.get" {
			continue
		}
		found = true
		ok := false
		for _, id := range m.Related {
			if id == "customers.get" {
				ok = true
			}
		}
		assert.True(t, ok)
	}
	assert.True(t, found)
}
