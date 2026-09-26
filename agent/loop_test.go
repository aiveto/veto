package agent_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/flow"
	"github.com/aiveto/veto/model"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/semantics"
)

func TestDeleteLoopStopsBeforeHTTPAndPacksOverlay(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	base := semantics.NewDerived(cat)
	sem, err := semantics.LoadOverlay("../testdata/semantics.yaml", base)
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer ts.Close()

	loop := &agent.Loop{
		Catalog:   cat,
		Semantics: sem,
		Model:     model.NewScripted(),
		Policy:    policy.Builtin{},
		State:     policy.NewState(),
		Exec:      execute.Client{BaseURL: ts.URL},
	}
	out, err := loop.Run(context.Background(), "Delete asset 123")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "confirmation_required" || out.OperationID != "assets.delete" {
		t.Fatalf("status %q operation %q", out.Status, out.OperationID)
	}
	if hits.Load() != 0 {
		t.Fatalf("HTTP ran before approval, hits=%d", hits.Load())
	}
	ser := out.Pack.Serialize()
	if runctx.ContainsRawSpec(ser) {
		t.Fatal("pack contains the raw spec")
	}
	if !strings.Contains(out.Pack.DescribedDetail, "Permanently remove") {
		t.Fatalf("overlay sentence missing from pack: %s", out.Pack.DescribedDetail)
	}
	items, err := loop.Memory.Search(context.Background(), "Delete asset 123")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one memory item, got %d", len(items))
	}
}

type flowPick struct{}

func (flowPick) Complete(ctx context.Context, req model.Request) (model.Response, error) {
	if strings.HasPrefix(req.UserMessage, "ok") {
		return model.Response{}, nil
	}
	return model.Response{FlowName: "list-then-get", Params: map[string]string{"id": "9"}}, nil
}

func TestLoopRunsNamedFlow(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	def, err := flow.Load("../testdata/flow.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	loop := &agent.Loop{
		Catalog:   cat,
		Semantics: semantics.NewDerived(cat),
		Model:     flowPick{},
		Exec:      execute.Client{BaseURL: ts.URL, HTTP: ts.Client()},
		Flows:     map[string]*flow.Definition{def.Name: def},
	}
	out, err := loop.Run(context.Background(), "list assets")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "ok" || out.OperationID != "assets.get" {
		t.Fatalf("status %q operation %q", out.Status, out.OperationID)
	}
	if hits.Load() != 2 {
		t.Fatalf("expected two HTTP calls, hits=%d", hits.Load())
	}
}
