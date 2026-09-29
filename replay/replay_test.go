package replay_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/replay"
	"github.com/aiveto/veto/telemetry"
)

func TestReplayShowsConfirmationAndOmitsTheMessage(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rec, err := telemetry.Record()
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Stop(context.Background())

	loop := agent.New(cat, nil, execute.Client{BaseURL: "http://127.0.0.1:9"})
	out, err := loop.Run(context.Background(), "Delete asset 123")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "confirmation_required" {
		t.Fatalf("status %s", out.Status)
	}
	view := replay.FromSpans(rec.Spans(), true)
	text := view.String()
	if !strings.Contains(text, "policy.decision") || !strings.Contains(text, "decision=confirmation_required") {
		t.Fatalf("missing decision:\n%s", text)
	}
	if strings.Contains(text, "execute.invoke") {
		t.Fatalf("HTTP span recorded before approval:\n%s", text)
	}
	if strings.Contains(text, "Delete asset 123") || strings.Contains(text, "user_message=") {
		t.Fatalf("message leaked:\n%s", text)
	}
	if !strings.Contains(text, "tools=") || !strings.Contains(text, "assets.delete") {
		t.Fatalf("tools missing:\n%s", text)
	}

	open := replay.FromSpans(rec.Spans(), false)
	if !strings.Contains(open.String(), "user_message=Delete asset 123") {
		t.Fatalf("expected message when retention is on:\n%s", open.String())
	}
}

func TestRedactDropsAttributesOutsideTheAllowlist(t *testing.T) {
	spans := []telemetry.Span{{
		Name: "model.request",
		Attrs: map[string]string{
			"operation.id": "assets.delete",
			"user_message": "Delete asset 123",
			"prompt":       "Delete asset 123",
			"input":        "secret",
		},
	}}
	text := replay.FromSpans(spans, true).String()
	if strings.Contains(text, "Delete") || strings.Contains(text, "secret") || strings.Contains(text, "prompt=") || strings.Contains(text, "input=") {
		t.Fatalf("attribute leaked:\n%s", text)
	}
	if !strings.Contains(text, "operation.id=assets.delete") {
		t.Fatalf("safe attribute dropped:\n%s", text)
	}
}

func TestTraceFileOmitsTheMessage(t *testing.T) {
	spans := []telemetry.Span{{
		Name: "agent.run",
		Attrs: map[string]string{
			"operation.id": "assets.delete",
			"user_message": "Delete asset 123",
		},
	}}
	path := filepath.Join(t.TempDir(), "trace.json")
	view := replay.FromSpans(spans, true)
	if err := replay.Save(path, view); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Delete asset 123") {
		t.Fatalf("file leaked the message:\n%s", raw)
	}
	loaded, err := replay.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(loaded.String(), "operation.id=assets.delete") {
		t.Fatalf("loaded:\n%s", loaded.String())
	}
}
