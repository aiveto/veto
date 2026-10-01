package runtime_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvokeKeepsTypedArgumentsUntilHTTP(t *testing.T) {
	var body, key, contentType string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		key = r.Header.Get("Idempotency-Key")
		contentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID:           "customers.create",
		Method:       http.MethodPost,
		PathTemplate: "/customers",
		Params: []catalog.Param{{
			Name: "body", In: "body", Required: true, MediaType: "application/json",
		}},
	}}}
	cat.Finalize()
	seen := &callerHook{}
	rt := runtime.Runtime{
		Catalog: cat,
		Policy:  seen,
		State:   policy.NewState(),
		Exec:    execute.Client{BaseURL: ts.URL},
	}
	out, err := rt.Invoke(context.Background(), runtime.Request{
		Operation: "customers.create",
		Arguments: map[string]any{
			"body": map[string]any{"name": "ada"},
		},
		Caller:      "tenant-a",
		Idempotency: "key-1",
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", out.Status)
	assert.Equal(t, `{"name":"ada"}`, body)
	assert.Equal(t, "application/json", contentType)
	assert.Equal(t, "key-1", key)
	assert.Equal(t, "tenant-a", seen.caller)
	assert.Equal(t, "ada", seen.args["body"].(map[string]any)["name"])
}

func TestDeleteWaitsForApproval(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	rt := runtime.Runtime{
		Catalog: cat,
		State:   policy.NewState(),
		Exec:    execute.Client{BaseURL: ts.URL},
	}
	req := runtime.Request{
		Operation: "orders.delete",
		Arguments: runtime.FromStrings(map[string]string{"id": "123"}),
	}
	first, err := rt.Invoke(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "confirmation_required", first.Status)
	assert.Equal(t, int32(0), hits.Load())
	req.Approval = first.ApprovalID
	_, err = rt.Invoke(context.Background(), req)
	assert.Error(t, err)
	assert.Equal(t, int32(0), hits.Load())
	approved, err := rt.State.Approve(first.ApprovalID)
	require.NoError(t, err)
	req.Approval = approved
	second, err := rt.Invoke(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "ok", second.Status)
	assert.Equal(t, int32(1), hits.Load())
}

func TestPreviewRejectsABodyThatIsNotAJSONObject(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer ts.Close()
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID:           "orders.create",
		Method:       http.MethodPost,
		PathTemplate: "/orders",
		Params: []catalog.Param{{
			Name: "body", In: "body", Required: true, Schema: `{"type":"object"}`,
		}},
	}}}
	cat.Finalize()
	rt := runtime.Runtime{Catalog: cat, State: policy.NewState(), Exec: execute.Client{BaseURL: ts.URL}}
	out, err := rt.Preview(context.Background(), runtime.Request{
		Operation: "orders.create",
		Arguments: runtime.FromStrings(map[string]string{"body": "not-json"}),
	})
	require.NoError(t, err)
	assert.NotEmpty(t, out.Errors)
	assert.Empty(t, out.Decision)
	_, err = rt.Invoke(context.Background(), runtime.Request{
		Operation: "orders.create",
		Arguments: runtime.FromStrings(map[string]string{"body": "not-json"}),
	})
	assert.Error(t, err)
	assert.Equal(t, int32(0), hits.Load())
}

func TestInvokeRejectsUnserializable(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer ts.Close()
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID:           "labels.get",
		Method:       http.MethodGet,
		PathTemplate: "/labels/{id}",
		Params: []catalog.Param{{
			Name: "id", In: "path", Required: true, Style: "matrix", Schema: `{"type":"string"}`,
		}},
	}}}
	cat.Finalize()
	rt := runtime.Runtime{Catalog: cat, State: policy.NewState(), Exec: execute.Client{BaseURL: ts.URL}}
	_, err := rt.Invoke(context.Background(), runtime.Request{
		Operation: "labels.get",
		Arguments: runtime.FromStrings(map[string]string{"id": "abc"}),
	})
	assert.ErrorContains(t, err, "cannot be serialized")
	assert.Equal(t, int32(0), hits.Load())
}

func TestMissingParamSkipsHTTP(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer ts.Close()
	rt := runtime.Runtime{Catalog: cat, State: policy.NewState(), Exec: execute.Client{BaseURL: ts.URL}}
	out, err := rt.Invoke(context.Background(), runtime.Request{Operation: "orders.get"})
	assert.Error(t, err)
	assert.Equal(t, "missing_param", out.Code)
	assert.Equal(t, int32(0), hits.Load())
}

type callerHook struct {
	caller string
	args   map[string]any
}

func (h *callerHook) Check(ctx context.Context, op *catalog.Operation) (policy.Decision, error) {
	in := policy.InputFrom(ctx)
	h.caller = in.Caller
	h.args = in.Arguments
	return policy.DecisionAllow, nil
}
