package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiveto/veto/config"
)

func TestLoadResolvesFilesAndDefaults(t *testing.T) {
	cfg, err := config.Load("../testdata/veto.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "scripted" || cfg.Subagents != "off" || cfg.TraceExport != "" {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if !strings.HasSuffix(cfg.SemanticsFile, "semantics.yaml") {
		t.Fatalf("semantics file: %s", cfg.SemanticsFile)
	}
	if !strings.HasSuffix(cfg.FlowFile, "flow.yaml") {
		t.Fatalf("flow file: %s", cfg.FlowFile)
	}
	if !strings.HasSuffix(cfg.AgentFile, "agent.yaml") {
		t.Fatalf("agent file: %s", cfg.AgentFile)
	}
	if !strings.HasSuffix(cfg.RelationsFile, "relations.yaml") {
		t.Fatalf("relations file: %s", cfg.RelationsFile)
	}
	if len(cfg.Contracts) != 2 {
		t.Fatalf("contracts: %v", cfg.Contracts)
	}
	if !cfg.Redact() {
		t.Fatal("replay redacts unless replay_redact is false")
	}
}

func TestOpenAIModelIsAllowed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veto.yaml")
	if err := os.WriteFile(path, []byte("model: openai\nmodel_name: gpt-test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "openai" || cfg.ModelName != "gpt-test" {
		t.Fatalf("%+v", cfg)
	}
	if cfg.ModelBaseURL != "" {
		t.Fatalf("empty host should stay empty: %q", cfg.ModelBaseURL)
	}
}

func TestModelBaseURLIsLoaded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veto.yaml")
	if err := os.WriteFile(path, []byte("model: openai\nmodel_base_url: http://127.0.0.1:9/v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ModelBaseURL != "http://127.0.0.1:9/v1" {
		t.Fatalf("base: %q", cfg.ModelBaseURL)
	}
}

func TestSubagentsRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veto.yaml")
	if err := os.WriteFile(path, []byte("subagents: on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "subagents") {
		t.Fatalf("expected subagent refusal, got %v", err)
	}
}
