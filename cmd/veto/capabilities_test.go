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
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/mcpserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLISearchDescribeInvokeMatchMCP(t *testing.T) {
	loop, err := testCapabilityLoop(t)
	require.NoError(t, err)
	srv := capabilityServer(loop)

	in, err := decodeSearch([]string{"retire order 123"})
	require.NoError(t, err)
	searchRaw, err := srv.RunSearch(in)
	require.NoError(t, err)
	mcpSearch, err := srv.RunSearch(mcpserver.SearchArgs{Query: "retire order 123"})
	require.NoError(t, err)
	assert.JSONEq(t, string(mcpSearch), string(searchRaw))
	var hits []mcpserver.SearchHit
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
	mcpDesc, err := srv.RunDescribe(mcpserver.DescribeArgs{OperationID: "orders.get"})
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
	loop.Exec = execute.Client{BaseURL: ts.URL, HTTP: ts.Client()}
	srv := capabilityServer(loop)
	in, err := decodeInvoke([]string{`{"operation_id":"orders.delete","params":{"id":"123"}}`})
	require.NoError(t, err)
	ctx := auth.WithCaller(context.Background(), callerName(""))
	raw, err := srv.RunInvoke(ctx, in)
	require.NoError(t, err)
	var res mcpserver.InvokeResult
	require.NoError(t, json.Unmarshal(raw, &res))
	assert.Equal(t, "confirmation_required", res.Status)
	assert.Equal(t, "held until you approve", res.Why)
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
		Flags []struct {
			Name string `json:"name"`
		} `json:"flags"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &spec))
	assert.Equal(t, mcpserver.SearchName, spec.Name)
	assert.Equal(t, mcpserver.SearchCommand, spec.Command)
	assert.Equal(t, mcpserver.SearchDescription, spec.Description)
	input := make([]string, 0, len(spec.Input))
	flags := make([]string, 0, len(spec.Flags))
	queryRequired := false
	for _, f := range spec.Input {
		input = append(input, f.Name)
		if f.Name == "query" {
			queryRequired = f.Required
		}
	}
	for _, f := range spec.Flags {
		flags = append(flags, f.Name)
	}
	assert.Contains(t, input, "query")
	assert.Contains(t, input, "offset")
	assert.True(t, queryRequired)
	assert.Contains(t, flags, "config")
	assert.NotContains(t, input, "config")
	assert.NotContains(t, flags, "query")

	buf.Reset()
	got, err = jsonHelp(&buf, root, []string{"invoke", "--help-json"})
	require.NoError(t, err)
	require.True(t, got)
	require.NoError(t, json.Unmarshal(buf.Bytes(), &spec))
	assert.Equal(t, mcpserver.InvokeName, spec.Name)
	input = make([]string, 0, len(spec.Input))
	for _, f := range spec.Input {
		input = append(input, f.Name)
	}
	assert.Contains(t, input, "operation_id")
	assert.Contains(t, input, "params")

	buf.Reset()
	got, err = jsonHelp(&buf, root, []string{"--help-json"})
	require.NoError(t, err)
	require.True(t, got)
	assert.Contains(t, buf.String(), `"command": "search"`)
	assert.Contains(t, buf.String(), mcpserver.SearchName)
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
