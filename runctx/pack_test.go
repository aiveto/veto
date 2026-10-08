package runctx_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/semantics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackOmitsRawSpecAndIncludesDescribed(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	sem := semantics.New(cat)
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
	sem := semantics.New(cat)
	pack := runctx.NewBuilder(8192).Build(cat, nil, cat.ByID("orders.get"), sem, nil)
	ser := pack.Serialize()
	assert.Contains(t, ser, "related: customers.get Order.customerId")
	assert.Contains(t, ser, "Order.customerId identifies customers.get")
}

func TestPackIncludesCallShapeAndRelationField(t *testing.T) {
	cat := joinedCatalog(t)
	sem := semantics.New(cat)
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
	sem := semantics.New(cat)
	full := runctx.NewBuilder(10000).Build(cat, []runctx.Turn{{Role: "user", Content: "read"}}, nil, sem, nil)
	parts := strings.Split(full.Index, "; ")
	require.GreaterOrEqual(t, len(parts), 2)
	overhead := full.Bytes - len(full.Index)
	pack := runctx.NewBuilder(overhead+len(parts[0])).Build(cat, []runctx.Turn{{Role: "user", Content: "read"}}, nil, sem, nil)
	assert.True(t, pack.Truncated)
	assert.Equal(t, parts[0], pack.Index)
	assert.True(t, strings.HasPrefix(pack.Index, "alpha.read") || strings.HasPrefix(pack.Index, "beta.read"))
}

func TestPackBoundsTurnsDetailAndPending(t *testing.T) {
	desc := strings.Repeat("describe-", 40)
	turn := strings.Repeat("turn-text-", 40)
	param := strings.Repeat("9", 400)
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID: "orders.get", Method: "GET", PathTemplate: "/orders/{id}", Description: desc, Name: "get",
	}}}
	cat.Finalize()
	sem := semantics.New(cat)
	turns := []runctx.Turn{{Role: "user", Content: turn}}
	pending := &policy.PendingConfirmation{
		ID:          "pend-1",
		OperationID: "orders.delete",
		Params:      map[string]string{"id": param, "note": strings.Repeat("n", 400)},
	}
	bare := runctx.NewBuilder(1<<20).Build(nil, nil, nil, nil, nil)
	pack := runctx.NewBuilder(bare.Bytes+64).Build(cat, turns, cat.ByID("orders.get"), sem, pending)
	assert.True(t, pack.Truncated)
	assert.LessOrEqual(t, pack.Bytes, bare.Bytes+64)
	assert.NotContains(t, pack.Index, desc)
	assert.NotContains(t, pack.DescribedDetail, desc)
	assert.NotContains(t, pack.Serialize(), turn)
	assert.NotContains(t, pack.Serialize(), param)
	assert.Equal(t, turn, turns[0].Content)
	assert.Equal(t, param, pending.Params["id"])
	require.NotNil(t, pack.PendingConfirmation)
	assert.Equal(t, "pend-1", pack.PendingConfirmation.ID)
	assert.Empty(t, pack.PendingConfirmation.Params)
	assert.Contains(t, pack.Serialize(), "pend-1")
}

