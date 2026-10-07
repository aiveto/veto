package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildLoopSharesTheKernelRuntime(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "orders.yaml"), []byte(`openapi: 3.0.3
info: {title: Orders, version: "1"}
servers:
  - url: http://127.0.0.1:9
paths:
  /orders/{id}:
    get:
      operationId: orders.get
      parameters:
        - name: id
          in: path
          required: true
          schema: {type: string}
      responses:
        "200":
          description: ok
`), 0o600))
	cfg := filepath.Join(dir, "veto.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte("page: follow\ncontracts:\n  - orders.yaml\n"), 0o600))
	loop, _, err := buildLoop(nil, cfg, "", "", "")
	require.NoError(t, err)
	rt := loop.RuntimePtr()
	require.NotNil(t, rt)
	assert.Equal(t, 5, rt.Pages)
	rt.Pages = 2
	assert.Equal(t, 2, loop.RuntimePtr().Pages)
	assert.Same(t, rt, loop.RuntimePtr())
}
