package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/eval"
)

func TestCheckAgainstFlag(t *testing.T) {
	if newCheckCommand().Flags().Lookup("against") == nil {
		t.Fatal("missing --against")
	}
}

func TestCheckAgainstSnapshot(t *testing.T) {
	cfgPath := filepath.Join("..", "..", "testdata", "veto.yaml")
	cases := filepath.Join("..", "..", "testdata", "cases")
	_, contracts, relations, agentPath, err := resolve(cfgPath, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := loadCatalog(contracts, relations)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyAgent(cat, agentPath); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "surface.json")
	body, err := json.Marshal(snapshotFile{Operations: catalog.Facts(cat)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := checkCmd{against: path, evalCmd: evalCmd{config: cfgPath, cases: []string{cases}}}
	if err := diffAgainst(cmd, cat); err != nil {
		t.Fatal(err)
	}
	facts := catalog.Facts(cat)
	facts["gone.get"] = catalog.OpFact{Referenced: true}
	body, err = json.Marshal(snapshotFile{
		Operations: facts,
		Cases: []eval.CaseExpect{{
			Name:                 "delete-requires-confirmation",
			Operation:            "assets.get",
			ConfirmationRequired: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	err = diffAgainst(cmd, cat)
	if err == nil || !strings.Contains(err.Error(), "gone.get") || !strings.Contains(err.Error(), "delete-requires-confirmation") {
		t.Fatalf("snapshot: %v", err)
	}
}

func TestCheckAgainstGitRef(t *testing.T) {
	dir := t.TempDir()
	caseBody := []byte("name: get-asset\ninput: get asset\nexpect:\n  operation: assets.get\n")
	if err := os.Mkdir(filepath.Join(dir, "cases"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"openapi.yaml":   headSpec,
		"agent.yaml":     "operations: []\n",
		"veto.yaml":      "contracts:\n  - openapi.yaml\nagent_file: agent.yaml\n",
		"cases/get.yaml": string(caseBody),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=veto",
			"GIT_AUTHOR_EMAIL=veto@example.com",
			"GIT_COMMITTER_NAME=veto",
			"GIT_COMMITTER_EMAIL=veto@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	git("init")
	git("add", ".")
	git("commit", "-m", "baseline")

	if err := os.WriteFile(filepath.Join(dir, "openapi.yaml"), []byte(nextSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cases", "get.yaml"), []byte("name: get-asset\ninput: get asset\nexpect:\n  operation: assets.purge\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "veto.yaml")
	cases := filepath.Join(dir, "cases")
	cat := loadChecked(t, cfgPath)
	cmd := checkCmd{against: "HEAD", evalCmd: evalCmd{config: cfgPath, cases: []string{cases}}}
	err := diffAgainst(cmd, cat)
	if err == nil {
		t.Fatal("expected regressions")
	}
	for _, needle := range []string{"teams.get", "assets.purge", "get-asset"} {
		if !strings.Contains(err.Error(), needle) {
			t.Fatalf("missing %s in %v", needle, err)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte("operations:\n  - operation: assets.purge\n    confirmation: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cases", "get.yaml"), caseBody, 0o644); err != nil {
		t.Fatal(err)
	}
	cat = loadChecked(t, cfgPath)
	err = diffAgainst(cmd, cat)
	if err == nil || !strings.Contains(err.Error(), "teams.get") || strings.Contains(err.Error(), "assets.purge") || strings.Contains(err.Error(), "get-asset") {
		t.Fatalf("after an intentional confirmation change: %v", err)
	}
}

func loadChecked(t *testing.T, cfgPath string) *catalog.Catalog {
	t.Helper()
	_, contracts, relations, agentPath, err := resolve(cfgPath, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := loadCatalog(contracts, relations)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyAgent(cat, agentPath); err != nil {
		t.Fatal(err)
	}
	return cat
}

const headSpec = `openapi: 3.0.3
info:
  title: Assets
  version: "1"
servers:
  - url: http://127.0.0.1:9
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
          description: One asset
          links:
            team:
              operationId: teams.get
  /teams/{id}:
    get:
      operationId: teams.get
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: One team
  /purge/{id}:
    delete:
      operationId: assets.purge
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "204":
          description: Gone
`

const nextSpec = `openapi: 3.0.3
info:
  title: Assets
  version: "1"
servers:
  - url: http://127.0.0.1:9
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
          description: One asset
  /purge/{id}:
    get:
      operationId: assets.purge
      summary: Fetch
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: One asset
`
