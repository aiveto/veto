package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeploymentConfirmationOffLetsDeleteThrough(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	dir := t.TempDir()
	spec := filepath.Join(dir, "orders.yaml")
	require.NoError(t, os.WriteFile(spec, []byte(confirmationSpec), 0o600))
	agent := filepath.Join(dir, "agent.yaml")
	require.NoError(t, os.WriteFile(agent, []byte("operations:\n  - operation: orders.delete\n    confirmation: true\n"), 0o600))
	conf := filepath.Join(dir, "veto.yaml")
	require.NoError(t, os.WriteFile(conf, []byte(fmt.Sprintf("confirmation: false\nagent_file: agent.yaml\ncontracts:\n  - %s\n", spec)), 0o600))

	loop, cfg, err := buildLoop(nil, conf, "", "", ts.URL)
	require.NoError(t, err)
	assert.False(t, cfg.Confirms())
	op := loop.Catalog.ByID("orders.delete")
	require.NotNil(t, op)
	assert.False(t, op.RequiresConfirmation)

	out, err := loop.Invoke(context.Background(), "orders.delete", map[string]string{"id": "10490"}, "")
	require.NoError(t, err)
	assert.Equal(t, "ok", out.Status)
	assert.Equal(t, int32(1), hits.Load())

	lines, fail := doctorReport(context.Background(), loop.Catalog, cfg, nil, false)
	report := strings.Join(lines, "\n")
	assert.False(t, fail)
	assert.Contains(t, report, "confirmation is off")
	assert.NotContains(t, report, "write requires approval")
}

func TestUnsetConfirmationStillStopsADelete(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	dir := t.TempDir()
	spec := filepath.Join(dir, "orders.yaml")
	require.NoError(t, os.WriteFile(spec, []byte(confirmationSpec), 0o600))
	conf := filepath.Join(dir, "veto.yaml")
	require.NoError(t, os.WriteFile(conf, []byte(fmt.Sprintf("contracts:\n  - %s\n", spec)), 0o600))

	loop, cfg, err := buildLoop(nil, conf, "", "", ts.URL)
	require.NoError(t, err)
	assert.True(t, cfg.Confirms())
	require.True(t, loop.Catalog.ByID("orders.delete").RequiresConfirmation)

	out, err := loop.Invoke(context.Background(), "orders.delete", map[string]string{"id": "10490"}, "")
	require.NoError(t, err)
	assert.Equal(t, "confirmation_required", out.Status)
	assert.Equal(t, int32(0), hits.Load())

	lines, fail := doctorReport(context.Background(), loop.Catalog, cfg, nil, false)
	report := strings.Join(lines, "\n")
	assert.False(t, fail)
	assert.Contains(t, report, "orders.delete: write requires approval")
	assert.NotContains(t, report, "confirmation is off")
}

func TestOverlayKeepsDeploymentConfirmation(t *testing.T) {
	off := false
	deploy := config.File{Confirmation: &off, Contracts: []string{"deploy.yaml"}}
	shared := config.File{Contracts: []string{"bundle.yaml"}}
	out := overlayBundle(deploy, shared)
	require.NotNil(t, out.Confirmation)
	assert.False(t, *out.Confirmation)
	assert.Equal(t, []string{"bundle.yaml"}, out.Contracts)
}

const confirmationSpec = `openapi: 3.0.3
info:
  title: Orders
  version: "1"
paths:
  /orders/{id}:
    delete:
      operationId: orders.delete
      summary: Delete one order
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
