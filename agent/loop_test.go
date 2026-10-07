package agent_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/flow"
	"github.com/aiveto/veto/memory"
	"github.com/aiveto/veto/opa"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/runtime"
	"github.com/aiveto/veto/semantics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type flowPick struct{}

func (flowPick) Complete(ctx context.Context, req agent.Request) (agent.Response, error) {
	if strings.HasPrefix(req.UserMessage, "ok") {
		return agent.Response{}, nil
	}
	return agent.Response{FlowName: "list-then-get", Params: map[string]string{"id": "9"}}, nil
}

func TestDeleteLoopStopsBeforeHTTPAndPacksOverlay(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	base := semantics.New(cat)
	overlay, err := os.ReadFile("../testdata/semantics.yaml")
	require.NoError(t, err)
	sem, err := semantics.ParseOverlay(overlay, base)
	require.NoError(t, err)
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
		Memory:    memory.New(),
		Packs:     runctx.NewBuilder(0),
	}
	out, err := loop.Run(context.Background(), "Delete order 123")
	require.NoError(t, err)
	assert.Equal(t, "confirmation_required", out.Status)
	assert.Equal(t, "orders.delete", out.OperationID)
	assert.Equal(t, "confirm orders.delete id=123", out.Text)
	assert.Contains(t, out.Pack.Serialize(), out.Text)
	assert.Equal(t, int32(0), hits.Load())
	assert.False(t, runctx.ContainsRawSpec(out.Pack.Serialize()))
	assert.Contains(t, out.Pack.DescribedDetail, "Permanently remove")
	items, err := loop.Memory.Search(context.Background(), "Delete order 123")
	require.NoError(t, err)
	assert.Len(t, items, 1)
}

type getPick struct{}

func (getPick) Complete(ctx context.Context, req agent.Request) (agent.Response, error) {
	return agent.Response{OperationID: "orders.get", Params: map[string]string{"id": "1"}}, nil
}

type capture struct{ saw string }

func (c *capture) Complete(ctx context.Context, req agent.Request) (agent.Response, error) {
	c.saw = req.Context
	return agent.Response{}, errors.New("stop")
}

func TestRecentTurnsStayInThePack(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	loop, err := agent.New(cat, nil, nil)
	require.NoError(t, err)
	require.NoError(t, loop.Memory.Store(context.Background(), memory.Item{ID: "old", Content: "earlier turn about widgets"}))
	model := &capture{}
	loop.Model = model
	_, err = loop.Run(context.Background(), "brand new question")
	require.Error(t, err)
	assert.Contains(t, model.saw, "earlier turn about widgets")
}

func TestLoopFollowsPagesLikeTheKernel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`openapi: 3.0.3
info: {title: Orders, version: "1"}
paths:
  /orders:
    get:
      operationId: orders.list
      parameters:
        - name: cursor
          in: query
          schema: {type: string}
      responses:
        "200":
          description: page
          content:
            application/json:
              schema:
                type: object
                properties:
                  items: {type: array, items: {type: object}}
                  next: {type: string}
          links:
            next:
              operationId: orders.list
              parameters:
                cursor: $response.body#/next
`), 0o600))
	cat, err := openapi.Load(context.Background(), path)
	require.NoError(t, err)
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("cursor") == "b" {
			_, _ = w.Write([]byte(`{"items":[{"id":"2"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"1"}],"next":"b"}`))
	}))
	defer ts.Close()
	loop, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	require.NoError(t, err)
	one, err := loop.Invoke(context.Background(), "orders.list", nil, "")
	require.NoError(t, err)
	assert.Equal(t, int32(1), hits.Load())
	assert.JSONEq(t, `{"items":[{"id":"1"}],"next":"b"}`, one.Body)
	loop.Pages = 5
	hits.Store(0)
	many, err := loop.Invoke(context.Background(), "orders.list", nil, "")
	require.NoError(t, err)
	assert.Equal(t, int32(2), hits.Load())
	assert.JSONEq(t, `[{"id":"1"},{"id":"2"}]`, many.Body)
}

func TestSetRuntimeIsWhatInvokeUses(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	rt := &runtime.Runtime{
		Catalog: cat,
		Exec:    execute.Client{BaseURL: ts.URL},
		Policy:  policy.Builtin{},
		State:   policy.NewState(),
		Gate:    &runtime.InvokeGate{},
	}
	loop, err := agent.New(cat, nil, nil)
	require.NoError(t, err)
	loop.SetRuntime(rt)
	_, err = loop.Invoke(context.Background(), "orders.get", map[string]string{"id": "1"}, "")
	require.NoError(t, err)
	assert.Equal(t, int32(1), hits.Load())
	rt.Pages = 4
	assert.Equal(t, 4, loop.RuntimePtr().Pages)
	assert.Same(t, rt, loop.RuntimePtr())
}