func TestSerializedPackStaysInsideTheBudget(t *testing.T) {
	note := strings.Repeat("n", 9000)
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID: "orders.get", Method: "GET", PathTemplate: "/orders/{id}", Name: "get",
	}}}
	cat.Finalize()
	cat.Graph.Edges = append(cat.Graph.Edges, catalog.Edge{
		From: "orders.get", To: "customers.get", Kind: catalog.EdgeRelates, Note: note,
	})
	sem := semantics.New(cat)
	pack := runctx.NewBuilder(8192).Build(cat, nil, cat.ByID("orders.get"), sem, nil)
	assert.LessOrEqual(t, len(pack.Serialize()), 8192)
	assert.NotContains(t, pack.Serialize(), note)
	assert.True(t, pack.Truncated)

	turns := make([]runctx.Turn, 40)
	for i := range turns {
		turns[i] = runctx.Turn{Role: "user", Content: "hi"}
	}
	longID := strings.Repeat("op", 3000)
	wide := &catalog.Catalog{Operations: []catalog.Operation{{ID: longID, Name: "n", Description: "d"}}}
	wide.Finalize()
	many := runctx.NewBuilder(8192).Build(wide, turns, wide.ByID(longID), semantics.New(wide), &policy.PendingConfirmation{
		ID: strings.Repeat("p", 5000), OperationID: longID, Params: map[string]string{"id": strings.Repeat("9", 4000)},
	})
	assert.LessOrEqual(t, len(many.Serialize()), 8192)
	assert.NotContains(t, many.Serialize(), strings.Repeat("9", 4000))
	if many.PendingConfirmation != nil {
		assert.Equal(t, strings.Repeat("p", 5000), many.PendingConfirmation.ID)
		assert.Equal(t, longID, many.PendingConfirmation.OperationID)
	} else {
		assert.NotContains(t, many.Serialize(), "pending_confirmation:")
	}

	tiny := runctx.NewBuilder(1).Build(cat, []runctx.Turn{{Role: "user", Content: "hello"}}, cat.ByID("orders.get"), sem, nil)
	assert.LessOrEqual(t, len(tiny.Serialize()), 1)
	assert.True(t, utf8.ValidString(tiny.Serialize()))

	runeText := strings.Repeat("é", 50)
	original := []runctx.Turn{{Role: "user", Content: runeText}}
	cut := runctx.NewBuilder(len("rules: ")+len(runctx.NewBuilder(0).Build(nil, nil, nil, nil, nil).Rules)+8).Build(nil, original, nil, nil, nil)
	assert.LessOrEqual(t, len(cut.Serialize()), len("rules: ")+len(runctx.NewBuilder(0).Build(nil, nil, nil, nil, nil).Rules)+8)
	assert.True(t, utf8.ValidString(cut.Serialize()))
	assert.Equal(t, runeText, original[0].Content)
}

func TestPackOmitsAnApprovalIDThatDoesNotFit(t *testing.T) {
	id := strings.Repeat("a", 80)
	pack := runctx.NewBuilder(40).Build(nil, nil, nil, nil, &policy.PendingConfirmation{
		ID: id, OperationID: "orders.delete",
	})
	assert.LessOrEqual(t, len(pack.Serialize()), 40)
	if pack.PendingConfirmation != nil {
		assert.Equal(t, id, pack.PendingConfirmation.ID)
	} else {
		assert.NotContains(t, pack.Serialize(), "pending_confirmation:")
	}
	assert.NotContains(t, pack.Serialize(), strings.Repeat("a", 10))
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
	sem := semantics.New(cat)
	turns := []runctx.Turn{{Role: "user", Content: "delete order 123"}}
	pack := runctx.NewBuilder(8192).Build(cat, turns, nil, sem, nil)
	assert.NotContains(t, pack.Index, "billing.list")
	assert.Contains(t, pack.Index, "orders.delete")
	assert.NotEqual(t, cat.IndexLine(), pack.Index)
}

func TestPackIncludesAMatchingTask(t *testing.T) {
	b := runctx.NewBuilder(8192)
	b.Tasks = []string{"task investigate-charge: Investigate a customer's disputed charge. answer: amount, currency | orders.get invoiceId -> invoices.get id"}
	pack := b.Build(nil, []runctx.Turn{{Role: "user", Content: "disputed charge"}}, nil, nil, nil)
	assert.Contains(t, pack.Index, "investigate-charge")
	assert.Contains(t, pack.Index, "invoiceId")
	other := b.Build(nil, []runctx.Turn{{Role: "user", Content: "orders.get"}}, nil, nil, nil)
	assert.NotContains(t, other.Index, "investigate-charge")
	b.Tasks = append(b.Tasks, "task read-orders-list: List orders on the desk. answer: data")
	orders := b.Build(nil, []runctx.Turn{{Role: "user", Content: "show me all orders"}}, nil, nil, nil)
	assert.Contains(t, orders.Index, "read-orders-list")
	customers := b.Build(nil, []runctx.Turn{{Role: "user", Content: "show me all customers"}}, nil, nil, nil)
	assert.NotContains(t, customers.Index, "read-orders-list")
}
