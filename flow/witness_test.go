package flow_test

import (
	"context"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWitnessNamesWhatTheRunKeptTrue(t *testing.T) {
	taught := taughtCharge(t)
	bodies := []string{
		`{"invoiceId":"inv_2291","customerId":"cus_mara"}`,
		`{"amount":"10.00","currency":"USD"}`,
	}
	lines, err := flow.Witness(taught, chargeCatalog(), bodies)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"orders.get supplied invoiceId",
		"invoices.get accepted id",
		"invoices.get included amount, currency",
		"investigate-charge stayed read-only",
	}, lines)
	joined := lines[0] + lines[1] + lines[2] + lines[3]
	assert.NotContains(t, joined, "inv_2291")
	assert.NotContains(t, joined, "cus_mara")
	assert.NotContains(t, joined, "10.00")
}

func TestWitnessNamesTheFieldTheResponseDropped(t *testing.T) {
	taught := taughtCharge(t)
	_, err := flow.Witness(taught, chargeCatalog(), []string{
		`{"customerId":"cus_mara"}`,
		`{"amount":"10.00","currency":"USD"}`,
	})
	require.EqualError(t, err, "task investigate-charge can no longer obtain invoiceId from orders.get")

	_, err = flow.Witness(taught, chargeCatalog(), []string{
		`{"invoiceId":"inv_2291"}`,
		`{"currency":"USD"}`,
	})
	require.EqualError(t, err, "task investigate-charge can no longer read amount from invoices.get")
}

func TestRunFailureNamesARefusedNextCall(t *testing.T) {
	taught := taughtCharge(t)
	err := flow.RunFailure(taught, flow.StoppedError{Status: "error", Operation: "invoices.get"})
	require.EqualError(t, err, "task investigate-charge: invoices.get did not accept id")

	err = flow.RunFailure(taught, flow.StoppedError{Status: "denied", Operation: "invoices.get", Why: "policy"})
	require.EqualError(t, err, "task investigate-charge: invoices.get did not accept id: policy")

	err = flow.RunFailure(taught, flow.MissingOutputError{Operation: "orders.get", Field: "invoiceId"})
	require.EqualError(t, err, "task investigate-charge can no longer obtain invoiceId from orders.get")
	assert.NotContains(t, err.Error(), "cus_mara")
}

func TestWitnessRefusesAStepThatIsNotARead(t *testing.T) {
	taught := taughtCharge(t)
	cat := chargeCatalog()
	cat.Operations[1].Kind = catalog.KindDelete
	_, err := flow.Witness(taught, cat, []string{`{"invoiceId":"inv_2291"}`, `{"amount":"10.00","currency":"USD"}`})
	require.EqualError(t, err, "task investigate-charge: invoices.get is not read-only")
}

func TestRunStopsWhenTheResponseOmitsTheBinding(t *testing.T) {
	taught := taughtCharge(t)
	runner := flow.Runner{Invoke: func(ctx context.Context, operationID string, params map[string]string, approvalID string) (string, string, error) {
		return "ok", `{"customerId":"cus_mara"}`, nil
	}}
	_, err := runner.Run(context.Background(), taught, map[string]string{"id": "10482"})
	require.EqualError(t, flow.RunFailure(taught, err), "task investigate-charge can no longer obtain invoiceId from orders.get")
}

func taughtCharge(t *testing.T) *flow.Definition {
	t.Helper()
	taught, err := flow.Teach(&flow.Definition{
		Name:     "investigate-charge",
		Question: "Investigate a customer's disputed charge",
		Answer:   []string{"amount", "currency"},
		Steps:    []flow.Step{{Operation: "orders.get"}, {Operation: "invoices.get"}},
	}, chargeCatalog())
	require.NoError(t, err)
	return taught
}
