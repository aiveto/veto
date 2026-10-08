package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProposeNeedsListedResponseFields(t *testing.T) {
	err := runPropose(catalogFlags{config: filepath.Join("..", "..", "testdata", "veto.yaml")}, "")
	require.ErrorContains(t, err, "no relation joins two reads that list response fields")
	require.ErrorContains(t, err, "relations.yaml")
}

func TestProposeWritesTheNamedFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "orders.yaml"), []byte(proposeOrders), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "invoices.yaml"), []byte(proposeInvoices), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "relations.yaml"), []byte("relations:\n  - {schema: Order, field: customerId, to: invoices.get}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "veto.yaml"), []byte("contracts: [orders.yaml, invoices.yaml]\nrelations_file: relations.yaml\n"), 0o600))
	out := filepath.Join(dir, "flow.yaml")
	require.NoError(t, os.WriteFile(out, []byte("old\n"), 0o600))
	require.NoError(t, runPropose(catalogFlags{config: filepath.Join(dir, "veto.yaml")}, out))
	raw, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "invoices.get")
	assert.NotContains(t, string(raw), "old")
}

func TestProposeNamesTheDraftedRun(t *testing.T) {
	tasks := []*flow.Definition{{
		Name:  "read-orders-get-customers-get",
		Steps: []flow.Step{{Operation: "orders.get"}},
	}}
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID:     "orders.get",
		Params: []catalog.Param{{Name: "id", In: "path", Required: true}},
	}}}
	lines := proposeCommands("/tmp/flow.yaml", "draft.yaml", cat, tasks)
	assert.Equal(t, []string{"veto run --flow draft.yaml read-orders-get-customers-get --param id="}, lines)
	same := proposeCommands("flow.yaml", "flow.yaml", cat, tasks)
	assert.Equal(t, []string{"veto run read-orders-get-customers-get --param id="}, same)
}

const proposeOrders = `
openapi: 3.0.3
info: {title: Orders, version: "1"}
paths:
  /orders/{id}:
    get:
      operationId: orders.get
      summary: Get order
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      responses:
        "200":
          description: One order
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Order"
components:
  schemas:
    Order:
      type: object
      properties:
        id: {type: string}
        customerId: {type: string}
`

const proposeInvoices = `
openapi: 3.0.3
info: {title: Invoices, version: "1"}
paths:
  /invoices/{id}:
    get:
      operationId: invoices.get
      summary: Get invoice
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      responses:
        "200":
          description: One invoice
          content:
            application/json:
              schema:
                type: object
                properties:
                  amount: {type: string}
`
