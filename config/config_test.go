package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if cfg.Timeout != 30*time.Second {
		t.Fatalf("timeout: %s", cfg.Timeout)
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

func TestTraceExportOTLPAndTraceFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veto.yaml")
	if err := os.WriteFile(path, []byte("trace_export: otlp\ntrace_file: trace.json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TraceExport != "otlp" || !strings.HasSuffix(cfg.TraceFile, "trace.json") {
		t.Fatalf("%+v", cfg)
	}
}

func TestOPAAndSpiceDBAreKeysOnly(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"opa", "spicedb"} {
		path := filepath.Join(dir, name+".yaml")
		if err := os.WriteFile(path, []byte("policy: "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Policy != name {
			t.Fatalf("policy: %s", cfg.Policy)
		}
	}
}

func TestMemoryFileRequiresAPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veto.yaml")
	if err := os.WriteFile(path, []byte("memory: file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil || !strings.Contains(err.Error(), "memory_file") {
		t.Fatalf("missing path: %v", err)
	}
	okPath := filepath.Join(dir, "ok.yaml")
	if err := os.WriteFile(okPath, []byte("memory: file\nmemory_file: turns.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(okPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Memory != "file" || !strings.HasSuffix(cfg.MemoryFile, "turns.log") {
		t.Fatalf("%+v", cfg)
	}
}

func TestTemporalAndJevAreKeysOnly(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		body string
		exec string
		dec  string
	}{
		{name: "temporal.yaml", body: "execution: temporal\n", exec: "temporal", dec: "default"},
		{name: "jev.yaml", body: "decision: jev\n", exec: "in-process", dec: "jev"},
	}
	for _, tc := range cases {
		path := filepath.Join(dir, tc.name)
		if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Execution != tc.exec || cfg.Decision != tc.dec {
			t.Fatalf("%s: %+v", tc.name, cfg)
		}
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