func TestLoopKeepsTheAPIResult(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"7","total":3}`))
	}))
	defer ts.Close()
	loop, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	require.NoError(t, err)
	loop.Model = getPick{}
	out, err := loop.Run(context.Background(), "get order 1")
	require.NoError(t, err)
	assert.Contains(t, out.Result.Body, `"id":"7"`)
	assert.Contains(t, out.Result.Body, `"total":3`)
	assert.True(t, out.Result.Sent)
	assert.True(t, out.Result.HTTP)
	assert.Contains(t, out.Pack.Serialize(), `"id":"7"`)
	assert.Contains(t, out.Pack.Serialize(), `"total":3`)
}

func TestLoopKeepsSendEvidenceWhenTransportFails(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	base := ts.URL
	ts.Close()
	loop, err := agent.New(cat, nil, execute.Client{BaseURL: base})
	require.NoError(t, err)
	loop.Model = getPick{}
	out, err := loop.Run(context.Background(), "get order 1")
	require.Error(t, err)
	assert.True(t, out.Result.Sent)
	assert.Equal(t, "orders.get", out.OperationID)
	assert.Equal(t, "orders.get", out.Result.OperationID)
}

func TestLoopShowsStableHTTPCode(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer ts.Close()
	loop, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	require.NoError(t, err)
	loop.Model = getPick{}
	out, err := loop.Run(context.Background(), "get order 1")
	require.NoError(t, err)
	assert.Contains(t, out.Text, "code=upstream")
	assert.Contains(t, out.Text, "retryable=true")
	assert.NotEqual(t, "down", out.Text)
}

func TestWrapPolicyKeepsBuiltinUnlessItStops(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	cases := []struct {
		name   string
		stop   bool
		status string
		hits   int32
	}{
		{name: "falls through to confirmation", status: "confirmation_required"},
		{name: "stop still confirms a delete", stop: true, status: "confirmation_required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
			}))
			defer ts.Close()
			loop, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
			require.NoError(t, err)
			loop.WrapPolicy(func(ctx context.Context, op *catalog.Operation) (policy.Decision, bool, error) {
				return policy.DecisionAllow, tc.stop, nil
			})
			out, err := loop.Invoke(context.Background(), "orders.delete", map[string]string{"id": "123"}, "")
			require.NoError(t, err)
			assert.Equal(t, tc.status, out.Status)
			assert.Equal(t, tc.hits, hits.Load())
		})
	}
}

type deleteFlow struct{}

func (deleteFlow) Complete(context.Context, agent.Request) (agent.Response, error) {
	return agent.Response{FlowName: "get-then-delete", Params: map[string]string{"id": "9"}}, nil
}

func TestPausedFlowKeepsTheApprovalID(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	def := &flow.Definition{Name: "get-then-delete", Steps: []flow.Step{
		{Operation: "orders.get"},
		{Operation: "orders.delete"},
	}}
	var methods []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	loop := &agent.Loop{
		Catalog:   cat,
		Semantics: semantics.New(cat),
		Model:     deleteFlow{},
		Policy:    policy.Builtin{},
		State:     policy.NewState(),
		Exec:      execute.Client{BaseURL: ts.URL, HTTP: ts.Client()},
		Flows:     map[string]*flow.Definition{def.Name: def},
		Packs:     runctx.NewBuilder(0),
	}
	out, err := loop.Run(context.Background(), "get order 9 then delete it")
	require.NoError(t, err)
	assert.Equal(t, "confirmation_required", out.Status)
	assert.Equal(t, "orders.delete", out.OperationID)
	assert.NotEmpty(t, out.ApprovalID)
	pending := loop.State.Pending(t.Context(), out.ApprovalID)
	require.NotNil(t, pending)
	assert.Equal(t, "orders.delete", pending.OperationID)
	assert.Equal(t, "9", pending.Params["id"])
	assert.Equal(t, []string{http.MethodGet}, methods)
}

func TestLoopRunsNamedFlow(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	flowData, err := os.ReadFile("../testdata/flow.yaml")
	require.NoError(t, err)
	def, err := flow.Parse(flowData)
	require.NoError(t, err)
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	loop := &agent.Loop{
		Catalog:   cat,
		Semantics: semantics.New(cat),
		Model:     flowPick{},
		Exec:      execute.Client{BaseURL: ts.URL, HTTP: ts.Client()},
		Flows:     map[string]*flow.Definition{def.Name: def},
		State:     policy.NewState(),
		Packs:     runctx.NewBuilder(0),
	}
	out, err := loop.Run(context.Background(), "list orders")
	require.NoError(t, err)
	assert.Equal(t, "ok", out.Status)
	assert.Equal(t, "orders.get", out.OperationID)
	assert.Equal(t, int32(2), hits.Load())
}

func TestAgentCannotApproveItsOwnCall(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	secret := []byte("approval-secret")
	dir := t.TempDir()
	first, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	require.NoError(t, err)
	first.State.SetNonceDir(dir)
	require.NoError(t, first.State.SetSigner(secret, time.Hour))
	out, err := first.Run(context.Background(), "Delete order 123")
	require.NoError(t, err)
	assert.Equal(t, "confirmation_required", out.Status)
	assert.Equal(t, "confirm orders.delete id=123", out.Text)
	assert.NotEmpty(t, out.ApprovalID)
	assert.False(t, strings.HasPrefix(out.ApprovalID, "v1."))
	assert.NotNil(t, first.State.Pending(t.Context(), out.ApprovalID))
	_, err = first.Invoke(context.Background(), "orders.delete", map[string]string{"id": "123"}, out.ApprovalID)
	require.Error(t, err)
	assert.Equal(t, int32(0), hits.Load())
	approver, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	require.NoError(t, err)
	approver.State.SetNonceDir(dir)
	require.NoError(t, approver.State.SetSigner(secret, time.Hour))
	approved, err := approver.State.Approve(t.Context(), out.ApprovalID)
	require.NoError(t, err)
	assert.NotEqual(t, out.ApprovalID, approved)
	assert.True(t, strings.HasPrefix(approved, "v1."))
	resumed, err := first.Invoke(context.Background(), "orders.delete", map[string]string{"id": "123"}, approved)
	require.NoError(t, err)
	assert.Equal(t, "ok", resumed.Status)
	assert.Equal(t, int32(1), hits.Load())
	_, err = first.Invoke(context.Background(), "orders.delete", map[string]string{"id": "123"}, approved)
	require.Error(t, err)
	assert.Equal(t, int32(1), hits.Load())
}

type allowAll struct{}

func (allowAll) Check(context.Context, *catalog.Operation) (policy.Decision, error) {
	return policy.DecisionAllow, nil
}

func TestExtensionCannotSkipConfirmationOrDenial(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	op := cat.ByID("orders.delete")
	require.NotNil(t, op)
	op.Permissions = []string{"order.delete"}
	regoPath := filepathRego(t)
	eng, err := opa.New(context.Background(), regoPath, "", policy.Builtin{Allow: map[string]bool{"order.delete": true}})
	require.NoError(t, err)
	allowStop := func(context.Context, *catalog.Operation) (policy.Decision, bool, error) {
		return policy.DecisionAllow, true, nil
	}
	cases := []struct {
		name   string
		setup  func(*agent.Loop)
		status string
	}{
		{
			name: "wrapper keeps a permission denial",
			setup: func(loop *agent.Loop) {
				loop.SetFloor(policy.Builtin{Allow: map[string]bool{}})
				loop.SetPolicy(policy.Builtin{Allow: map[string]bool{}})
				loop.WrapPolicy(allowStop)
			},
			status: "denied",
		},
		{
			name: "wrapper cannot skip confirmation",
			setup: func(loop *agent.Loop) {
				loop.SetPolicy(policy.Builtin{Allow: map[string]bool{"order.delete": true}})
				loop.WrapPolicy(allowStop)
			},
			status: "confirmation_required",
		},
		{
			name: "replaced allow-all keeps a permission denial",
			setup: func(loop *agent.Loop) {
				loop.SetFloor(policy.Builtin{Allow: map[string]bool{}})
				loop.Policy = allowAll{}
			},
			status: "denied",
		},
		{
			name: "replaced allow-all cannot skip confirmation",
			setup: func(loop *agent.Loop) {
				loop.SetPolicy(policy.Builtin{Allow: map[string]bool{"order.delete": true}})
				loop.Policy = allowAll{}
			},
			status: "confirmation_required",
		},
		{
			name: "opa deny stays on the overlay",
			setup: func(loop *agent.Loop) {
				loop.SetPolicy(eng)
			},
			status: "denied",
		},
		{
			name: "wrapper keeps an opa denial",
			setup: func(loop *agent.Loop) {
				loop.SetPolicy(eng)
				loop.WrapPolicy(allowStop)
			},
			status: "denied",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
			}))
			defer ts.Close()
			loop, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
			require.NoError(t, err)
			tc.setup(loop)
			out, err := loop.Invoke(context.Background(), "orders.delete", map[string]string{"id": "123"}, "")
			require.NoError(t, err)
			assert.Equal(t, tc.status, out.Status)
			assert.Equal(t, int32(0), hits.Load())
		})
	}
}

func filepathRego(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "deny.rego")
	body := []byte("package veto\n\nimport rego.v1\n\ndefault decision := \"allow\"\ndefault reason := \"\"\n\ndecision := \"deny\" if {\n\tinput.operation == \"orders.delete\"\n}\n")
	require.NoError(t, os.WriteFile(path, body, 0o600))
	return path
}
