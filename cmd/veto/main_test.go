package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/agent/openai"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/config"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/mcpserver"
	"github.com/aiveto/veto/memory"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runtime"
	"github.com/aiveto/veto/telemetry"
	"github.com/alicebob/miniredis/v2"
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
	var help struct {
		Capabilities []struct {
			Name    string `json:"name"`
			Command string `json:"command"`
		} `json:"capabilities"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &help))
	require.Len(t, help.Capabilities, 3)
	assert.Equal(t, "capabilities_search", help.Capabilities[0].Name)
	assert.Equal(t, "search", help.Capabilities[0].Command)
	assert.NotContains(t, buf.String(), `"command": "serve"`)
}

func TestApproveReadsStoreFromConfig(t *testing.T) {
	srv := miniredis.RunT(t)
	t.Setenv("VALKEY_URL", "redis://"+srv.Addr())
	t.Setenv("VETO_APPROVAL_STORE", "")
	t.Setenv("VETO_APPROVAL_SECRET", "test-secret")
	t.Setenv("VETO_APPROVAL_NONCE_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "veto.yaml")
	require.NoError(t, os.WriteFile(path, []byte("approval_store: VALKEY_URL\napproval_ttl: 30m\n"), 0o600))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	caller := policy.NewState()
	require.NoError(t, applyApprovalConfig(t.Context(), caller, cfg))
	pending, err := caller.RequestFor(t.Context(), "", "orders.delete", map[string]string{"id": "123"})
	require.NoError(t, err)
	approved, err := approveID(t.Context(), pending, path)
	require.NoError(t, err)
	other := policy.NewState()
	require.NoError(t, applyApprovalConfig(t.Context(), other, cfg))
	ok, err := other.ConsumeFor(t.Context(), "", approved, "orders.delete", map[string]string{"id": "123"})
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = caller.ConsumeFor(t.Context(), "", approved, "orders.delete", map[string]string{"id": "123"})
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestSignedApprovalIsOneUseOnValkey(t *testing.T) {
	srv := miniredis.RunT(t)
	t.Setenv("VETO_APPROVAL_STORE", "redis://"+srv.Addr())
	t.Setenv("VETO_APPROVAL_SECRET", "test-secret")
	issuer := policy.NewState()
	require.NoError(t, applyApprovalEnv(t.Context(), issuer))
	pending, err := issuer.RequestFor(t.Context(), "", "orders.delete", map[string]string{"id": "123"})
	require.NoError(t, err)
	approved, err := issuer.Approve(t.Context(), pending)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(approved, "v1."))
	var wins atomic.Int32
	var wg sync.WaitGroup
	errCh := make(chan error, 24)
	for range 24 {
		wg.Go(func() {
			other := policy.NewState()
			if err := applyApprovalEnv(t.Context(), other); err != nil {
				errCh <- err
				return
			}
			ok, err := other.ConsumeFor(t.Context(), "", approved, "orders.delete", map[string]string{"id": "123"})
			if err != nil {
				errCh <- err
				return
			}
			if ok {
				wins.Add(1)
			}
		})
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), wins.Load())
}

func TestPinRegistersADirectTool(t *testing.T) {
	opt := serveOptions(serveCmd{pin: []string{"orders.get"}})
	assert.True(t, opt.DirectPins)
	assert.Equal(t, []string{"orders.get"}, opt.Pins)
	assert.False(t, serveOptions(serveCmd{}).DirectPins)
}

func TestApprovalStoreIsSharedAcrossStates(t *testing.T) {
	srv := miniredis.RunT(t)
	t.Setenv("VETO_APPROVAL_STORE", "redis://"+srv.Addr())
	t.Setenv("VETO_APPROVAL_SECRET", "test-secret")
	t.Setenv("VETO_APPROVAL_NONCE_DIR", t.TempDir())
	caller := policy.NewState()
	require.NoError(t, applyApprovalEnv(t.Context(), caller))
	pending, err := caller.RequestFor(t.Context(), "", "orders.delete", map[string]string{"id": "123"})
	require.NoError(t, err)
	approved, err := approveID(t.Context(), pending, "")
	require.NoError(t, err)
	other := policy.NewState()
	require.NoError(t, applyApprovalEnv(t.Context(), other))
	ok, err := other.ConsumeFor(t.Context(), "", approved, "orders.delete", map[string]string{"id": "123"})
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = caller.ConsumeFor(t.Context(), "", approved, "orders.delete", map[string]string{"id": "123"})
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestApproveRecordsAnIDTheCallerCannotMint(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VETO_APPROVAL_NONCE_DIR", dir)
	t.Setenv("VETO_APPROVAL_SECRET", "test-secret")
	caller := policy.NewState()
	require.NoError(t, applyApprovalEnv(t.Context(), caller))
	pending, err := caller.RequestFor(t.Context(), "", "orders.delete", map[string]string{"id": "123"})
	require.NoError(t, err)
	approved, err := approveID(t.Context(), pending, "")
	require.NoError(t, err)
	assert.NotEqual(t, pending, approved)
	assert.True(t, strings.HasPrefix(approved, "v1."))
	ok, err := caller.ConsumeFor(t.Context(), "", pending, "orders.delete", map[string]string{"id": "123"})
	require.NoError(t, err)
	assert.False(t, ok)
	ok, err = caller.ConsumeFor(t.Context(), "", approved, "orders.delete", map[string]string{"id": "123"})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestConfigIsTheCatalog(t *testing.T) {
	src, err := resolve("../../testdata/veto.yaml", nil, "", "")
	require.NoError(t, err)
	cat, err := loadCatalog(src.contracts, src.relations)
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
	fileText := fmt.Sprintf(`memory: file
memory_file: turns.log
contracts:
  - %s
`, contract)
	require.NoError(t, os.WriteFile(fileCfg, []byte(fileText), 0o600))
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
			text := fmt.Sprintf(`%scontracts:
  - %s
`, tc.yaml, contract)
			require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
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
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(`contracts:
  - %s
`, contract)), 0o600))
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

func TestVersionPrintsTheBuild(t *testing.T) {
	root, err := newRoot()
	require.NoError(t, err)
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"version"})
	require.NoError(t, root.Execute())
	assert.Equal(t, "veto "+mcpserver.Version+"\n", buf.String())

	buf.Reset()
	root.SetArgs([]string{"--version"})
	require.NoError(t, root.Execute())
	assert.Equal(t, "veto "+mcpserver.Version+"\n", buf.String())
}

func TestAuthSecretComesFromTheEnv(t *testing.T) {
	contract, err := filepath.Abs("../../testdata/orders.yaml")
	require.NoError(t, err)
	dir := t.TempDir()
	path := filepath.Join(dir, "veto.yaml")
	text := fmt.Sprintf(`auth:
  bearerAuth: ORDER_TOKEN
contracts:
  - %s
`, contract)
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	t.Setenv("ORDER_TOKEN", "s3cret")
	loop, cfg, err := buildLoop(nil, path, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, "ORDER_TOKEN", cfg.Auth["bearerAuth"].Env)
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
		{name: "unknown policy", body: "policy: other\n", want: "unsupported policy provider"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "veto.yaml")
			require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(`%scontracts:
  - %s
`, tc.body, contract)), 0o600))
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

func TestEvalPinsTheScriptedModel(t *testing.T) {
	contract, err := filepath.Abs("../../testdata/orders.yaml")
	require.NoError(t, err)
	conf := filepath.Join(t.TempDir(), "veto.yaml")
	require.NoError(t, os.WriteFile(conf, []byte(fmt.Sprintf("model: openai\ncontracts:\n  - %s\n", contract)), 0o600))
	t.Setenv("OPENAI_API_KEY", "")
	_, _, err = buildLoop(nil, conf, "", "", "")
	require.ErrorContains(t, err, "API key")
	loop, _, err := buildEvalLoop(nil, conf, "", "", "")
	require.NoError(t, err)
	_, ok := loop.Model.(*agent.Scripted)
	assert.True(t, ok)
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
	require.NoError(t, os.WriteFile(spec, []byte(body), 0o600))
	conf := filepath.Join(dir, "veto.yaml")
	text := fmt.Sprintf(`auth:
  bearerAuth: ORDER_TOKEN
contracts:
  - %s
`, spec)
	require.NoError(t, os.WriteFile(conf, []byte(text), 0o600))
	t.Setenv("VETO_TOKEN_DIR", t.TempDir())
	t.Setenv("ORDER_TOKEN", "")
	secured, loaded, err := buildLoop(nil, conf, "", "", "")
	require.NoError(t, err)
	missing := doctorBlockers(context.Background(), secured.Catalog, loaded, nil, true)
	report := strings.Join(missing, "\n")
	assert.Contains(t, report, "ORDER_TOKEN is unset")
	assert.NotContains(t, report, "ping ")
	t.Setenv("ORDER_TOKEN", "s3cret")
	setLines, setFail := doctorReport(context.Background(), secured.Catalog, loaded, nil, false)
	set := strings.Join(setLines, "\n")
	assert.NotContains(t, set, "s3cret")
	assert.NotContains(t, set, "unset")
	assert.NotContains(t, set, "missing auth")
	assert.Contains(t, set, "ping: empty summary")
	assert.False(t, setFail)
	assert.Contains(t, report, "ping: missing auth bearerAuth")

	downSpec := filepath.Join(dir, "down.yaml")
	down := strings.ReplaceAll(securedSpec, "http://example.test", "http://127.0.0.1:1")
	require.NoError(t, os.WriteFile(downSpec, []byte(down), 0o600))
	downConf := filepath.Join(dir, "down.yaml.conf")
	require.NoError(t, os.WriteFile(downConf, []byte(fmt.Sprintf(`contracts:
  - %s
`, downSpec)), 0o600))
	downLoop, downCfg, err := buildLoop(nil, downConf, "", "", "")
	require.NoError(t, err)
	blocked := doctorBlockers(context.Background(), downLoop.Catalog, downCfg, nil, true)
	assert.Contains(t, strings.Join(blocked, "\n"), "ping ")
	assert.Contains(t, strings.Join(blocked, "\n"), "upstream")
}

func TestDoctorReportsCallRisks(t *testing.T) {
	risks := loadDoctorCatalog(t, doctorRiskSpec)
	lines, fail := doctorReport(context.Background(), risks, config.File{}, nil, false)
	report := strings.Join(lines, "\n")
	assert.True(t, fail)
	assert.Contains(t, report, "orders.get: fallback id")
	assert.Contains(t, report, "colliding id orders.get")
	assert.Contains(t, report, "search.find: parameter session cannot be serialized")
	assert.Contains(t, report, "labels.get: parameter id cannot be serialized: style matrix")
	assert.Contains(t, report, "ping: empty summary")
	assert.Contains(t, report, "items.create: weak summary")
	assert.Contains(t, report, "items.delete: write requires approval")
	assert.Contains(t, report, "missing auth bearerAuth")
	assert.NotContains(t, report, "ping: missing auth")
	assert.Equal(t, 1, strings.Count(report, "missing auth"))

	notes := loadDoctorCatalog(t, doctorNoteSpec)
	noteLines, noteFail := doctorReport(context.Background(), notes, config.File{}, nil, false)
	noteReport := strings.Join(noteLines, "\n")
	assert.False(t, noteFail)
	assert.Contains(t, noteReport, "orders.get: fallback id")
	assert.Contains(t, noteReport, "orders.get: weak summary")
	assert.Contains(t, noteReport, "items.delete: write requires approval")
	assert.NotContains(t, noteReport, "missing auth")
	assert.NotContains(t, noteReport, "colliding id")
	assert.NotContains(t, noteReport, "cannot be serialized")
}

func TestPreviewDoesNotCallUpstreamOrTokenURL(t *testing.T) {
	var tokenHits, upstreamHits atomic.Int32
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenHits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "preview-access-token", "expires_in": 3600})
	}))
	defer tokenSrv.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer up.Close()

	const secret = "preview-client-secret"
	const querySecret = "query-secret"
	const bodySecret = "body-secret"
	t.Setenv("PREVIEW_SECRET", secret)
	dir := t.TempDir()
	spec := filepath.Join(dir, "api.yaml")
	require.NoError(t, os.WriteFile(spec, []byte(strings.ReplaceAll(previewSpec, "http://upstream.example", up.URL)), 0o600))
	conf := filepath.Join(dir, "veto.yaml")
	text := fmt.Sprintf(`auth:
  bearerAuth:
    source: client_credentials
    token_url: %s
    client_id: job
    client_secret_env: PREVIEW_SECRET
contracts:
  - %s
`, tokenSrv.URL, spec)
	require.NoError(t, os.WriteFile(conf, []byte(text), 0o600))
	loop, _, err := buildLoop(nil, conf, "", "", "")
	require.NoError(t, err)
	rt := loop.Runtime()
	args, err := paramArgs([]string{"body={\"name\":\"ada\",\"password\":\"" + bodySecret + "\"}", "token=" + querySecret})
	require.NoError(t, err)
	out, err := rt.Preview(context.Background(), runtime.Request{Operation: "orders.create", Arguments: args})
	require.NoError(t, err)
	assert.Empty(t, out.Errors)
	assert.Equal(t, "allow", out.Decision)
	assert.False(t, out.ApprovalRequired)
	assert.Equal(t, "orders.create", out.OperationID)
	assert.Equal(t, http.MethodPost, out.Request.Method)
	assert.Contains(t, out.Request.URL, up.URL)
	assert.NotContains(t, out.Request.URL, tokenSrv.URL)
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	textOut := string(raw)
	assert.NotContains(t, textOut, secret)
	assert.NotContains(t, textOut, querySecret)
	assert.NotContains(t, textOut, bodySecret)
	assert.NotContains(t, textOut, "preview-access-token")
	assert.Contains(t, textOut, "ada")
	assert.Contains(t, out.Request.Body, "REDACTED")
	assert.Equal(t, int32(0), tokenHits.Load())
	assert.Equal(t, int32(0), upstreamHits.Load())

	deleted, err := rt.Preview(context.Background(), runtime.Request{
		Operation: "orders.delete",
		Arguments: runtime.FromStrings(map[string]string{"id": "123"}),
	})
	require.NoError(t, err)
	assert.Empty(t, deleted.Errors)
	assert.Equal(t, "confirmation_required", deleted.Decision)
	assert.True(t, deleted.ApprovalRequired)
	assert.Equal(t, int32(0), tokenHits.Load())
	assert.Equal(t, int32(0), upstreamHits.Load())

	missing, err := rt.Preview(context.Background(), runtime.Request{Operation: "orders.get"})
	require.NoError(t, err)
	assert.Contains(t, strings.Join(missing.Errors, "\n"), "id required")
	assert.Equal(t, "orders.get", missing.OperationID)
	assert.Empty(t, missing.Decision)
	assert.Equal(t, int32(0), tokenHits.Load())
	assert.Equal(t, int32(0), upstreamHits.Load())

	sent, err := rt.Invoke(context.Background(), runtime.Request{Operation: "orders.create", Arguments: args})
	require.NoError(t, err)
	assert.Equal(t, "ok", sent.Status)
	assert.Positive(t, tokenHits.Load())
	assert.Positive(t, upstreamHits.Load())
}

func loadDoctorCatalog(t *testing.T, body string) *catalog.Catalog {
	t.Helper()
	dir := t.TempDir()
	spec := filepath.Join(dir, "api.yaml")
	require.NoError(t, os.WriteFile(spec, []byte(body), 0o600))
	conf := filepath.Join(dir, "veto.yaml")
	require.NoError(t, os.WriteFile(conf, []byte(fmt.Sprintf(`contracts:
  - %s
`, spec)), 0o600))
	loop, _, err := buildLoop(nil, conf, "", "", "")
	require.NoError(t, err)
	return loop.Catalog
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

const doctorRiskSpec = `openapi: 3.0.3
info:
  title: risks
  version: "1"
servers:
  - url: http://example.test
paths:
  /orders:
    get:
      summary: List orders for the account
      responses:
        "204":
          description: ok
  /orders/{id}:
    get:
      operationId: orders.get
      summary: Get one order by its id
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "204":
          description: ok
  /items:
    post:
      operationId: items.create
      summary: Create
      responses:
        "201":
          description: created
  /items/{id}:
    delete:
      operationId: items.delete
      summary: Delete one item by its id
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "204":
          description: ok
  /search:
    get:
      operationId: search.find
      summary: Find items that match a query
      parameters:
        - name: session
          in: cookie
          schema:
            type: string
      responses:
        "200":
          description: ok
  /labels/{id}:
    get:
      operationId: labels.get
      summary: Get one label by its id
      parameters:
        - name: id
          in: path
          required: true
          style: matrix
          schema:
            type: string
      responses:
        "204":
          description: ok
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

const doctorNoteSpec = `openapi: 3.0.3
info:
  title: notes
  version: "1"
paths:
  /orders:
    get:
      summary: List
      responses:
        "204":
          description: ok
  /items/{id}:
    delete:
      operationId: items.delete
      summary: Delete one item by its id
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "204":
          description: Deleted
`

const previewSpec = `openapi: 3.0.3
info:
  title: preview
  version: "1"
servers:
  - url: http://upstream.example
paths:
  /orders:
    post:
      operationId: orders.create
      summary: Create one order for a customer
      parameters:
        - name: token
          in: query
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
      responses:
        "201":
          description: created
  /orders/{id}:
    get:
      operationId: orders.get
      summary: Get one order by its id
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
    delete:
      operationId: orders.delete
      summary: Delete one order by its id
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "204":
          description: deleted
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
security:
  - bearerAuth: []
`
