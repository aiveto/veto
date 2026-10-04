package catalog_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/openapi"
)

type relationSuite struct {
	suite.Suite
	orders    *catalog.Catalog
	customers *catalog.Catalog
	cat       *catalog.Catalog
}

func (s *relationSuite) SetupTest() {
	var err error
	s.orders, err = openapi.Load(context.Background(), "../testdata/orders.yaml")
	s.Require().NoError(err)
	s.customers, err = openapi.Load(context.Background(), "../testdata/customers.yaml")
	s.Require().NoError(err)
	s.cat, err = catalog.Merge(s.orders, s.customers)
	s.Require().NoError(err)
}

func (s *relationSuite) TestJoinAppearsOnlyAfterDeclaration() {
	s.NotContains(s.cat.Graph.Related("orders.get"), "customers.get")
	relData, err := os.ReadFile("../testdata/relations.yaml")
	s.Require().NoError(err)
	rels, err := catalog.ParseRelations(relData)
	s.Require().NoError(err)
	s.Require().NoError(catalog.ApplyRelations(s.cat, rels))
	s.Contains(s.cat.Graph.Related("orders.get"), "customers.get")
	s.Contains(s.cat.Joins(), "orders.get --[Order.customerId]--> customers.get")
	s.NotEqual(s.orders.ByID("orders.get").BaseURL, s.customers.ByID("customers.get").BaseURL)
}

func (s *relationSuite) TestRejectsUnusedSchemaAndMissingTarget() {
	err := catalog.ApplyRelations(s.cat, []catalog.Relation{{Schema: "Missing", Field: "id", To: "customers.get"}})
	s.Require().ErrorContains(err, "not used")
	err = catalog.ApplyRelations(s.cat, []catalog.Relation{{Schema: "Order", Field: "customerId", To: "missing.get"}})
	s.Require().ErrorContains(err, "unknown operation")
}

func (s *relationSuite) TestJoinFromOtherOperationIsKept() {
	err := catalog.ApplyRelations(s.cat, []catalog.Relation{{Schema: "Order", Field: "id", To: "orders.get"}})
	s.Require().NoError(err)
	s.Contains(s.cat.Graph.Related("orders.list"), "orders.get")
	s.NotContains(s.cat.Graph.Related("orders.get"), "orders.get")
}

func (s *relationSuite) TestRelatedIdsAreUnique() {
	s.Require().NoError(catalog.ApplyRelations(s.cat, []catalog.Relation{
		{Schema: "Order", Field: "customerId", To: "customers.get"},
		{Schema: "Order", Field: "warehouseId", To: "customers.get"},
	}))
	n := 0
	for _, id := range s.cat.Graph.Related("orders.get") {
		if id == "customers.get" {
			n++
		}
	}
	s.Equal(1, n)
}

func TestApplyRelationsRejectsOnlySelf(t *testing.T) {
	cat := &catalog.Catalog{
		Operations: []catalog.Operation{{ID: "orders.get", Name: "get"}},
		Uses:       []catalog.SchemaUse{{OperationID: "orders.get", Name: "Order"}},
	}
	err := catalog.ApplyRelations(cat, []catalog.Relation{{Schema: "Order", Field: "id", To: "orders.get"}})
	require.ErrorContains(t, err, "same operation")
}

func TestRelations(t *testing.T) {
	suite.Run(t, new(relationSuite))
}

func TestDuplicateOperationRefused(t *testing.T) {
	orders, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	_, err = catalog.Merge(orders, orders)
	require.ErrorContains(t, err, "duplicate operation")
	require.ErrorContains(t, err, "orders.delete")
	require.ErrorContains(t, err, "orders.get")
	require.ErrorContains(t, err, "orders.list")

	dir := t.TempDir()
	const spec = `openapi: 3.0.3
info:
  title: Customers
  version: "1"
paths:
  /customers/{id}:
    get:
      operationId: customers.get
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
`
	for _, name := range []string{"customers.yaml", "customer-v3.yaml"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(spec), 0o600))
	}
	left, err := openapi.Load(context.Background(), filepath.Join(dir, "customers.yaml"))
	require.NoError(t, err)
	right, err := openapi.Load(context.Background(), filepath.Join(dir, "customer-v3.yaml"))
	require.NoError(t, err)
	_, err = catalog.Merge(left, right)
	require.ErrorContains(t, err, "duplicate operation")
	assert.ErrorContains(t, err, "customers.get")
}

func TestParseRelationsRejectsUnknownFields(t *testing.T) {
	_, err := catalog.ParseRelations([]byte("relations:\n  - schema: Order\n    field: customerId\n    to: customers.get\n    extra: true\n"))
	require.Error(t, err)
	_, err = catalog.ParseRelations([]byte("relations:\n  - schema: Order\n    field: id\n    to: orders.get\n---\nrelations: []\n"))
	require.Error(t, err)
}
