package eval_test

import (
	"context"
	"strings"
	"testing"

	"os"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/eval"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/semantics"
)

func TestDeleteEvalCasePasses(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c, err := eval.LoadCase("../testdata/delete.yaml")
	if err != nil {
		t.Fatal(err)
	}
	r := &eval.Runner{
		Catalog:   cat,
		Semantics: semantics.NewDerived(cat),
	}
	if err := r.Run(context.Background(), c); err != nil {
		t.Fatal(err)
	}
}

func TestNeighborCaseSelectsTheRelationAndDropsBilling(t *testing.T) {
	cat := threeAPIs(t)
	if cat.ByID("billing.invoice") == nil {
		t.Fatal("billing.invoice missing from the catalog")
	}
	cases, err := eval.LoadCases([]string{"../testdata/cases"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 || cases[0].Name != "delete-requires-confirmation" || cases[1].Name != "holding-selects-neighbor" {
		t.Fatalf("cases: %#v", names(cases))
	}
	sem := semantics.NewDerived(cat)
	loop, err := agent.New(cat, sem, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &eval.Runner{Loop: loop}
	for _, c := range cases {
		if err := r.Run(context.Background(), c); err != nil {
			t.Fatal(err)
		}
	}
	bad := *cases[1]
	bad.Expect.PackExcludes = []string{"assets.get"}
	if err := r.Run(context.Background(), &bad); err == nil || !strings.Contains(err.Error(), "assets.get") {
		t.Fatalf("exclude: %v", err)
	}
}

func threeAPIs(t *testing.T) *catalog.Catalog {
	t.Helper()
	var parts []*catalog.Catalog
	for _, path := range []string{"../testdata/openapi.yaml", "../testdata/teams.yaml", "../testdata/billing.yaml"} {
		cat, err := openapi.Load(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, cat)
	}
	cat, err := catalog.Merge(parts...)
	if err != nil {
		t.Fatal(err)
	}
	relData, err := os.ReadFile("../testdata/relations.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rels, err := catalog.ParseRelations(relData)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.ApplyRelations(cat, rels); err != nil {
		t.Fatal(err)
	}
	return cat
}

func names(cases []*eval.Case) []string {
	out := make([]string, len(cases))
	for i, c := range cases {
		out[i] = c.Name
	}
	return out
}
