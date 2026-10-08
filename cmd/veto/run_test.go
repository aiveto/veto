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

func TestTaskParamsKeepTheValueOffTheError(t *testing.T) {
	_, err := taskParams([]string{"=secret"})
	require.EqualError(t, err, "param needs a name and a value")
	assert.NotContains(t, err.Error(), "secret")

	_, err = taskParams([]string{"id=10482", "id=9"})
	require.EqualError(t, err, "param id is repeated")
	assert.NotContains(t, err.Error(), "10482")
}

func TestSavedCheckWritesTheReviewedTask(t *testing.T) {
	cat := &catalog.Catalog{
		Operations: []catalog.Operation{
			{ID: "orders.get", Kind: catalog.KindRead, ResponseFields: []string{"invoiceId"}},
			{
				ID: "invoices.get", Kind: catalog.KindRead, ResponseFields: []string{"amount", "currency"},
				Params: []catalog.Param{{Name: "id", In: "path", Required: true}},
			},
		},
		Links: []catalog.OpLink{{From: "orders.get", To: "invoices.get", Note: "Order.invoiceId"}},
	}
	taught, err := flow.Teach(&flow.Definition{
		Name:     "investigate-charge",
		Question: "Investigate a customer's disputed charge",
		Answer:   []string{"amount", "currency"},
		Steps:    []flow.Step{{Operation: "orders.get"}, {Operation: "invoices.get"}},
	}, cat)
	require.NoError(t, err)
	dir := t.TempDir()
	path := filepath.Join(dir, "investigate-charge.yaml")
	require.NoError(t, writeSavedCheck(path, taught, cat))
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	want, err := flow.Format([]*flow.Definition{flow.Review(taught, cat)})
	require.NoError(t, err)
	assert.Equal(t, string(want), string(body))
	assert.NotContains(t, string(body), "output:")
	assert.NotContains(t, string(body), "inv_2291")
	require.ErrorContains(t, writeSavedCheck(path, taught, cat), "exists")
}

func TestRunRefusesAFlowThatIsNotATask(t *testing.T) {
	err := runTask(catalogFlags{config: filepath.Join("..", "..", "testdata", "veto.yaml")}, "list-then-get", nil, "")
	require.EqualError(t, err, "unknown task list-then-get")
}
