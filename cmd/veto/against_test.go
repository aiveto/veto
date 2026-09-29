package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/eval"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckAgainstSnapshot(t *testing.T) {
	cfgPath := filepath.Join("..", "..", "testdata", "veto.yaml")
	cases := filepath.Join("..", "..", "testdata", "cases")
	_, contracts, relations, agentPath, err := resolve(cfgPath, nil, "", "")
	require.NoError(t, err)
	cat, err := loadCatalog(contracts, relations)
	require.NoError(t, err)
	require.NoError(t, applyAgent(cat, agentPath))
	dir := t.TempDir()
	path := filepath.Join(dir, "surface.json")
	body, err := json.Marshal(snapshotFile{Operations: catalog.Facts(cat)})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, body, 0o644))
	cmd := checkCmd{against: path, evalCmd: evalCmd{config: cfgPath, cases: []string{cases}}}
	require.NoError(t, diffAgainst(cmd, cat))
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
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, body, 0o644))
	err = diffAgainst(cmd, cat)
	assert.ErrorContains(t, err, "gone.get")
	assert.ErrorContains(t, err, "delete-requires-confirmation")
}

func TestCheckAgainstGitRef(t *testing.T) {
	dir := t.TempDir()
	caseBody := []byte("name: get-asset\ninput: get asset\nexpect:\n  operation: assets.get\n")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "cases"), 0o755))
	files := map[string]string{
		"openapi.yaml":   headSpec,
		"agent.yaml":     "operations: []\n",
		"veto.yaml":      "contracts:\n  - openapi.yaml\nagent_file: agent.yaml\n",
		"cases/get.yaml": string(caseBody),
	}
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
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
		require.NoError(t, err, string(out))
	}
	git("init")
	git("add", ".")
	git("commit", "-m", "baseline")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "openapi.yaml"), []byte(nextSpec), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cases", "get.yaml"), []byte("name: get-asset\ninput: get asset\nexpect:\n  operation: assets.purge\n"), 0o644))
	cfgPath := filepath.Join(dir, "veto.yaml")
	cases := filepath.Join(dir, "cases")
	cat := loadChecked(t, cfgPath)
	cmd := checkCmd{against: "HEAD", evalCmd: evalCmd{config: cfgPath, cases: []string{cases}}}
	err := diffAgainst(cmd, cat)
	require.Error(t, err)
	assert.ErrorContains(t, err, "teams.get")
	assert.ErrorContains(t, err, "assets.purge")
	assert.ErrorContains(t, err, "get-asset")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte("operations:\n  - operation: assets.purge\n    confirmation: false\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cases", "get.yaml"), caseBody, 0o644))
	cat = loadChecked(t, cfgPath)
	err = diffAgainst(cmd, cat)
	require.Error(t, err)
	assert.ErrorContains(t, err, "teams.get")
	assert.NotContains(t, err.Error(), "assets.purge")
	assert.NotContains(t, err.Error(), "get-asset")
}

func loadChecked(t *testing.T, cfgPath string) *catalog.Catalog {
	t.Helper()
	_, contracts, relations, agentPath, err := resolve(cfgPath, nil, "", "")
	require.NoError(t, err)
	cat, err := loadCatalog(contracts, relations)
	require.NoError(t, err)
	require.NoError(t, applyAgent(cat, agentPath))
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
