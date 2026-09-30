package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiveto/veto/agent/openai"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/memory"
	"github.com/aiveto/veto/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelpJSONStaysOffTheHumanHelpPath(t *testing.T) {
	root, err := newRoot()
	require.NoError(t, err)
	var buf bytes.Buffer
	got, err := jsonHelp(&buf, root, []string{"serve", "--help"})
	require.NoError(t, err)
	assert.False(t, got)
	got, err = jsonHelp(&buf, root, []string{"serve", "--help-json"})
	require.NoError(t, err)
	require.True(t, got)
	var doc struct {
		Command string `json:"command"`
		Flags   []struct {
			Name string `json:"name"`
		} `json:"flags"`
		Commands []string `json:"commands"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))
	assert.Equal(t, "serve", doc.Command)
	assert.Empty(t, doc.Commands)
	var sawStdio bool
	for _, f := range doc.Flags {
		if f.Name == "stdio" {
			sawStdio = true
		}
	}
	assert.True(t, sawStdio)
	buf.Reset()
	got, err = jsonHelp(&buf, root, []string{"--help-json"})
	require.NoError(t, err)
	require.True(t, got)
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))
	for _, name := range []string{"serve", "eval", "replay", "validate", "generate", "pack", "doctor", "check"} {
		assert.Contains(t, doc.Commands, name)
	}
}

func TestConfigIsTheCatalog(t *testing.T) {
	_, contracts, relations, _, err := resolve("../../testdata/veto.yaml", nil, "", "")
	require.NoError(t, err)
	cat, err := loadCatalog(contracts, relations)
	require.NoError(t, err)
	require.NotNil(t, cat.ByID("orders.get"))
	require.NotNil(t, cat.ByID("customers.get"))
	matches := catalog.Search(cat, "customerId", nil)
	var joined bool
	for _, m := range matches {
		if m.Operation.ID != "orders.get" {
			continue
		}
		for _, id := range m.Related {
			if id == "customers.get" {
				joined = true
			}
		}
	}
	assert.True(t, joined)
	assert.NotContains(t, cat.IndexLine(), "openapi:")
}

func TestBuildLoopConstructsDefaultsAndOpenAIHost(t *testing.T) {
	contract, err := filepath.Abs("../../testdata/orders.yaml")
	require.NoError(t, err)
	dir := t.TempDir()
	fileCfg := filepath.Join(dir, "file.yaml")
	fileText := "memory: file\nmemory_file: turns.log\ncontracts:\n  - " + contract + "\n"
	require.NoError(t, os.WriteFile(fileCfg, []byte(fileText), 0o644))
	fileLoop, _, err := buildLoop(nil, fileCfg, "", "", "")
	require.NoError(t, err)
	require.IsType(t, &memory.Log{}, fileLoop.Memory)

	t.Setenv("OPENAI_API_KEY", "test-key")
	for _, tc := range []struct {
		name string
		yaml string
		want string
	}{
		{name: "empty", yaml: "model: openai\n", want: "https://api.openai.com/v1"},
		{name: "set", yaml: "model: openai\nmodel_base_url: http://127.0.0.1:9/v1\nmodel_name: gpt-test\n", want: "http://127.0.0.1:9/v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".yaml")
			text := tc.yaml + "contracts:\n  - " + contract + "\n"
			require.NoError(t, os.WriteFile(path, []byte(text), 0o644))
			loop, _, err := buildLoop(nil, path, "", "", "")
			require.NoError(t, err)
			live, ok := loop.Model.(*openai.Client)
			require.True(t, ok)
			assert.Equal(t, tc.want, live.BaseURL)
		})
	}
}

func TestPackPrintsTheDeleteCall(t *testing.T) {
	contract, err := filepath.Abs("../../testdata/orders.yaml")
	require.NoError(t, err)
	dir := t.TempDir()
	path := filepath.Join(dir, "veto.yaml")
	require.NoError(t, os.WriteFile(path, []byte("contracts:\n  - "+contract+"\n"), 0o644))
	loop, _, err := buildLoop(nil, path, "", "", "")
	require.NoError(t, err)
	text, err := packOutput(loop, "delete order 123", false)
	require.NoError(t, err)
	assert.Contains(t, text, "orders.delete")
	raw, err := packOutput(loop, "delete order 123", true)
	require.NoError(t, err)
	var pack struct {
		Index string `json:"Index"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &pack))
	assert.Contains(t, pack.Index, "orders.delete")
}

func TestAuthSecretComesFromTheEnv(t *testing.T) {
	contract, err := filepath.Abs("../../testdata/orders.yaml")
	require.NoError(t, err)
	dir := t.TempDir()
	path := filepath.Join(dir, "veto.yaml")
	text := "auth:\n  bearerAuth: ORDER_TOKEN\ncontracts:\n  - " + contract + "\n"
	require.NoError(t, os.WriteFile(path, []byte(text), 0o644))
	t.Setenv("ORDER_TOKEN", "s3cret")
	loop, cfg, err := buildLoop(nil, path, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, "ORDER_TOKEN", cfg.Auth["bearerAuth"])
	exec, ok := loop.Exec.(execute.Client)
	require.True(t, ok)
	assert.Equal(t, "s3cret", exec.Auth["bearerAuth"])
}

func TestFinishReplayWritesARedactedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.json")
	text, err := finishReplay([]telemetry.Span{{
		Name: "agent.run",
		Attrs: map[string]string{
			"operation.id": "orders.delete",
			"user_message": "Delete order 123",
		},
	}}, true, path)
	require.NoError(t, err)
	assert.NotContains(t, text, "Delete order 123")
	assert.Contains(t, text, "operation.id=orders.delete")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "Delete order 123")
	var buf bytes.Buffer
	root, err := newRoot()
	require.NoError(t, err)
	got, err := jsonHelp(&buf, root, []string{"replay", "--help-json"})
	require.NoError(t, err)
	require.True(t, got)
	assert.Contains(t, buf.String(), `"name": "from"`)
	assert.Contains(t, buf.String(), "Record response bodies")
	assert.NotContains(t, buf.String(), "parameter values")
	buf.Reset()
	got, err = jsonHelp(&buf, root, []string{"generate", "--help-json"})
	require.NoError(t, err)
	require.True(t, got)
	assert.Contains(t, buf.String(), "Go client")
	assert.NotContains(t, buf.String(), "typed SDK")
}

func TestTwoAPIExampleJoinsCustomers(t *testing.T) {
	cfg, err := filepath.Abs("../../examples/two-apis/veto.yaml")
	require.NoError(t, err)
	loop, _, err := buildLoop(nil, cfg, "", "", "")
	require.NoError(t, err)
	require.NotNil(t, loop.Catalog.ByID("orders.get"))
	require.NotNil(t, loop.Catalog.ByID("customers.get"))
	assert.Contains(t, strings.Join(loop.Catalog.Joins(), "\n"), "customers.get")
}

func TestExternalPolicyFailsClosed(t *testing.T) {
	contract, err := filepath.Abs("../../testdata/orders.yaml")
	require.NoError(t, err)
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "opa", body: "policy: opa\n", want: "opa"},
		{name: "temporal", body: "execution: temporal\n", want: "not in this slice"},
		{name: "jev", body: "decision: jev\n", want: "not in this slice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "veto.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.body+"contracts:\n  - "+contract+"\n"), 0o644))
			_, _, err := buildLoop(nil, path, "", "", "")
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestCheckRunsTheCaseDirectory(t *testing.T) {
	cfg, err := filepath.Abs("../../testdata/veto.yaml")
	require.NoError(t, err)
	cases, err := filepath.Abs("../../testdata/cases")
	require.NoError(t, err)
	loop, _, err := buildLoop(nil, cfg, "", "", "")
	require.NoError(t, err)
	require.NoError(t, runCases(loop, []string{cases}))
}

func TestDoctorReportsPinsAuthAndPing(t *testing.T) {
	cfgPath, err := filepath.Abs("../../testdata/veto.yaml")
	require.NoError(t, err)
	loop, cfg, err := buildLoop(nil, cfgPath, "", "", "")
	require.NoError(t, err)
	pins := doctorBlockers(context.Background(), loop.Catalog, cfg, []string{"orders.list"}, false)
	assert.Contains(t, strings.Join(pins, "\n"), "discovery-only")

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer up.Close()
	dir := t.TempDir()
	spec := filepath.Join(dir, "api.yaml")
	body := strings.ReplaceAll(securedSpec, "http://example.test", up.URL)
	require.NoError(t, os.WriteFile(spec, []byte(body), 0o644))
	conf := filepath.Join(dir, "veto.yaml")
	text := "auth:\n  bearerAuth: ORDER_TOKEN\ncontracts:\n  - " + spec + "\n"
	require.NoError(t, os.WriteFile(conf, []byte(text), 0o644))
	t.Setenv("ORDER_TOKEN", "")
	secured, loaded, err := buildLoop(nil, conf, "", "", "")
	require.NoError(t, err)
	missing := doctorBlockers(context.Background(), secured.Catalog, loaded, nil, true)
	report := strings.Join(missing, "\n")
	assert.Contains(t, report, "ORDER_TOKEN is unset")
	assert.NotContains(t, report, "ping ")
	t.Setenv("ORDER_TOKEN", "s3cret")
	set := strings.Join(doctorBlockers(context.Background(), secured.Catalog, loaded, nil, false), "\n")
	assert.NotContains(t, set, "s3cret")
	assert.NotContains(t, set, "unset")

	downSpec := filepath.Join(dir, "down.yaml")
	down := strings.ReplaceAll(securedSpec, "http://example.test", "http://127.0.0.1:1")
	require.NoError(t, os.WriteFile(downSpec, []byte(down), 0o644))
	downConf := filepath.Join(dir, "down.yaml.conf")
	require.NoError(t, os.WriteFile(downConf, []byte("contracts:\n  - "+downSpec+"\n"), 0o644))
	downLoop, downCfg, err := buildLoop(nil, downConf, "", "", "")
	require.NoError(t, err)
	blocked := doctorBlockers(context.Background(), downLoop.Catalog, downCfg, nil, true)
	assert.Contains(t, strings.Join(blocked, "\n"), "ping ")
}

const securedSpec = `openapi: 3.0.3
info:
  title: secured
  version: "1"
servers:
  - url: http://example.test
paths:
  /ping:
    get:
      operationId: ping
      responses:
        "204":
          description: ok
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
security:
  - bearerAuth: []
`
