package catalog_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"testing"

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
	sem := semantics.New(cat)
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
	seen := map[string]bool{}
	for _, m := range first {
		seen[m.Operation.ID] = true
	}
	for _, m := range next {
		assert.False(t, seen[m.Operation.ID], m.Operation.ID)
	}
}

func TestSearchPageClampsTheWindow(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID: "item.get", Name: "Get item", Description: "fetch one",
	}}}
	cat.Finalize()
	assert.Empty(t, catalog.SearchPage(cat, "item.get", nil, 1, math.MaxInt))
	matches := catalog.SearchPage(cat, "item.get", nil, 0, math.MaxInt)
	require.Len(t, matches, 1)
}

func FuzzSearchPage(f *testing.F) {
	f.Add(0, 0)
	f.Add(1, int(math.MaxInt))
	f.Add(-1, -1)
	f.Add(int(math.MaxInt), int(math.MaxInt))
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID: "item.get", Name: "Get item", Description: "fetch one",
	}}}
	cat.Finalize()
	f.Fuzz(func(t *testing.T, offset, limit int) {
		matches := catalog.SearchPage(cat, "item.get", nil, offset, limit)
		if len(matches) > 8 {
			t.Fatalf("len %d", len(matches))
		}
	})
}

func TestSearchSynonymsFindTheOperation(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	sem := semantics.New(cat)
	cases := []struct {
		q, want string
	}{
		{q: "remove", want: "orders.delete"},
		{q: "retire", want: "orders.delete"},
		{q: "fetch", want: "orders.get"},
		{q: "read", want: "orders.get"},
	}
	for _, tc := range cases {
		t.Run(tc.q, func(t *testing.T) {
			matches := catalog.Search(cat, tc.q, sem.AllSynonyms())
			require.NotEmpty(t, matches)
			assert.Equal(t, tc.want, matches[0].Operation.ID)
		})
	}
}

func TestSearchRetireFindsDelete(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	base := semantics.New(cat)
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

func TestSearchRelatedIsCallerOwned(t *testing.T) {
	cat := &catalog.Catalog{
		Operations: []catalog.Operation{{ID: "orders.get"}, {ID: "customers.get"}},
		Links:      []catalog.OpLink{{From: "orders.get", To: "customers.get", Note: "Order.customerId"}},
	}
	cat.Finalize()
	first := catalog.Search(cat, "customerId", nil)
	require.NotEmpty(t, first)
	first[0].Related = append(first[0].Related, "injected")
	second := catalog.Search(cat, "customerId", nil)
	require.NotEmpty(t, second)
	assert.NotContains(t, second[0].Related, "injected")
}

func TestConcurrentSearchAndByID(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{ID: "item.get", Name: "Get item", Description: "fetch one"},
		{ID: "item.list", Name: "List items", Description: "list"},
	}}
	const n = 32
	var start sync.WaitGroup
	start.Add(n)
	goOn := make(chan struct{})
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for range n {
		wg.Go(func() {
			start.Done()
			<-goOn
			if catalog.Search(cat, "item.get", nil) == nil {
				errCh <- errors.New("empty search")
				return
			}
			if cat.ByID("item.get") == nil {
				errCh <- errors.New("missing id")
			}
		})
	}
	start.Wait()
	close(goOn)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
	assert.Equal(t, "item.get", cat.ByID("item.get").ID)
}

func BenchmarkSearch(b *testing.B) {
	ops := make([]catalog.Operation, 1000)
	uses := make([]catalog.SchemaUse, 1000)
	for i := range ops {
		id := fmt.Sprintf("item.%04d", i)
		ops[i] = catalog.Operation{ID: id, Name: "Get item", Description: "fetch one", Group: "items"}
		uses[i] = catalog.SchemaUse{OperationID: id, Name: "Order"}
	}
	ops[0].ID = "orders.get"
	uses[0].OperationID = "orders.get"
	cat := &catalog.Catalog{Operations: ops, Uses: uses, Links: []catalog.OpLink{{From: "orders.get", To: "item.0001", Note: "Order.customerId"}}}
	cat.Finalize()
	sem := semantics.New(cat)
	syns := sem.AllSynonyms()
	cases := []struct {
		name, q string
	}{
		{name: "exact", q: "orders.get"},
		{name: "broad", q: "order"},
		{name: "miss", q: "zzzz"},
		{name: "relation", q: "customerId"},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				catalog.Search(cat, tc.q, syns)
			}
		})
	}
	b.Run("parallel", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				catalog.Search(cat, "order", syns)
			}
		})
	})
}
