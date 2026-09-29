package generate_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/generate"
	"github.com/aiveto/veto/openapi"
)

func TestGeneratedMethodsKeepEachServerURL(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{ID: "assets.get", Method: "GET", PathTemplate: "/assets/{id}", BaseURL: "http://assets.example", Params: []catalog.Param{{Name: "id", In: "path", Required: true}}},
		{ID: "teams.get", Method: "GET", PathTemplate: "/teams/{id}", BaseURL: "http://teams.example", Params: []catalog.Param{{Name: "id", In: "path", Required: true}}},
	}}
	cat.Finalize()
	files, err := generate.Render("example.com/both", cat)
	if err != nil {
		t.Fatal(err)
	}
	sdk := string(files.SDK)
	cli := string(files.CLI)
	if !strings.Contains(sdk, "http://assets.example") || !strings.Contains(sdk, "http://teams.example") {
		t.Fatalf("sdk dropped a server:\n%s", sdk)
	}
	if strings.Contains(cli, "127.0.0.1:8080") || !strings.Contains(cli, "VETO_BASE_URL") {
		t.Fatalf("cli override:\n%s", cli)
	}
	if !strings.Contains(sdk, "execute.Client{BaseURL: baseURL") {
		t.Fatalf("override is not the client base URL:\n%s", sdk)
	}
}

func TestGeneratedCLIHelpAndConfirm(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	const module = "example.com/assetgen"
	if err := generate.Write(dir, module, cat); err != nil {
		t.Fatal(err)
	}
	sdk, err := os.ReadFile(filepath.Join(dir, "sdk", "client.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sdk), "Loop.Invoke") || strings.Contains(string(sdk), "execute.Invoke") || strings.Contains(string(sdk), "http.NewRequest") {
		t.Fatal("sdk must call agent.Invoke and must not build its own request")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	replace := exec.Command("go", "mod", "edit", "-replace", "github.com/aiveto/veto="+root)
	replace.Dir = dir
	if out, err := replace.CombinedOutput(); err != nil {
		t.Fatalf("replace: %v\n%s", err, out)
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("tidy: %v\n%s", err, out)
	}

	compile := exec.Command("go", "test", "./...")
	compile.Dir = dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("generated module: %v\n%s", err, out)
	}

	help := exec.Command("go", "run", "./cli", "delete", "--help-json")
	help.Dir = dir
	out, err := help.CombinedOutput()
	if err != nil {
		t.Fatalf("help-json: %v\n%s", err, out)
	}
	var doc struct {
		Command      string   `json:"command"`
		OperationID  string   `json:"operation"`
		Confirmation bool     `json:"confirmation"`
		Method       string   `json:"method"`
		Server       string   `json:"server"`
		Permissions  []string `json:"permissions"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Command != "delete" || doc.OperationID != "assets.delete" || !doc.Confirmation || doc.Method != "DELETE" || doc.Server != "http://127.0.0.1:8080" {
		t.Fatalf("help doc: %+v", doc)
	}

	deny := exec.Command("go", "run", "./cli", "delete", "--id", "123")
	deny.Dir = dir
	out, err = deny.CombinedOutput()
	if err == nil {
		t.Fatal("delete without --confirm should fail")
	}
	if !strings.Contains(string(out), "confirmation required") {
		t.Fatalf("stderr: %s", out)
	}
}
