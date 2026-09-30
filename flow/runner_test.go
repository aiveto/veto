package flow_test

import (
	"context"
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
	var stopped flow.Stopped
	require.ErrorAs(t, err, &stopped)
	assert.Equal(t, "orders.delete", stopped.Operation)
	assert.Equal(t, []string{"orders.get", "orders.delete"}, called)
}

func clone(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
