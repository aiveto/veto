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
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/capability"
	"github.com/aiveto/veto/execute"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLISearchDescribeInvokeMatchMCP(t *testing.T) {
	loop, err := testCapabilityLoop(t)
	require.NoError(t, err)
	srv := newServer(loop)

	in, err := decodeSearch([]string{"retire order 123"})
	require.NoError(t, err)
	searchRaw, err := srv.RunSearch(in)
	require.NoError(t, err)
	mcpSearch, err := srv.RunSearch(capability.SearchArgs{Query: "retire order 123"})
	require.NoError(t, err)
	assert.JSONEq(t, string(mcpSearch), string(searchRaw))
	var hits []capability.SearchHit
	require.NoError(t, json.Unmarshal(searchRaw, &hits))
	require.NotEmpty(t, hits)
	assert.Equal(t, "orders.delete", hits[0].ID)
	assert.True(t, hits[0].Confirmation)

	jsonIn, err := decodeSearch([]string{`{"query":"retire order 123"}`})
	require.NoError(t, err)
	assert.Equal(t, in, jsonIn)

	descIn, err := decodeDescribe([]string{"orders.get"})
	require.NoError(t, err)
	descRaw, err := srv.RunDescribe(descIn)
	require.NoError(t, err)
	mcpDesc, err := srv.RunDescribe(capability.DescribeArgs{OperationID: "orders.get"})
	require.NoError(t, err)
	assert.JSONEq(t, string(mcpDesc), string(descRaw))
	assert.Contains(t, string(descRaw), "Order.customerId identifies customers.get")

	jsonInvoke, err := decodeInvoke([]string{`{"operation_id":"orders.delete","params":{"id":"123"}}`})
	require.NoError(t, err)
	shortInvoke, err := decodeInvoke([]string{"orders.delete", `{"id":"123"}`})
	require.NoError(t, err)
	assert.Equal(t, jsonInvoke, shortInvoke)
}

func TestCLIInvokeDeleteWaits(t *testing.T) {
	loop, err := testCapabilityLoop(t)
	require.NoError(t, err)
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	defer ts.Close()
	loop.RuntimePtr().Exec = execute.Client{BaseURL: ts.URL, HTTP: ts.Client()}
	srv := newServer(loop)
	in, err := decodeInvoke([]string{`{"operation_id":"orders.delete","params":{"id":"123"}}`})
	require.NoError(t, err)
	ctx := auth.WithCaller(context.Background(), auth.OrLocal(""))
	raw, err := srv.RunInvoke(ctx, in)
	require.NoError(t, err)
	var res capability.InvokeResult
	require.NoError(t, json.Unmarshal(raw, &res))
	assert.Equal(t, "confirmation_required", res.Status)
	assert.Equal(t, "held until you approve", res.Why)
	assert.False(t, res.HTTP)
	assert.Equal(t, int32(0), hits.Load())
}

func TestCLIInvokeTTYAcceptRunsOnce(t *testing.T) {
	loop, err := testCapabilityLoop(t)
	require.NoError(t, err)
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	loop.RuntimePtr().Exec = execute.Client{BaseURL: ts.URL, HTTP: ts.Client()}
	srv := newServer(loop)
	in, err := decodeInvoke([]string{`{"operation_id":"orders.delete","params":{"id":"123"}}`})
	require.NoError(t, err)
	ctx := auth.WithCaller(context.Background(), auth.OrLocal(""))
	var asked string
	confirmInvoke = func(sentence string) (bool, bool) {
		asked = sentence
		return true, true
	}
	t.Cleanup(func() { confirmInvoke = confirmInvokeTTY })
	raw, err := runInvoke(ctx, srv, in)
	require.NoError(t, err)
	var res capability.InvokeResult
	require.NoError(t, json.Unmarshal(raw, &res))
	assert.Equal(t, "ok", res.Status)
	assert.True(t, res.HTTP)
	assert.True(t, res.Sent)
	assert.Contains(t, asked, "orders.delete")
	assert.Contains(t, asked, "id=123")
	assert.Equal(t, int32(1), hits.Load())
}

func TestCLIInvokeTTYDeclineLeavesPending(t *testing.T) {
	loop, err := testCapabilityLoop(t)
	require.NoError(t, err)
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	defer ts.Close()
	loop.RuntimePtr().Exec = execute.Client{BaseURL: ts.URL, HTTP: ts.Client()}
	srv := newServer(loop)
	in, err := decodeInvoke([]string{`{"operation_id":"orders.delete","params":{"id":"123"}}`})
	require.NoError(t, err)
	ctx := auth.WithCaller(context.Background(), auth.OrLocal(""))
	confirmInvoke = func(string) (bool, bool) { return false, true }
	t.Cleanup(func() { confirmInvoke = confirmInvokeTTY })
	raw, err := runInvoke(ctx, srv, in)
	require.NoError(t, err)
	var res capability.InvokeResult
	require.NoError(t, json.Unmarshal(raw, &res))
	assert.Equal(t, "confirmation_required", res.Status)
	assert.NotEmpty(t, res.ApprovalID)
	assert.False(t, res.HTTP)
	assert.Equal(t, int32(0), hits.Load())
}

