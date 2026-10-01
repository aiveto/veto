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

func TestAuthSourcesKeepTheEnvShorthand(t *testing.T) {
	body := "token_dir: tokens\nauth:\n  bearerAuth: ORDER_TOKEN\n  user:\n    source: login\n    client_id: veto\n    issuer: https://idp.example\n    scopes: [orders.read]\n    auth_token: app_token\n    user_token: person_token\n    user_header: X-User-Token\n  upstream:\n    source: token_exchange\n    token_url: https://idp.example/token\n    client_id: veto\n    client_secret_env: VETO_SECRET\n    audience: https://api.example\n    scopes: [orders.read]\n    subject: invoke\n  workforce:\n    source: client_credentials\n    token_url: https://idp.example/token\n    client_id: job\n    client_secret_env: WORKFORCE_SECRET\n    audience: https://api.example\n  sig:\n    source: command\n    command: /usr/local/bin/veto-sig\n    timeout: 5s\n"
	path := filepath.Join(t.TempDir(), "veto.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "ORDER_TOKEN", cfg.Auth["bearerAuth"].Env)
	assert.Equal(t, "env", cfg.Auth["bearerAuth"].Kind())
	assert.Equal(t, "login", cfg.Auth["user"].Kind())
	assert.Equal(t, "https://idp.example", cfg.Auth["user"].Issuer)
	assert.Equal(t, []string{"orders.read"}, cfg.Auth["user"].Scopes)
	assert.Equal(t, "app_token", cfg.Auth["user"].AuthToken)
	assert.Equal(t, "person_token", cfg.Auth["user"].UserToken)
	assert.Equal(t, "X-User-Token", cfg.Auth["user"].UserHeader)
	assert.Equal(t, "token_exchange", cfg.Auth["upstream"].Kind())
	assert.Equal(t, "invoke", cfg.Auth["upstream"].Subject)
	assert.Equal(t, "https://api.example", cfg.Auth["upstream"].Audience)
	assert.Equal(t, "WORKFORCE_SECRET", cfg.Auth["workforce"].ClientSecretEnv)
	assert.Equal(t, []string{"/usr/local/bin/veto-sig"}, []string(cfg.Auth["sig"].Command))
	assert.Equal(t, 5*time.Second, time.Duration(cfg.Auth["sig"].Timeout))
	assert.True(t, strings.HasSuffix(cfg.TokenDir, "tokens"))
}

func TestApprovalWebhookIsACommandOrURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "veto.yaml")
	require.NoError(t, os.WriteFile(path, []byte("approval_webhook: https://hooks.example/pending\n"), 0o644))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "https://hooks.example/pending", cfg.ApprovalWebhook.URL)
	assert.Empty(t, cfg.ApprovalWebhook.Command)

	require.NoError(t, os.WriteFile(path, []byte("approval_webhook: ./bin/notify\n"), 0o644))
	cfg, err = config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, "bin/notify")}, cfg.ApprovalWebhook.Command)

	require.NoError(t, os.WriteFile(path, []byte("approval_webhook:\n  - /usr/local/bin/notify\n"), 0o644))
	cfg, err = config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"/usr/local/bin/notify"}, cfg.ApprovalWebhook.Command)

	require.NoError(t, os.WriteFile(path, []byte("approval_webhook: ftp://hooks.example/pending\n"), 0o644))
	_, err = config.Load(path)
	assert.ErrorContains(t, err, "approval_webhook")
}

func TestCallersNameEnvVars(t *testing.T) {
	path := filepath.Join(t.TempDir(), "veto.yaml")
	require.NoError(t, os.WriteFile(path, []byte("callers:\n  ada: ADA_CALLER_TOKEN\n  grace: GRACE_CALLER_TOKEN\n"), 0o644))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "ADA_CALLER_TOKEN", cfg.Callers["ada"])
	assert.Equal(t, "GRACE_CALLER_TOKEN", cfg.Callers["grace"])
}

func TestResponseFieldsLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "veto.yaml")
	require.NoError(t, os.WriteFile(path, []byte("response_fields: [id, name]\nresponse_limit: 20\n"), 0o644))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"id", "name"}, cfg.ResponseFields)
	assert.Equal(t, 20, cfg.ResponseLimit)

	path = filepath.Join(t.TempDir(), "veto.yaml")
	require.NoError(t, os.WriteFile(path, []byte("response_limit: -1\n"), 0o644))
	_, err = config.Load(path)
	assert.ErrorContains(t, err, "response_limit")
}

func TestUnusableProviderKeysFail(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "memory file without a path", body: "memory: file\n", want: "memory_file"},
		{name: "subagents", body: "subagents: on\n", want: "subagents"},
		{name: "unknown policy", body: "policy: other\n", want: "unsupported policy provider"},
		{name: "unknown execution", body: "execution: other\n", want: "not in this slice"},
		{name: "unknown decision", body: "decision: other\n", want: "not in this slice"},
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

func TestUnknownConfigField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "veto.yaml")
	require.NoError(t, os.WriteFile(path, []byte("polciy: builtin\n"), 0o644))
	_, err := config.Load(path)
	assert.ErrorContains(t, err, `unknown config field "polciy"`)

	require.NoError(t, os.WriteFile(path, []byte("auth:\n  bearer:\n    source: env\n    env: TOKEN\n    nope: 1\n"), 0o644))
	_, err = config.Load(path)
	assert.ErrorContains(t, err, `unknown config field "nope"`)
}
