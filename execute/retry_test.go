package execute_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

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
				ID: "assets.create", Method: http.MethodPost, PathTemplate: "/assets",
				Idempotency: "key", Retry: "2",
				Params: []catalog.Param{{Name: "body", In: "body", Required: true}},
			},
			params:  map[string]string{"body": `{"name":"a"}`},
			hits:    3,
			wantKey: true,
		},
		{
			name: "retry never stays one call",
			op:   &catalog.Operation{ID: "assets.get", Method: http.MethodGet, PathTemplate: "/assets", Retry: "never"},
			hits: 1,
		},
		{
			name: "delete without a key does not retry",
			op: &catalog.Operation{
				ID: "assets.delete", Method: http.MethodDelete, PathTemplate: "/assets/{id}", Retry: "2",
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
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			resp, err := execute.InvokeResponse(context.Background(), execute.Config{BaseURL: ts.URL}, tc.op, tc.params)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, tc.hits, hits.Load())
			assert.False(t, keyChanged)
			if tc.wantKey {
				assert.NotEmpty(t, key)
			} else {
				assert.Empty(t, key)
			}
		})
	}
}
