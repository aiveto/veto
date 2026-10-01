package runtime_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPendingWebhookDoesNotCallUpstream(t *testing.T) {
	const secret = "sekret-upstream-token"
	t.Setenv("UPSTREAM_TOKEN", secret)
	t.Setenv("VETO_APPROVAL_SECRET", secret)

	t.Run("http", func(t *testing.T) {
		var hits atomic.Int32
		var body []byte
		var authz string
		hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			authz = r.Header.Get("Authorization")
			body, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(hook.Close)
		wh, err := policy.NewWebhook(hook.URL, nil)
		require.NoError(t, err)
		pending := invokePending(t, wh, secret)
		assert.Equal(t, int32(1), hits.Load())
		assert.Empty(t, authz)
		assertNotice(t, body, pending, secret)
	})

	t.Run("command", func(t *testing.T) {
		dir := t.TempDir()
		script := filepath.Join(dir, "hook.sh")
		bodyPath := filepath.Join(dir, "body")
		envPath := filepath.Join(dir, "env")
		require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\ncat > \"$1\"\nenv > \"$2\"\n"), 0o755))
		wh, err := policy.NewWebhook("", []string{script, bodyPath, envPath})
		require.NoError(t, err)
		pending := invokePending(t, wh, secret)
		body, err := os.ReadFile(bodyPath)
		require.NoError(t, err)
		env, err := os.ReadFile(envPath)
		require.NoError(t, err)
		assertNotice(t, body, pending, secret)
		assert.NotContains(t, string(env), secret)
		assert.NotContains(t, string(env), "UPSTREAM_TOKEN")
		assert.NotContains(t, string(env), "VETO_APPROVAL_SECRET")
	})
}

func invokePending(t *testing.T, notify policy.Notifier, secret string) string {
	t.Helper()
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(up.Close)
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID:                   "orders.delete",
		Method:               http.MethodDelete,
		PathTemplate:         "/orders/{id}",
		RequiresConfirmation: true,
		Params:               []catalog.Param{{Name: "id", In: "path", Required: true}},
	}}}
	cat.Finalize()
	rt := runtime.Runtime{
		Catalog: cat,
		State:   policy.NewState(),
		Exec:    execute.Client{BaseURL: up.URL},
		Notify:  notify,
	}
	req := runtime.Request{
		Operation: "orders.delete",
		Arguments: runtime.FromStrings(map[string]string{"id": "123", "token": secret}),
		Caller:    "ada",
	}
	first, err := rt.Invoke(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "confirmation_required", first.Status)
	assert.Equal(t, int32(0), hits.Load())

	req.Approval = first.ApprovalID
	_, err = rt.Invoke(context.Background(), req)
	assert.Error(t, err)
	assert.Equal(t, int32(0), hits.Load())
	return first.ApprovalID
}

func assertNotice(t *testing.T, body []byte, pending, secret string) {
	t.Helper()
	var doc map[string]string
	require.NoError(t, json.Unmarshal(body, &doc))
	assert.Equal(t, map[string]string{
		"id":        pending,
		"operation": "orders.delete",
		"caller":    "ada",
	}, doc)
	assert.NotContains(t, string(body), secret)
}
