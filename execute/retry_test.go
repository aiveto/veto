package execute_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
)

func TestRetryOnlyWhenTheCallIsIdempotent(t *testing.T) {
	cases := []struct {
		name    string
		op      *catalog.Operation
		params  map[string]string
		hits    int32
		wantKey bool
	}{
		{
			name: "idempotency key retries and keeps the key",
			op: &catalog.Operation{
				ID: "orders.create", Method: http.MethodPost, PathTemplate: "/orders",
				Idempotency: "key", Retry: "2",
				Params: []catalog.Param{{Name: "body", In: "body", Required: true}},
			},
			params:  map[string]string{"body": `{"name":"a"}`},
			hits:    3,
			wantKey: true,
		},
		{
			name: "retry never stays one call",
			op:   &catalog.Operation{ID: "orders.get", Method: http.MethodGet, PathTemplate: "/orders", Retry: "never"},
			hits: 1,
		},
		{
			name: "delete without a key does not retry",
			op: &catalog.Operation{
				ID: "orders.delete", Method: http.MethodDelete, PathTemplate: "/orders/{id}", Retry: "2",
				Params: []catalog.Param{{Name: "id", In: "path", Required: true}},
			},
			params: map[string]string{"id": "1"},
			hits:   1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			var key string
			var keyChanged bool
			var times []time.Time
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				times = append(times, time.Now())
				hits.Add(1)
				got := r.Header.Get("Idempotency-Key")
				if key == "" {
					key = got
				} else if got != key {
					keyChanged = true
				}
				w.WriteHeader(http.StatusBadGateway)
			}))
			defer ts.Close()
			resp, err := execute.InvokeResponse(context.Background(), execute.Client{BaseURL: ts.URL}, tc.op, tc.params)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, tc.hits, hits.Load())
			assert.False(t, keyChanged)
			if tc.wantKey {
				assert.NotEmpty(t, key)
			} else {
				assert.Empty(t, key)
			}
			if tc.hits > 1 {
				require.GreaterOrEqual(t, len(times), 2)
				assert.GreaterOrEqual(t, times[1].Sub(times[0]), 30*time.Millisecond)
			}
		})
	}
}

func TestRetryHonorsRetryAfter(t *testing.T) {
	op := &catalog.Operation{
		ID: "orders.create", Method: http.MethodPost, PathTemplate: "/orders",
		Idempotency: "key", Retry: "1",
		Params: []catalog.Param{{Name: "body", In: "body", Required: true}},
	}
	cases := []struct {
		name   string
		header func() string
	}{
		{name: "delay seconds", header: func() string { return "1" }},
		{name: "http date", header: func() string { return time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			var key string
			var keyChanged bool
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got := r.Header.Get("Idempotency-Key")
				if key == "" {
					key = got
				} else if got != key {
					keyChanged = true
				}
				if hits.Add(1) == 1 {
					w.Header().Set("Retry-After", tc.header())
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				w.WriteHeader(http.StatusCreated)
			}))
			defer ts.Close()
			start := time.Now()
			resp, err := execute.InvokeResponse(context.Background(), execute.Client{BaseURL: ts.URL}, op, map[string]string{"body": `{"name":"a"}`})
			elapsed := time.Since(start)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, int32(2), hits.Load())
			assert.GreaterOrEqual(t, elapsed, 900*time.Millisecond)
			assert.False(t, keyChanged)
			assert.NotEmpty(t, key)
		})
	}
}

func TestRetryAfterStopsWhenTheContextIsCanceled(t *testing.T) {
	op := &catalog.Operation{ID: "orders.get", Method: http.MethodGet, PathTemplate: "/orders", Retry: "2"}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(40 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	resp, err := execute.InvokeResponse(ctx, execute.Client{BaseURL: ts.URL}, op, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), 1500*time.Millisecond)
}
