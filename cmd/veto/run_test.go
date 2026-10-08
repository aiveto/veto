package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/eval"
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

func TestSavedCheckRecordsTheContractWithoutTheResponse(t *testing.T) {
	def := &flow.Definition{
		Name:     "investigate-charge",
		Question: "Investigate a customer's disputed charge",
		Answer:   []string{"amount", "currency"},
		Steps: []flow.Step{
			{Operation: "orders.get", Output: "invoiceId", To: "id"},
			{Operation: "invoices.get"},
		},
	}
	body, err := savedCheck(def)
	require.NoError(t, err)
	text := string(body)
	assert.NotContains(t, text, "inv_2291")
	assert.NotContains(t, text, "cus_mara")
	assert.Contains(t, text, "answer: amount, currency")
	assert.Contains(t, text, "orders.get invoiceId -> invoices.get id")

	dir := t.TempDir()
	path := filepath.Join(dir, "investigate-charge.yaml")
	require.NoError(t, os.WriteFile(path, body, 0o600))
	cases, err := eval.LoadCases([]string{path})
	require.NoError(t, err)
	require.Len(t, cases, 1)
	assert.Equal(t, def.Question, cases[0].Input)
	assert.Equal(t, []string{
		"task investigate-charge: Investigate a customer's disputed charge. answer: amount, currency",
		"orders.get invoiceId -> invoices.get id",
	}, cases[0].Expect.PackContains)
	require.ErrorContains(t, writeSavedCheck(path, def), "exists")
}

func TestRunRefusesAFlowThatIsNotATask(t *testing.T) {
	err := runTask(catalogFlags{config: filepath.Join("..", "..", "testdata", "veto.yaml")}, "list-then-get", nil, "")
	require.EqualError(t, err, "unknown task list-then-get")
}
