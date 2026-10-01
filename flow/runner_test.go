package flow_test

import (
	"context"
	"maps"
	"testing"

	"github.com/aiveto/veto/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStepOutputFeedsTheNextParameter(t *testing.T) {
	def := &flow.Definition{Name: "get-customer", Steps: []flow.Step{
		{Operation: "orders.get", Output: "customerId", To: "id"},
		{Operation: "customers.get"},
	}}
	var got []map[string]string
	runner := flow.Runner{Invoke: func(ctx context.Context, operationID string, params map[string]string, approvalID string) (string, string, error) {
		got = append(got, clone(params))
		if operationID == "orders.get" {
			return "ok", `{"customerId":"7"}`, nil
		}
		return "ok", `{"id":"7"}`, nil
	}}
	results, err := runner.Run(context.Background(), def, map[string]string{"id": "1"})
	require.NoError(t, err)
	assert.Len(t, results, 2)
	require.Len(t, got, 2)
	assert.Equal(t, "7", got[1]["id"])
}

func TestConfirmationStopsTheNextStep(t *testing.T) {
	def := &flow.Definition{Name: "then-delete", Steps: []flow.Step{
		{Operation: "orders.get", Output: "id", To: "id"},
		{Operation: "orders.delete"},
	}}
	var called []string
	runner := flow.Runner{Invoke: func(ctx context.Context, operationID string, params map[string]string, approvalID string) (string, string, error) {
		called = append(called, operationID)
		if operationID == "orders.delete" {
			return "confirmation_required", "", nil
		}
		return "ok", `{"id":"7"}`, nil
	}}
	_, err := runner.Run(context.Background(), def, nil)
	var stopped flow.StoppedError
	require.ErrorAs(t, err, &stopped)
	assert.Equal(t, "orders.delete", stopped.Operation)
	assert.Equal(t, []string{"orders.get", "orders.delete"}, called)
}

func TestParseRejectsUnknownFieldsAndExtraDocuments(t *testing.T) {
	_, err := flow.Parse([]byte("name: get\nsteps:\n  - operation: orders.get\n    confirmaton: true\n"))
	require.Error(t, err)
	_, err = flow.Parse([]byte("name: get\nname: again\nsteps: []\n"))
	require.Error(t, err)
	_, err = flow.Parse([]byte("name: get\nsteps: [orders.get]\n---\nname: other\n"))
	require.Error(t, err)
	def, err := flow.Parse([]byte("name: get\nsteps:\n  - orders.get\n"))
	require.NoError(t, err)
	require.Len(t, def.Steps, 1)
	assert.Equal(t, "orders.get", def.Steps[0].Operation)
}

func clone(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}
