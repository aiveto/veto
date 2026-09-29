package agent_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/flow"
	"github.com/aiveto/veto/memory"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runctx"
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
	base := semantics.NewDerived(cat)
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
		Memory:    memory.NewLocalMap(),
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
	return agent.Response{}, fmt.Errorf("stop")
}

func TestRecentTurnsStayInThePack(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	loop, err := agent.New(cat, nil, nil)
	require.NoError(t, err)
	require.NoError(t, loop.Memory.Store(context.Background(), memory.Item{ID: "old", Content: "earlier turn about widgets"}))
	cap := &capture{}
	loop.Model = cap
	_, err = loop.Run(context.Background(), "brand new question")
	assert.Error(t, err)
	assert.Contains(t, cap.saw, "earlier turn about widgets")
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
		{name: "stop allows the call", stop: true, status: "ok", hits: 1},
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
		Semantics: semantics.NewDerived(cat),
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

func TestSignedApprovalResumesOnAnotherLoop(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	secret := []byte("approval-secret")
	first, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	require.NoError(t, err)
	first.State.SetSigner(secret, time.Hour)
	out, err := first.Run(context.Background(), "Delete order 123")
	require.NoError(t, err)
	assert.Equal(t, "confirmation_required", out.Status)
	assert.Equal(t, "confirm orders.delete id=123", out.Text)
	assert.True(t, strings.HasPrefix(out.ApprovalID, "v1."))
	assert.Nil(t, first.State.Pending(out.ApprovalID))
	assert.Equal(t, int32(0), hits.Load())
	second, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	require.NoError(t, err)
	second.State.SetSigner(secret, time.Hour)
	resumed, err := second.Invoke(context.Background(), "orders.delete", map[string]string{"id": "123"}, out.ApprovalID)
	require.NoError(t, err)
	assert.Equal(t, "ok", resumed.Status)
	assert.Equal(t, int32(1), hits.Load())
}