func TestConfirmInvokeTTYNeedsStdinAndStdout(t *testing.T) {
	invokeIsTTY = func() bool { return false }
	t.Cleanup(func() { invokeIsTTY = detectInvokeTTY })
	accepted, asked := confirmInvokeTTY("delete orders.delete id=123?")
	assert.False(t, accepted)
	assert.False(t, asked)
}

func TestIsCharDevice(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "plain")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	assert.False(t, isCharDevice(f))
	assert.False(t, isCharDevice(nil))
}

func TestCLIInvokeWithoutTTYLeavesPending(t *testing.T) {
	loop, err := testCapabilityLoop(t)
	require.NoError(t, err)
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	defer ts.Close()
	loop.RuntimePtr().Exec = execute.Client{BaseURL: ts.URL, HTTP: ts.Client()}
	srv := newServer(loop)
	in, err := decodeInvoke([]string{`{"operation_id":"orders.delete","params":{"id":"123"}}`})
	require.NoError(t, err)
	ctx := auth.WithCaller(context.Background(), auth.OrLocal(""))
	raw, err := runInvoke(ctx, srv, in)
	require.NoError(t, err)
	var res capability.InvokeResult
	require.NoError(t, json.Unmarshal(raw, &res))
	assert.Equal(t, "confirmation_required", res.Status)
	assert.NotEmpty(t, res.ApprovalID)
	assert.False(t, res.HTTP)
	assert.Equal(t, int32(0), hits.Load())
}

func TestCapabilityHelpJSONIsTheMCPContract(t *testing.T) {
	root, err := newRoot()
	require.NoError(t, err)
	var buf bytes.Buffer
	got, err := jsonHelp(&buf, root, []string{"search", "--help-json"})
	require.NoError(t, err)
	require.True(t, got)
	var spec struct {
		Name        string `json:"name"`
		Command     string `json:"command"`
		Description string `json:"description"`
		Input       []struct {
			Name     string `json:"name"`
			Required bool   `json:"required"`
		} `json:"input"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &spec))
	assert.Equal(t, capability.SearchName, spec.Name)
	assert.Equal(t, capability.SearchCommand, spec.Command)
	assert.Equal(t, capability.SearchDescription, spec.Description)
	input := make([]string, 0, len(spec.Input))
	queryRequired := false
	for _, f := range spec.Input {
		input = append(input, f.Name)
		if f.Name == "query" {
			queryRequired = f.Required
		}
	}
	assert.Contains(t, input, "query")
	assert.Contains(t, input, "offset")
	assert.True(t, queryRequired)
	assert.NotContains(t, input, "config")
	assert.NotContains(t, buf.String(), `"flags"`)

	buf.Reset()
	got, err = jsonHelp(&buf, root, []string{"invoke", "--help-json"})
	require.NoError(t, err)
	require.True(t, got)
	require.NoError(t, json.Unmarshal(buf.Bytes(), &spec))
	assert.Equal(t, capability.InvokeName, spec.Name)
	input = make([]string, 0, len(spec.Input))
	for _, f := range spec.Input {
		input = append(input, f.Name)
	}
	assert.Contains(t, input, "operation_id")
	assert.Contains(t, input, "params")
	assert.Contains(t, input, "idempotency_key")
	assert.Contains(t, spec.Description, "pending_approval")

	buf.Reset()
	got, err = jsonHelp(&buf, root, []string{"--help-json"})
	require.NoError(t, err)
	require.True(t, got)
	var help capability.Help
	require.NoError(t, json.Unmarshal(buf.Bytes(), &help))
	require.Len(t, help.Capabilities, 3)
	assert.Equal(t, capability.SearchName, help.Capabilities[0].Name)
	assert.NotContains(t, buf.String(), `"command": "serve"`)
}

func testCapabilityLoop(t *testing.T) (*agent.Loop, error) {
	t.Helper()
	orders, err := filepath.Abs("../../testdata/orders.yaml")
	if err != nil {
		return nil, err
	}
	customers, err := filepath.Abs("../../testdata/customers.yaml")
	if err != nil {
		return nil, err
	}
	relations, err := filepath.Abs("../../testdata/relations.yaml")
	if err != nil {
		return nil, err
	}
	agentFile, err := filepath.Abs("../../testdata/agent.yaml")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(t.TempDir(), "veto.yaml")
	body := fmt.Sprintf("contracts:\n  - %s\n  - %s\nrelations_file: %s\nagent_file: %s\n", orders, customers, relations, agentFile)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return nil, err
	}
	loop, _, err := buildLoop(nil, path, "", "", "")
	return loop, err
}

func TestSearchStartsWithoutAModelKey(t *testing.T) {
	orders, err := filepath.Abs("../../testdata/orders.yaml")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "veto.yaml")
	require.NoError(t, os.WriteFile(path, fmt.Appendf(nil, "contracts:\n  - %s\nmodel: openai\n", orders), 0o600))
	t.Setenv("OPENAI_API_KEY", "")
	srv, _, err := buildServer(nil, path, "", "", "")
	require.NoError(t, err)
	hits := srv.Search("order", 0, 8)
	require.NotEmpty(t, hits)
}
