package capability_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/aiveto/veto/capability"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/flow"
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
	related := cat.ByID("customers.get")
	require.NotNil(t, related)
	assert.Contains(t, get.RelatedCalls, runctx.OperationLine(cat, *related, sem.Note(related.ID).Text()))
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

func TestSearchDoesNotCloneSynonymsOnEveryCall(t *testing.T) {
	ops := make([]catalog.Operation, 0, 256)
	for i := range 256 {
		ops = append(ops, catalog.Operation{ID: fmt.Sprintf("orders.op%d", i), Name: "order", Description: "retire delete"})
	}
	cat := &catalog.Catalog{Operations: ops}
	cat.Finalize()
	sem := semantics.New(cat)
	srv := &capability.Server{Catalog: cat, Semantics: sem}
	srv.Search("retire", 0, 8)
	allocs := testing.AllocsPerRun(20, func() { srv.Search("retire", 0, 8) })
	assert.Less(t, allocs, 200.0)
}

func TestSearchReturnsATask(t *testing.T) {
	taught, err := flow.Teach(&flow.Definition{
		Name:     "investigate-charge",
		Question: "Investigate a customer's disputed charge",
		Answer:   []string{"amount", "currency"},
		Steps:    []flow.Step{{Operation: "orders.get"}, {Operation: "invoices.get"}},
	}, &catalog.Catalog{
		Operations: []catalog.Operation{
			{ID: "orders.get", Kind: catalog.KindRead, ResponseFields: []string{"invoiceId"}},
			{ID: "invoices.get", Kind: catalog.KindRead, ResponseFields: []string{"amount", "currency"}, Params: []catalog.Param{{Name: "id", In: "path", Required: true}}},
		},
		Links: []catalog.OpLink{{From: "orders.get", To: "invoices.get", Note: "Order.invoiceId"}},
	})
	require.NoError(t, err)
	srv := &capability.Server{Flows: map[string]*flow.Definition{taught.Name: taught}}
	hits := srv.Search("disputed charge", 0, 8)
	require.Len(t, hits, 1)
	assert.True(t, hits[0].Task)
	assert.Equal(t, "investigate-charge", hits[0].ID)
	assert.Equal(t, "Investigate a customer's disputed charge", hits[0].Call)
	assert.Equal(t, []string{"orders.get invoiceId -> invoices.get id"}, hits[0].Related)
	body, err := srv.Describe("investigate-charge")
	require.NoError(t, err)
	assert.Contains(t, string(body), "invoiceId")
	assert.Empty(t, srv.Search("orders.get", 0, 8))
}
