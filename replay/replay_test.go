package replay_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/replay"
	"github.com/aiveto/veto/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplayShowsConfirmationAndOmitsTheMessage(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	rec, err := telemetry.Record()
	require.NoError(t, err)
	defer rec.Stop(context.Background())

	loop, err := agent.New(cat, nil, execute.Client{BaseURL: "http://127.0.0.1:9"})
	require.NoError(t, err)
	out, err := loop.Run(context.Background(), "Delete order 123")
	require.NoError(t, err)
	assert.Equal(t, "confirmation_required", out.Status)
	view := replay.FromSpans(rec.Spans(), true)
	text := view.String()
	assert.Contains(t, text, "policy.decision")
	assert.Contains(t, text, "decision=confirmation_required")
	assert.NotContains(t, text, "execute.invoke")
	assert.NotContains(t, text, "Delete order 123")
	assert.NotContains(t, text, "user_message=")
	assert.Contains(t, text, "tools=")
	assert.Contains(t, text, "orders.delete")

	open := replay.FromSpans(rec.Spans(), false)
	assert.NotContains(t, open.String(), "user_message=")
	assert.NotContains(t, open.String(), "Delete order 123")
}

func TestRedactDropsAttributesOutsideTheAllowlist(t *testing.T) {
	spans := []telemetry.Span{{
		Name: "model.request",
		Attrs: map[string]string{
			"operation.id": "orders.delete",
			"user_message": "Delete order 123",
			"prompt":       "Delete order 123",
			"input":        "secret",
		},
	}}
	text := replay.FromSpans(spans, true).String()
	assert.NotContains(t, text, "Delete")
	assert.NotContains(t, text, "secret")
	assert.NotContains(t, text, "prompt=")
	assert.NotContains(t, text, "input=")
	assert.Contains(t, text, "operation.id=orders.delete")
}

func TestTraceFileOmitsTheMessage(t *testing.T) {
	spans := []telemetry.Span{{
		Name: "agent.run",
		Attrs: map[string]string{
			"operation.id": "orders.delete",
			"user_message": "Delete order 123",
		},
	}}
	path := filepath.Join(t.TempDir(), "trace.json")
	view := replay.FromSpans(spans, true)
	require.NoError(t, replay.Save(path, view))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "Delete order 123")
	loaded, err := replay.Load(path)
	require.NoError(t, err)
	assert.Contains(t, loaded.String(), "operation.id=orders.delete")
}
