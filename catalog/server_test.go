package catalog_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiveto/veto/openapi"
)

func TestFirstServerWinsUntilANameIsSelected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.yaml")
	if err := os.WriteFile(path, []byte(twoServers), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := openapi.Load(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	op := cat.ByID("assets.get")
	if op.BaseURL != "http://prod.example" {
		t.Fatalf("first server: %s", op.BaseURL)
	}
	if err := cat.SelectServer(""); err != nil {
		t.Fatal(err)
	}
	if cat.ByID("assets.get").BaseURL != "http://prod.example" {
		t.Fatalf("unset name changed the server: %s", cat.ByID("assets.get").BaseURL)
	}
	if err := cat.SelectServer("staging"); err != nil {
		t.Fatal(err)
	}
	if cat.ByID("assets.get").BaseURL != "http://stage.example" {
		t.Fatalf("named server: %s", cat.ByID("assets.get").BaseURL)
	}
	if err := cat.SelectServer("missing"); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("err: %v", err)
	}
}

const twoServers = `openapi: 3.0.3
info:
  title: Assets
  version: "1"
servers:
  - url: http://prod.example
  - url: http://stage.example
    description: staging
paths:
  /assets/{id}:
    get:
      operationId: assets.get
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
`
