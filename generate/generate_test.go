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
		{ID: "assets.get", Method: "GET", PathTemplate: "/assets/{id}", BaseURL: "http://assets.example", Params: []catalog.Param{{Name: "id", In: "path", Required: true}}},
		{ID: "teams.get", Method: "GET", PathTemplate: "/teams/{id}", BaseURL: "http://teams.example", Params: []catalog.Param{{Name: "id", In: "path", Required: true}}},
	}}
	cat.Finalize()
	files, err := generate.Render("example.com/both", cat)
	require.NoError(t, err)
	sdk := string(files.SDK)
	cli := string(files.CLI)
	assert.Contains(t, sdk, "http://assets.example")
	assert.Contains(t, sdk, "http://teams.example")
	assert.NotContains(t, cli, "127.0.0.1:8080")
	assert.Contains(t, cli, "VETO_BASE_URL")
	assert.Contains(t, sdk, "execute.Client{BaseURL: baseURL")
}

func TestGeneratedCLIHelpAndConfirm(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	require.NoError(t, err)
	dir := t.TempDir()
	const module = "example.com/assetgen"
	require.NoError(t, generate.Write(dir, module, cat))
	sdk, err := os.ReadFile(filepath.Join(dir, "sdk", "client.go"))
	require.NoError(t, err)
	assert.Contains(t, string(sdk), "Loop.Invoke")
	assert.NotContains(t, string(sdk), "execute.Invoke")
	assert.NotContains(t, string(sdk), "http.NewRequest")
	assert.NotContains(t, string(sdk), "return agent.Call{}")
	root, err := filepath.Abs("..")
	require.NoError(t, err)
	replace := exec.Command("go", "mod", "edit", "-replace", "github.com/aiveto/veto="+root)
	replace.Dir = dir
	out, err := replace.CombinedOutput()
	require.NoError(t, err, string(out))
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	out, err = tidy.CombinedOutput()
	require.NoError(t, err, string(out))

	compile := exec.Command("go", "test", "./...")
	compile.Dir = dir
	out, err = compile.CombinedOutput()
	require.NoError(t, err, string(out))

	help := exec.Command("go", "run", "./cli", "delete", "--help-json")
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
	assert.Equal(t, "assets.delete", doc.OperationID)
	assert.True(t, doc.Confirmation)
	assert.Equal(t, "DELETE", doc.Method)
	assert.Equal(t, "http://127.0.0.1:8080", doc.Server)

	deny := exec.Command("go", "run", "./cli", "delete", "--id", "123")
	deny.Dir = dir
	out, err = deny.CombinedOutput()
	assert.Error(t, err)
	assert.Contains(t, string(out), "confirmation required")
}
