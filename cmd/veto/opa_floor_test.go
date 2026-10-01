package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfiguredOPAConfirmationKeepsTheAllowlist(t *testing.T) {
	t.Setenv("VETO_APPROVAL_NONCE_DIR", t.TempDir())
	t.Setenv("VETO_APPROVAL_SECRET", "")
	dir := t.TempDir()
	spec := `openapi: 3.0.3
info: {title: orders, version: "1"}
servers:
  - url: http://127.0.0.1:9
paths:
  /orders/{id}:
    get:
      operationId: orders.get
      summary: Get one order
      parameters:
        - name: id
          in: path
          required: true
          schema: {type: string}
      responses:
        "200": {description: ok}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "api.yaml"), []byte(spec), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(`operations:
  - operation: orders.get
    permissions: [orders.read]
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "policy.rego"), []byte(`package veto

import rego.v1

default decision := "confirmation"
default reason := ""
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "veto.yaml"), []byte(`policy: opa
policy_file: policy.rego
permissions: []
agent_file: agent.yaml
contracts:
  - api.yaml
`), 0o600))

	loop, _, err := buildLoop(nil, filepath.Join(dir, "veto.yaml"), "", "", "")
	require.NoError(t, err)
	var hits atomic.Int32
	loop.Exec = floorHit{hits: &hits}
	out, err := loop.Invoke(context.Background(), "orders.get", map[string]string{"id": "123"}, "")
	require.NoError(t, err)
	assert.Equal(t, "denied", out.Status)
	assert.Equal(t, int32(0), hits.Load())

	pending, err := loop.State.RequestFor("", "orders.get", map[string]string{"id": "123"})
	require.NoError(t, err)
	approved, err := loop.State.Approve(pending)
	require.NoError(t, err)
	again, err := loop.Invoke(context.Background(), "orders.get", map[string]string{"id": "123"}, approved)
	require.NoError(t, err)
	assert.Equal(t, "denied", again.Status)
	assert.Equal(t, int32(0), hits.Load())
}

type floorHit struct{ hits *atomic.Int32 }

func (e floorHit) InvokeHTTPResult(context.Context, *catalog.Operation, map[string]string) (result.HTTPResult, error) {
	e.hits.Add(1)
	return result.HTTPResult{Status: http.StatusNoContent, Code: "ok"}, nil
}
