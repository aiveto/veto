package agent_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/flow"
	"github.com/aiveto/veto/memory"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/semantics"
)

type flowPick struct{}

func (flowPick) Complete(ctx context.Context, req agent.Request) (agent.Response, error) {
	if strings.HasPrefix(req.UserMessage, "ok") {
		return agent.Response{}, nil
	}
	return agent.Response{FlowName: "list-then-get", Params: map[string]string{"id": "9"}}, nil
}

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
		Model:     agent.NewScripted(),
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
	if out.Text != "confirm assets.delete id=123" || !strings.Contains(out.Pack.Serialize(), out.Text) {
		t.Fatalf("confirmation sentence: %q\n%s", out.Text, out.Pack.Serialize())
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

type getPick struct{}

func (getPick) Complete(ctx context.Context, req agent.Request) (agent.Response, error) {
	return agent.Response{OperationID: "assets.get", Params: map[string]string{"id": "1"}}, nil
}

type capture struct{ saw string }

func (c *capture) Complete(ctx context.Context, req agent.Request) (agent.Response, error) {
	c.saw = req.Context
	return agent.Response{}, fmt.Errorf("stop")
}

func TestRecentTurnsStayInThePack(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	loop := agent.New(cat, nil, nil)
	if err := loop.Memory.Store(context.Background(), memory.Item{ID: "old", Content: "earlier turn about widgets"}); err != nil {
		t.Fatal(err)
	}
	cap := &capture{}
	loop.Model = cap
	_, err = loop.Run(context.Background(), "brand new question")
	if err == nil {
		t.Fatal("expected the completer to stop the turn")
	}
	if !strings.Contains(cap.saw, "earlier turn about widgets") {
		t.Fatalf("recent turn missing:\n%s", cap.saw)
	}
}

func TestLoopShowsStableHTTPCode(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer ts.Close()
	loop := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	loop.Model = getPick{}
	out, err := loop.Run(context.Background(), "get asset 1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Text, "code=upstream") || !strings.Contains(out.Text, "retryable=true") {
		t.Fatalf("text: %s", out.Text)
	}
	if out.Text == "down" {
		t.Fatal("model saw only the raw body")
	}
}

func TestWrapPolicyKeepsBuiltinUnlessItStops(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer ts.Close()
	loop := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	loop.WrapPolicy(func(ctx context.Context, op *catalog.Operation) (policy.Decision, bool, error) {
		return policy.DecisionAllow, false, nil
	})
	out, err := loop.Invoke(context.Background(), "assets.delete", map[string]string{"id": "123"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "confirmation_required" || hits.Load() != 0 {
		t.Fatalf("status %q hits %d", out.Status, hits.Load())
	}
	loop.WrapPolicy(func(ctx context.Context, op *catalog.Operation) (policy.Decision, bool, error) {
		return policy.DecisionAllow, true, nil
	})
	out, err = loop.Invoke(context.Background(), "assets.delete", map[string]string{"id": "123"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "ok" || hits.Load() != 1 {
		t.Fatalf("status %q hits %d", out.Status, hits.Load())
	}
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
