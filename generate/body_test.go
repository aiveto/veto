package generate_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/generate"
	"github.com/aiveto/veto/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedBodyParamReachesInvoke(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(spec, []byte(createSpec), 0o644))
	cat, err := openapi.Load(context.Background(), spec)
	require.NoError(t, err)
	out := filepath.Join(dir, "gen")
	const module = "example.com/ordergen"
	require.NoError(t, generate.Write(out, module, cat))
	sdk, err := os.ReadFile(filepath.Join(out, "sdk", "client.go"))
	require.NoError(t, err)
	cli, err := os.ReadFile(filepath.Join(out, "cli", "main.go"))
	require.NoError(t, err)
	dispatch, err := os.ReadFile(filepath.Join(out, "dispatch", "call.go"))
	require.NoError(t, err)
	for _, src := range []string{string(sdk), string(cli), string(dispatch)} {
		assert.NotContains(t, src, "_body")
	}
	assert.Contains(t, string(sdk), "Calls.Invoke")
	assert.Contains(t, string(sdk), `"body": body`)
	assert.Contains(t, string(dispatch), `params["body"]`)

	root, err := filepath.Abs("..")
	require.NoError(t, err)
	replace := exec.CommandContext(t.Context(), "go", "mod", "edit", "-replace", "github.com/aiveto/veto="+root)
	replace.Dir = out
	msg, err := replace.CombinedOutput()
	require.NoError(t, err, string(msg))
	tidy := exec.CommandContext(t.Context(), "go", "mod", "tidy")
	tidy.Dir = out
	msg, err = tidy.CombinedOutput()
	require.NoError(t, err, string(msg))
	help := exec.CommandContext(t.Context(), "go", "run", "./cli", "create", "--help-json")
	help.Dir = out
	msg, err = help.CombinedOutput()
	require.NoError(t, err, string(msg))
	var doc struct {
		Params []struct {
			Name     string `json:"name"`
			In       string `json:"in"`
			Required bool   `json:"required"`
		} `json:"params"`
	}
	require.NoError(t, json.Unmarshal(msg, &doc))
	require.Len(t, doc.Params, 1)
	assert.Equal(t, "body", doc.Params[0].Name)
	assert.Equal(t, "body", doc.Params[0].In)
	assert.True(t, doc.Params[0].Required)
}

const createSpec = `openapi: 3.0.3
info:
  title: Orders
  version: "1"
servers:
  - url: http://127.0.0.1:8080
paths:
  /orders:
    post:
      operationId: orders.create
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
      responses:
        "201":
          description: created
`
