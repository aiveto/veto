package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aiveto/veto/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadResolvesFilesAndDefaults(t *testing.T) {
	cfg, err := config.Load("../testdata/veto.yaml")
	require.NoError(t, err)
	assert.Equal(t, "scripted", cfg.Model)
	assert.Equal(t, "off", cfg.Subagents)
	assert.Empty(t, cfg.TraceExport)
	assert.True(t, strings.HasSuffix(cfg.SemanticsFile, "semantics.yaml"))
	assert.True(t, strings.HasSuffix(cfg.FlowFile, "flow.yaml"))
	assert.True(t, strings.HasSuffix(cfg.AgentFile, "agent.yaml"))
	assert.True(t, strings.HasSuffix(cfg.RelationsFile, "relations.yaml"))
	assert.Len(t, cfg.Contracts, 2)
	assert.True(t, cfg.Redact())
	assert.Equal(t, 30*time.Second, cfg.Timeout)
}

func TestKnownProviderKeysLoad(t *testing.T) {
	cases := []struct {
		name string
		body string
		want config.File
	}{
		{name: "openai name keeps an empty host", body: "model: openai\nmodel_name: gpt-test\n", want: config.File{Model: "openai", ModelName: "gpt-test"}},
		{name: "openai host", body: "model: openai\nmodel_base_url: http://127.0.0.1:9/v1\n", want: config.File{ModelBaseURL: "http://127.0.0.1:9/v1"}},
		{name: "otlp and trace file", body: "trace_export: otlp\ntrace_file: trace.json\n", want: config.File{TraceExport: "otlp", TraceFile: "trace.json"}},
		{name: "opa", body: "policy: opa\n", want: config.File{Policy: "opa"}},
		{name: "spicedb", body: "policy: spicedb\n", want: config.File{Policy: "spicedb"}},
		{name: "temporal", body: "execution: temporal\n", want: config.File{Execution: "temporal", Decision: "default"}},
		{name: "jev", body: "decision: jev\n", want: config.File{Execution: "in-process", Decision: "jev"}},
		{name: "memory file", body: "memory: file\nmemory_file: turns.log\n", want: config.File{Memory: "file", MemoryFile: "turns.log"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "veto.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.body), 0o644))
			cfg, err := config.Load(path)
			require.NoError(t, err)
			if tc.want.Model != "" {
				assert.Equal(t, tc.want.Model, cfg.Model)
			}
			assert.Equal(t, tc.want.ModelName, cfg.ModelName)
			assert.Equal(t, tc.want.ModelBaseURL, cfg.ModelBaseURL)
			assert.Equal(t, tc.want.TraceExport, cfg.TraceExport)
			if tc.want.TraceFile != "" {
				assert.True(t, strings.HasSuffix(cfg.TraceFile, tc.want.TraceFile))
			}
			if tc.want.Policy != "" {
				assert.Equal(t, tc.want.Policy, cfg.Policy)
			}
			if tc.want.Execution != "" {
				assert.Equal(t, tc.want.Execution, cfg.Execution)
			}
			if tc.want.Decision != "" {
				assert.Equal(t, tc.want.Decision, cfg.Decision)
			}
			if tc.want.Memory != "" {
				assert.Equal(t, tc.want.Memory, cfg.Memory)
				assert.True(t, strings.HasSuffix(cfg.MemoryFile, tc.want.MemoryFile))
			}
		})
	}
}

func TestUnusableProviderKeysFail(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "memory file without a path", body: "memory: file\n", want: "memory_file"},
		{name: "subagents", body: "subagents: on\n", want: "subagents"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "veto.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.body), 0o644))
			_, err := config.Load(path)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}
