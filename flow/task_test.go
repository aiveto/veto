package flow_test

import (
	"context"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTeachBindsAReadOnlyTaskFromTheRelation(t *testing.T) {
	def := &flow.Definition{
		Name:     "investigate-charge",
		Question: "Investigate a customer's disputed charge",
		Answer:   []string{"amount", "currency"},
		Steps:    []flow.Step{{Operation: "orders.get"}, {Operation: "invoices.get"}},
	}
	taught, err := flow.Teach(def, chargeCatalog())
	require.NoError(t, err)
	require.Len(t, taught.Steps, 2)
	assert.Equal(t, "invoiceId", taught.Steps[0].Output)
	assert.Equal(t, "id", taught.Steps[0].To)
	assert.Equal(t, []string{"orders.get invoiceId -> invoices.get id"}, taught.Binds())

	var got []map[string]string
	runner := flow.Runner{Invoke: func(ctx context.Context, operationID string, params map[string]string, approvalID string) (string, string, error) {
		got = append(got, clone(params))
		if operationID == "orders.get" {
			return "ok", `{"invoiceId":"inv_2291","customerId":"cus_mara"}`, nil
		}
		return "ok", `{"amount":"10.00","currency":"USD"}`, nil
	}}
	results, err := runner.Run(context.Background(), taught, map[string]string{"id": "10482"})
	require.NoError(t, err)
	assert.Equal(t, []string{"ok", "ok"}, results)
	require.Len(t, got, 2)
	assert.Equal(t, "inv_2291", got[1]["id"])
}

func TestTeachRefusesATaskTheContractCannotSupport(t *testing.T) {
	cat := chargeCatalog()
	base := &flow.Definition{
		Name:     "investigate-charge",
		Question: "Investigate a customer's disputed charge",
		Answer:   []string{"amount", "currency"},
		Steps:    []flow.Step{{Operation: "orders.get"}, {Operation: "invoices.get"}},
	}
	cat.Operations[0].ResponseFields = []string{"customerId"}
	_, err := flow.Teach(base, cat)
	require.EqualError(t, err, "task investigate-charge can no longer obtain invoiceId from orders.get")

	cat = chargeCatalog()
	cat.Operations[1].ResponseFields = []string{"currency"}
	_, err = flow.Teach(base, cat)
	require.EqualError(t, err, "task investigate-charge can no longer read amount from invoices.get")

	cat = chargeCatalog()
	cat.Links = nil
	_, err = flow.Teach(base, cat)
	require.EqualError(t, err, "task investigate-charge: no relation binds orders.get to invoices.get")

	cat = chargeCatalog()
	cat.Links = append(cat.Links, catalog.OpLink{From: "orders.get", To: "invoices.get", Note: "Order.customerId"})
	_, err = flow.Teach(base, cat)
	require.EqualError(t, err, "task investigate-charge: orders.get has more than one relation to invoices.get; set output")

	explicit := *base
	explicit.Steps = []flow.Step{{Operation: "orders.get", Output: "invoiceId", To: "id"}, {Operation: "invoices.get"}}
	taught, err := flow.Teach(&explicit, cat)
	require.NoError(t, err)
	assert.Equal(t, "invoiceId", taught.Steps[0].Output)

	cat = chargeCatalog()
	cat.Operations[1].Kind = catalog.KindDelete
	_, err = flow.Teach(base, cat)
	require.EqualError(t, err, "task investigate-charge: invoices.get is not read-only")
}

func TestTeachLeavesAFlowWithoutAQuestion(t *testing.T) {
	def := &flow.Definition{Name: "then-delete", Steps: []flow.Step{{Operation: "orders.get"}, {Operation: "orders.delete"}}}
	got, err := flow.Teach(def, chargeCatalog())
	require.NoError(t, err)
	assert.Same(t, def, got)
	assert.Empty(t, got.Steps[0].Output)
}

func TestFindMatchesTheQuestionNotTheOperation(t *testing.T) {
	taught, err := flow.Teach(&flow.Definition{
		Name:     "investigate-charge",
		Question: "Investigate a customer's disputed charge",
		Answer:   []string{"amount", "currency"},
		Steps:    []flow.Step{{Operation: "orders.get"}, {Operation: "invoices.get"}},
	}, chargeCatalog())
	require.NoError(t, err)
	flows := map[string]*flow.Definition{taught.Name: taught, "list": {Name: "list", Steps: []flow.Step{{Operation: "orders.list"}}}}
	require.Len(t, flow.Find(flows, "disputed charge"), 1)
	assert.Empty(t, flow.Find(flows, "orders.get"))
	assert.Contains(t, flow.Lines(flows)[0], "answer: amount, currency")
}

func TestTaskRegressionsNameTheBrokenBinding(t *testing.T) {
	taught, err := flow.Teach(&flow.Definition{
		Name:     "investigate-charge",
		Question: "Investigate a customer's disputed charge",
		Answer:   []string{"amount", "currency"},
		Steps:    []flow.Step{{Operation: "orders.get"}, {Operation: "invoices.get"}},
	}, chargeCatalog())
	require.NoError(t, err)
	base := flow.Lines(map[string]*flow.Definition{taught.Name: taught})
	broken := "task investigate-charge: Investigate a customer's disputed charge. answer: currency"
	assert.Equal(t, []string{
		"task investigate-charge can no longer obtain invoiceId from orders.get",
		"task investigate-charge can no longer read amount from invoices.get",
	}, flow.TaskRegressions(base, []string{broken}))
	assert.Empty(t, flow.TaskRegressions(nil, base))
}

func TestParseReadsTheQuestion(t *testing.T) {
	def, err := flow.Parse([]byte("name: investigate-charge\nquestion: Investigate a customer's disputed charge\nanswer: [amount, currency]\nsteps:\n  - orders.get\n  - invoices.get\n"))
	require.NoError(t, err)
	assert.Equal(t, []string{"amount", "currency"}, def.Answer)
	assert.Equal(t, "Investigate a customer's disputed charge", def.Question)
}

func TestParseFileKeepsAFlowBesideATask(t *testing.T) {
	defs, err := flow.ParseFile([]byte("flows:\n  - name: list-then-get\n    steps: [orders.list, orders.get]\n  - name: investigate-charge\n    question: Investigate a customer's disputed charge\n    answer: [amount, currency]\n    steps: [orders.get, invoices.get]\n"))
	require.NoError(t, err)
	require.Len(t, defs, 2)
	assert.Empty(t, defs[0].Question)
	assert.Equal(t, "investigate-charge", defs[1].Name)
	_, err = flow.ParseFile([]byte("name: get\nflows: []\n"))
	require.Error(t, err)
	_, err = flow.ParseFile([]byte("flows:\n  - name: a\n    steps: [orders.get]\n  - name: a\n    steps: [orders.get]\n"))
	require.Error(t, err)
}

func chargeCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		Operations: []catalog.Operation{
			{ID: "orders.get", Kind: catalog.KindRead, ResponseFields: []string{"customerId", "invoiceId"}},
			{
				ID: "invoices.get", Kind: catalog.KindRead, ResponseFields: []string{"amount", "currency"},
				Params: []catalog.Param{{Name: "id", In: "path", Required: true}},
			},
		},
		Links: []catalog.OpLink{{From: "orders.get", To: "invoices.get", Note: "Order.invoiceId"}},
	}
}
