package eval_test

import (
	"context"
	"testing"

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
