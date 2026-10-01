package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countHook struct{ n int }

func (h *countHook) Check(context.Context, *catalog.Operation) (policy.Decision, error) {
	h.n++
	return policy.DecisionAllow, nil
}

func TestInvokeLimitStopsBeforeHTTP(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID:           "orders.get",
		Method:       http.MethodGet,
		PathTemplate: "/orders/{id}",
		Params:       []catalog.Param{{Name: "id", In: "path", Required: true}},
	}}}
	cat.Finalize()
	hook := &countHook{}
	when := time.Unix(1_700_000_000, 0)
	rt := Runtime{
		Catalog: cat,
		Policy:  hook,
		State:   policy.NewState(),
		Exec:    execute.Client{BaseURL: ts.URL},
		now:     func() time.Time { return when },
	}
	req := Request{
		Operation: "orders.get",
		Caller:    "ada",
		Arguments: map[string]any{"id": "123"},
	}
	for range invokePerWindow {
		out, err := rt.Preview(context.Background(), req)
		require.NoError(t, err)
		assert.Empty(t, out.Errors)
	}
	assert.Equal(t, int32(0), hits.Load())
	checked := hook.n

	for i := range invokePerWindow {
		out, err := rt.Invoke(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, "ok", out.Status, "call %d", i)
	}
	assert.Equal(t, int32(invokePerWindow), hits.Load())
	assert.Equal(t, checked+invokePerWindow, hook.n)

	const secret = "super-secret-token"
	limited, err := rt.Invoke(context.Background(), Request{
		Operation: "orders.get",
		Caller:    "ada",
		Arguments: map[string]any{"id": "123", "password": secret},
	})
	require.NoError(t, err)
	assert.Equal(t, "limited", limited.Status)
	assert.Equal(t, "invoke_limited", limited.Code)
	assert.Equal(t, "invoke limit", limited.Error)
	assert.NotEmpty(t, limited.RetryAfter)
	assert.Equal(t, "orders.get", limited.OperationID)
	assert.False(t, limited.Retryable)
	assert.Empty(t, limited.Body)
	assert.Zero(t, limited.HTTPStatus)
	raw, err := json.Marshal(limited)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), secret)
	assert.Equal(t, int32(invokePerWindow), hits.Load())
	assert.Equal(t, checked+invokePerWindow, hook.n)

	other, err := rt.Invoke(context.Background(), Request{
		Operation: "orders.get",
		Caller:    "grace",
		Arguments: map[string]any{"id": "123"},
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", other.Status)
	assert.Equal(t, int32(invokePerWindow+1), hits.Load())

	fresh := rt
	fresh.State = policy.NewState()
	fresh.Gate = rt.Gate
	still, err := fresh.Invoke(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "limited", still.Status)

	otherRT := Runtime{
		Catalog: cat,
		Policy:  policy.Builtin{},
		Exec:    execute.Client{BaseURL: ts.URL},
		now:     func() time.Time { return when },
	}
	again, err := otherRT.Invoke(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "ok", again.Status)
}
