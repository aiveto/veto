package generate_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiveto/veto/generate"
	"github.com/aiveto/veto/openapi"
)

func TestGeneratedBodyParamReachesInvoke(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.yaml")
	if err := os.WriteFile(spec, []byte(createSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := openapi.Load(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "gen")
	const module = "example.com/assetgen"
	if err := generate.Write(out, module, cat); err != nil {
		t.Fatal(err)
	}
	sdk, err := os.ReadFile(filepath.Join(out, "sdk", "client.go"))
	if err != nil {
		t.Fatal(err)
	}
	cli, err := os.ReadFile(filepath.Join(out, "cli", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := os.ReadFile(filepath.Join(out, "dispatch", "call.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{string(sdk), string(cli), string(dispatch)} {
		if strings.Contains(src, "_body") {
			t.Fatalf("magic body key:\n%s", src)
		}
	}
	if !strings.Contains(string(sdk), "Loop.Invoke") || !strings.Contains(string(sdk), `"body": body`) {
		t.Fatalf("sdk:\n%s", sdk)
	}
	if !strings.Contains(string(dispatch), `params["body"]`) {
		t.Fatalf("dispatch:\n%s", dispatch)
	}

	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	replace := exec.Command("go", "mod", "edit", "-replace", "github.com/aiveto/veto="+root)
	replace.Dir = out
	if msg, err := replace.CombinedOutput(); err != nil {
		t.Fatalf("replace: %v\n%s", err, msg)
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = out
	if msg, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("tidy: %v\n%s", err, msg)
	}
	help := exec.Command("go", "run", "./cli", "create", "--help-json")
	help.Dir = out
	msg, err := help.CombinedOutput()
	if err != nil {
		t.Fatalf("help-json: %v\n%s", err, msg)
	}
	var doc struct {
		Params []struct {
			Name     string `json:"name"`
			In       string `json:"in"`
			Required bool   `json:"required"`
		} `json:"params"`
	}
	if err := json.Unmarshal(msg, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Params) != 1 || doc.Params[0].Name != "body" || doc.Params[0].In != "body" || !doc.Params[0].Required {
		t.Fatalf("params: %+v", doc.Params)
	}
}

const createSpec = `openapi: 3.0.3
info:
  title: Assets
  version: "1"
servers:
  - url: http://127.0.0.1:8080
paths:
  /assets:
    post:
      operationId: assets.create
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
