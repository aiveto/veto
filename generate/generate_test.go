package generate_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/generate"
	"github.com/aiveto/veto/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedMethodsKeepEachServerURL(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{ID: "orders.get", Method: "GET", PathTemplate: "/orders/{id}", BaseURL: "http://orders.example", Params: []catalog.Param{{Name: "id", In: "path", Required: true}}},
		{ID: "customers.get", Method: "GET", PathTemplate: "/customers/{id}", BaseURL: "http://customers.example", Params: []catalog.Param{{Name: "id", In: "path", Required: true}}},
	}}
	cat.Finalize()
	files, err := generate.Render("example.com/both", cat)
	require.NoError(t, err)
	sdk := string(files.SDK)
	cli := string(files.CLI)
	assert.Contains(t, sdk, "http://orders.example")
	assert.Contains(t, sdk, "http://customers.example")
	assert.NotContains(t, cli, "127.0.0.1:8080")
	assert.Contains(t, cli, "VETO_BASE_URL")
	assert.Contains(t, sdk, "execute.Client{BaseURL: baseURL")
	assert.Contains(t, sdk, "Go client")
	assert.NotContains(t, sdk, "typed SDK")
	assert.NotContains(t, sdk, "typed surface")
	assert.Contains(t, string(files.MCP), "Go client")
	assert.NotContains(t, string(files.MCP), "typed SDK")
}

func TestGeneratedCLIHelpAndConfirm(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	dir := t.TempDir()
	const module = "example.com/ordergen"
	require.NoError(t, generate.Write(dir, module, cat))
	sdk, err := os.ReadFile(filepath.Join(dir, "sdk", "client.go"))
	require.NoError(t, err)
	assert.Contains(t, string(sdk), "Calls.Invoke")
	assert.NotContains(t, string(sdk), "execute.Invoke")
	assert.NotContains(t, string(sdk), "http.NewRequest")
	assert.NotContains(t, string(sdk), "return agent.Call{}")
	root, err := filepath.Abs("..")
	require.NoError(t, err)
	replace := exec.CommandContext(t.Context(), "go", "mod", "edit", "-replace", "github.com/aiveto/veto="+root)
	replace.Dir = dir
	out, err := replace.CombinedOutput()
	require.NoError(t, err, string(out))
	tidy := exec.CommandContext(t.Context(), "go", "mod", "tidy")
	tidy.Dir = dir
	out, err = tidy.CombinedOutput()
	require.NoError(t, err, string(out))

	compile := exec.CommandContext(t.Context(), "go", "test", "./...")
	compile.Dir = dir
	out, err = compile.CombinedOutput()
	require.NoError(t, err, string(out))

	help := exec.CommandContext(t.Context(), "go", "run", "./cli", "delete", "--help-json")
	help.Dir = dir
	out, err = help.CombinedOutput()
	require.NoError(t, err, string(out))
	var doc struct {
		Command      string   `json:"command"`
		OperationID  string   `json:"operation"`
		Confirmation bool     `json:"confirmation"`
		Method       string   `json:"method"`
		Server       string   `json:"server"`
		Permissions  []string `json:"permissions"`
	}
	require.NoError(t, json.Unmarshal(out, &doc))
	assert.Equal(t, "delete", doc.Command)
	assert.Equal(t, "orders.delete", doc.OperationID)
	assert.True(t, doc.Confirmation)
	assert.Equal(t, "DELETE", doc.Method)
	assert.Equal(t, "http://127.0.0.1:8080", doc.Server)

	deny := exec.CommandContext(t.Context(), "go", "run", "./cli", "delete", "--id", "123")
	deny.Dir = dir
	out, err = deny.CombinedOutput()
	assert.Error(t, err)
	assert.Contains(t, string(out), "confirmation required")
}
