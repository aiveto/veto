package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/result"
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
	assert.JSONEq(t, `{"name":"ada"}`, body)
	assert.Equal(t, "application/json", contentType)
	assert.Equal(t, "key-1", key)
	assert.Equal(t, "tenant-a", seen.caller)
	bodyArg, ok := seen.args["body"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ada", bodyArg["name"])
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
	assert.Equal(t, "held until you approve", first.Why)
	assert.False(t, first.HTTP)
	assert.Equal(t, int32(0), hits.Load())
	req.Approval = first.ApprovalID
	_, err = rt.Invoke(context.Background(), req)
	require.ErrorIs(t, err, runtime.ErrInvalidApproval)
	assert.Equal(t, int32(0), hits.Load())
	approved, err := rt.State.Approve(t.Context(), first.ApprovalID)
	require.NoError(t, err)
	req.Approval = approved
	second, err := rt.Invoke(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "ok", second.Status)
	assert.True(t, second.HTTP)
	assert.Empty(t, second.Why)
	assert.Equal(t, int32(1), hits.Load())
}

func TestConfirmationWithoutStateDoesNotPanic(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID:                   "orders.delete",
		Method:               http.MethodDelete,
		PathTemplate:         "/orders/{id}",
		RequiresConfirmation: true,
		Params:               []catalog.Param{{Name: "id", In: "path", Required: true}},
	}}}
	cat.Finalize()
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer ts.Close()
	rt := runtime.Runtime{Catalog: cat, Exec: execute.Client{BaseURL: ts.URL}}
	out, err := rt.Invoke(context.Background(), runtime.Request{
		Operation: "orders.delete",
		Arguments: runtime.FromStrings(map[string]string{"id": "123"}),
	})
	require.Error(t, err)
	assert.Equal(t, "error", out.Status)
	assert.Contains(t, err.Error(), "confirmation state")
	assert.Equal(t, int32(0), hits.Load())
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
	require.Error(t, err)
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
	require.ErrorContains(t, err, "cannot be serialized")
	assert.Equal(t, int32(0), hits.Load())
}

func TestInvokeRejectsABodyOutsideTheSchemaBeforeHTTP(t *testing.T) {
	const secret = "s3cret-value"
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer ts.Close()
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID:           "customers.create",
		Method:       http.MethodPost,
		PathTemplate: "/customers",
		Params: []catalog.Param{{
			Name: "body", In: "body", Required: true, MediaType: "application/json",
			Schema: `{"type":"object","required":["name"],"properties":{"name":{"type":"string"},"age":{"type":"integer"}}}`,
		}},
	}}}
	cat.Finalize()
	rt := runtime.Runtime{Catalog: cat, State: policy.NewState(), Exec: execute.Client{BaseURL: ts.URL}}
	out, err := rt.Invoke(context.Background(), runtime.Request{
		Operation: "customers.create",
		Arguments: map[string]any{"body": map[string]any{"name": "ada", "age": secret}},
	})
	require.Error(t, err)
	assert.Equal(t, "invalid_body", out.Code)
	assert.Empty(t, out.Why)
	assert.False(t, out.HTTP)
	assert.Contains(t, err.Error(), "/age")
	assert.NotContains(t, err.Error(), secret)
	assert.NotContains(t, out.Error, secret)
	assert.Equal(t, int32(0), hits.Load())
}

func TestMissingAuthWhyDoesNotCallHTTP(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer ts.Close()
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID:           "orders.get",
		Method:       http.MethodGet,
		PathTemplate: "/orders/{id}",
		Params:       []catalog.Param{{Name: "id", In: "path", Required: true}},
		Auth:         []catalog.Auth{{Name: "bearerAuth", Kind: "http"}},
	}}}
	cat.Finalize()
	rt := runtime.Runtime{
		Catalog: cat,
		State:   policy.NewState(),
		Exec:    execute.Client{BaseURL: ts.URL},
	}
	out, err := rt.Invoke(context.Background(), runtime.Request{
		Operation: "orders.get",
		Arguments: runtime.FromStrings(map[string]string{"id": "1"}),
		Caller:    "ada",
	})
	require.ErrorContains(t, err, "bearerAuth is unset")
	assert.Equal(t, "missing_auth", out.Code)
	assert.Equal(t, "missing auth", out.Why)
	assert.Equal(t, "ada", out.Caller)
	assert.False(t, out.HTTP)
	assert.Equal(t, int32(0), hits.Load())
}

