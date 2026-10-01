package mcpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/mcpserver"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/semantics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvokeDeleteRequiresApprovalBeforeHTTP(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	sem := semantics.NewDerived(cat)
	loop, err := agent.New(cat, sem, execute.Client{BaseURL: ts.URL})
	require.NoError(t, err)
	calls := loop.Runtime()
	srv := &mcpserver.Server{
		Catalog:   cat,
		Semantics: sem,
		Calls:     &calls,
	}
	ctx := context.Background()
	first, err := srv.Invoke(ctx, "orders.delete", map[string]string{"id": "123"}, "")
	require.NoError(t, err)
	assert.Equal(t, "confirmation_required", first.Status)
	assert.Equal(t, int32(0), hits.Load())
	_, err = srv.Invoke(ctx, "orders.delete", map[string]string{"id": "123"}, first.ApprovalID)
	require.Error(t, err)
	assert.Equal(t, int32(0), hits.Load())
	approved, err := loop.State.Approve(first.ApprovalID)
	require.NoError(t, err)
	assert.NotEqual(t, first.ApprovalID, approved)
	second, err := srv.Invoke(ctx, "orders.delete", map[string]string{"id": "123"}, approved)
	require.NoError(t, err)
	assert.Equal(t, "ok", second.Status)
	assert.Equal(t, int32(1), hits.Load())
	_, err = srv.Invoke(ctx, "orders.delete", map[string]string{"id": "123"}, approved)
	require.Error(t, err)
	assert.Equal(t, int32(1), hits.Load())
	raw, err := json.Marshal(first)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	assert.Equal(t, "confirmation_required", doc["status"])
	assert.NotEmpty(t, doc["approval_id"])
}

func TestInvokeJSONCarriesCodeAndRetryable(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	sem := semantics.NewDerived(cat)
	loop, err := agent.New(cat, sem, execute.Client{BaseURL: ts.URL})
	require.NoError(t, err)
	calls := loop.Runtime()
	srv := &mcpserver.Server{
		Catalog:   cat,
		Semantics: sem,
		Calls:     &calls,
	}
	missing, err := srv.Invoke(context.Background(), "orders.get", nil, "")
	require.Error(t, err)
	assert.Equal(t, "missing_param", missing.Code)
	got, err := srv.Invoke(context.Background(), "orders.get", map[string]string{"id": "9"}, "")
	require.NoError(t, err)
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	var doc struct {
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	assert.Equal(t, "not_found", doc.Code)
	assert.Equal(t, "not_found", got.Status)
	assert.False(t, doc.Retryable)
}

func TestInvokeDiscoveryOnlyDoesNotCallHTTP(t *testing.T) {
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
	sem := semantics.NewDerived(cat)
	loop, err := agent.New(cat, sem, execute.Client{BaseURL: ts.URL})
	require.NoError(t, err)
	calls := loop.Runtime()
	srv := &mcpserver.Server{Catalog: cat, Semantics: sem, Calls: &calls}
	got, err := srv.Invoke(context.Background(), "orders.get", map[string]string{"id": "1"}, "")
	require.ErrorContains(t, err, "discovery-only")
	assert.Equal(t, "not_callable", got.Code)
	assert.Equal(t, int32(0), hits.Load())
	direct, err := loop.Invoke(context.Background(), "orders.delete", map[string]string{"id": "1"}, "")
	require.NoError(t, err)
	assert.Equal(t, "confirmation_required", direct.Status)
	op = cat.ByID("orders.delete")
	op.Exposure = catalog.ExposureDiscovery
	_, err = loop.Invoke(context.Background(), "orders.delete", map[string]string{"id": "1"}, "")
	require.ErrorContains(t, err, "discovery-only")
	assert.Equal(t, int32(0), hits.Load())
}
