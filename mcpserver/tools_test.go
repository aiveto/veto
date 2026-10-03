package mcpserver_test

import (
	"context"
	"os"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/mcpserver"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/semantics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPToolListIsCapabilitiesPlusPins(t *testing.T) {
	names := mcpserver.RegisterTools(nil, mcpserver.Options{
		Pins:       []string{"orders.get"},
		DirectPins: true,
	})
	want := []string{
		"capabilities_search",
		"capabilities_describe",
		"capabilities_invoke",
		"orders.get",
	}
	assert.Equal(t, want, names)
	namesDefault := mcpserver.RegisterTools(nil, mcpserver.Options{})
	assert.Len(t, namesDefault, 3)
}

func TestGroupedAddsOneToolPerResource(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{ID: "orders.list", Group: "orders"},
		{ID: "orders.get", Group: "orders"},
		{ID: "orders.delete", Group: "orders", Exposure: catalog.ExposureDiscovery},
	}}
	cat.Finalize()
	names := mcpserver.RegisterTools(cat, mcpserver.Options{Grouped: true})
	assert.Equal(t, []string{"capabilities_search", "capabilities_describe", "capabilities_invoke", "orders"}, names)
	err := mcpserver.ValidatePins(cat, []string{"orders.delete"})
	assert.ErrorContains(t, err, "discovery-only")
}

func TestDescribeIncludesLinkAndSchema(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	srv := &mcpserver.Server{Catalog: cat, Semantics: semantics.New(cat)}
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
	srv := &mcpserver.Server{Catalog: cat, Semantics: sem}
	b, err := srv.Describe("orders.get")
	require.NoError(t, err)
	text := string(b)
	assert.Contains(t, text, "id path required")
	assert.Contains(t, text, "customerId")
	assert.Contains(t, text, "Order.customerId identifies customers.get")
}