func TestTokenURLUnsetIsNotMissingAuth(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID:           "orders.get",
		Method:       http.MethodGet,
		PathTemplate: "/orders/{id}",
		Params:       []catalog.Param{{Name: "id", In: "path", Required: true}},
	}}}
	cat.Finalize()
	rt := runtime.Runtime{Catalog: cat, State: policy.NewState(), Exec: errExec{err: errors.New("token url is unset")}}
	out, err := rt.Invoke(context.Background(), runtime.Request{
		Operation: "orders.get",
		Arguments: runtime.FromStrings(map[string]string{"id": "1"}),
		Caller:    "ada",
	})
	require.ErrorContains(t, err, "token url is unset")
	assert.Empty(t, out.Code)
	assert.Empty(t, out.Why)
	assert.Equal(t, "ada", out.Caller)
	assert.False(t, out.HTTP)
}

func TestConcurrentFirstInvokeSharesTheGate(t *testing.T) {
	testConcurrentFirstInvoke(t, &runtime.InvokeGate{})
}

func TestConcurrentFirstInvokeCreatesOneGate(t *testing.T) {
	testConcurrentFirstInvoke(t, nil)
}

func testConcurrentFirstInvoke(t *testing.T, gate *runtime.InvokeGate) {
	t.Helper()
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID: "orders.get", Method: http.MethodGet, PathTemplate: "/orders/{id}",
		Params: []catalog.Param{{Name: "id", In: "path", Required: true}},
	}}}
	cat.Finalize()
	rt := &runtime.Runtime{Catalog: cat, State: policy.NewState(), Exec: allowExec{}, Gate: gate}
	const n = 32
	var ready sync.WaitGroup
	ready.Add(n)
	start := make(chan struct{})
	var ok, limited atomic.Int32
	errCh := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			ready.Done()
			<-start
			out, err := rt.Invoke(context.Background(), runtime.Request{
				Operation: "orders.get",
				Arguments: runtime.FromStrings(map[string]string{"id": "1"}),
			})
			if err != nil {
				errCh <- err
				return
			}
			switch out.Status {
			case "ok":
				ok.Add(1)
			case "limited":
				limited.Add(1)
			default:
				errCh <- fmt.Errorf("status %s", out.Status)
			}
		})
	}
	ready.Wait()
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
	assert.Equal(t, int32(16), ok.Load())
	assert.Equal(t, int32(16), limited.Load())
}

func TestDiscoveryOnlyDoesNotCallHTTP(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	op := cat.ByID("orders.get")
	require.NotNil(t, op)
	op.Exposure = catalog.ExposureDiscovery
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer ts.Close()
	rt := runtime.Runtime{Catalog: cat, State: policy.NewState(), Exec: execute.Client{BaseURL: ts.URL}}
	out, err := rt.Invoke(context.Background(), runtime.Request{
		Operation: "orders.get",
		Arguments: runtime.FromStrings(map[string]string{"id": "1"}),
	})
	require.ErrorContains(t, err, "discovery-only")
	assert.Equal(t, "not_callable", out.Code)
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
	require.Error(t, err)
	assert.Equal(t, "missing_param", out.Code)
	assert.Equal(t, int32(0), hits.Load())
}

type errExec struct{ err error }

func (e errExec) InvokeHTTPResult(context.Context, *catalog.Operation, map[string]string) (result.HTTPResult, error) {
	return result.HTTPResult{}, e.err
}

type allowExec struct{}

func (allowExec) InvokeHTTPResult(context.Context, *catalog.Operation, map[string]string) (result.HTTPResult, error) {
	return result.HTTPResult{Status: http.StatusOK, Body: "{}", Code: "ok"}, nil
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
