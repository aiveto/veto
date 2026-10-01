package eval_test

import (
	"context"
	"testing"

	"os"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/eval"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/result"
	"github.com/aiveto/veto/semantics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type (
	pickGet struct{}
	hitExec struct{}
)

func (pickGet) Complete(context.Context, agent.Request) (agent.Response, error) {
	return agent.Response{OperationID: "orders.get", Params: map[string]string{"id": "1"}}, nil
}

func (hitExec) InvokeHTTPResult(context.Context, *catalog.Operation, map[string]string) (result.HTTPResult, error) {
	return result.HTTPResult{Status: 200, Code: "ok"}, nil
}

func TestNeighborCaseSelectsTheRelationAndDropsBilling(t *testing.T) {
	cat := threeAPIs(t)
	cases, err := eval.LoadCases([]string{"../testdata/cases"})
	require.NoError(t, err)
	assert.Equal(t, []string{"delete-requires-confirmation", "order-selects-neighbor"}, names(cases))
	sem := semantics.NewDerived(cat)
	loop, err := agent.New(cat, sem, nil)
	require.NoError(t, err)
	r := &eval.Runner{Loop: loop}
	for _, c := range cases {
		require.NoError(t, r.Run(context.Background(), c))
	}
	require.Len(t, cases, 2)
	bad := *cases[1]
	bad.Expect.PackExcludes = []string{"orders.get"}
	err = r.Run(context.Background(), &bad)
	assert.ErrorContains(t, err, "orders.get")
}

func threeAPIs(t *testing.T) *catalog.Catalog {
	t.Helper()
	parts := make([]*catalog.Catalog, 0, 3)
	for _, path := range []string{"../testdata/orders.yaml", "../testdata/customers.yaml", "../testdata/billing.yaml"} {
		cat, err := openapi.Load(context.Background(), path)
		require.NoError(t, err)
		parts = append(parts, cat)
	}
	cat, err := catalog.Merge(parts...)
	require.NoError(t, err)
	relData, err := os.ReadFile("../testdata/relations.yaml")
	require.NoError(t, err)
	rels, err := catalog.ParseRelations(relData)
	require.NoError(t, err)
	require.NoError(t, catalog.ApplyRelations(cat, rels))
	return cat
}

func TestNoHTTPFailsWhenTheCallRuns(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	loop, err := agent.New(cat, semantics.NewDerived(cat), hitExec{})
	require.NoError(t, err)
	loop.Model = pickGet{}
	err = (&eval.Runner{Loop: loop}).Run(context.Background(), &eval.Case{
		Name:  "get",
		Input: "get order 1",
		Expect: eval.Expectations{
			OperationID: "orders.get",
			NoHTTP:      true,
		},
	})
	assert.ErrorContains(t, err, "http ran")
}

func names(cases []*eval.Case) []string {
	out := make([]string, len(cases))
	for i, c := range cases {
		out[i] = c.Name
	}
	return out
}
