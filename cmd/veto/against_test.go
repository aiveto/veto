package main

import (
	"encoding/json"
	"fmt"
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
	src, err := resolve(cfgPath, nil, "", "")
	contracts, relations, agentPath := src.contracts, src.relations, src.agent
	require.NoError(t, err)
	cat, err := loadCatalog(contracts, relations)
	require.NoError(t, err)
	require.NoError(t, applyAgent(cat, agentPath))
	dir := t.TempDir()
	path := filepath.Join(dir, "surface.json")
	body, err := json.Marshal(snapshotFile{Operations: catalog.Facts(cat)})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, body, 0o600))
	cmd := checkCmd{against: path, config: cfgPath, cases: []string{cases}}
	require.NoError(t, diffAgainst(cmd, cat, nil))
	facts := catalog.Facts(cat)
	facts["gone.get"] = catalog.OpFact{Referenced: true}
	body, err = json.Marshal(snapshotFile{
		Operations: facts,
		Cases: []eval.CaseExpect{{
			Name:                 "delete-requires-confirmation",
			Operation:            "orders.get",
			ConfirmationRequired: true,
		}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, body, 0o600))
	err = diffAgainst(cmd, cat, nil)
	require.ErrorContains(t, err, "gone.get")
	assert.ErrorContains(t, err, "delete-requires-confirmation")
}

func TestCheckAgainstNamesABrokenTask(t *testing.T) {
	cfgPath := filepath.Join("..", "..", "testdata", "veto.yaml")
	cases := filepath.Join("..", "..", "testdata", "cases")
	src, err := resolve(cfgPath, nil, "", "")
	require.NoError(t, err)
	cat, err := loadCatalog(src.contracts, src.relations)
	require.NoError(t, err)
	dir := t.TempDir()
	path := filepath.Join(dir, "surface.json")
	body, err := json.Marshal(snapshotFile{Operations: catalog.Facts(cat), Tasks: []string{
		"task investigate-charge: Investigate a customer's disputed charge. answer: amount, currency | orders.get invoiceId -> invoices.get id",
	}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, body, 0o600))
	err = diffAgainst(checkCmd{against: path, config: cfgPath, cases: []string{cases}}, cat, []string{
		"task investigate-charge: Investigate a customer's disputed charge. answer: currency",
	})
	require.ErrorContains(t, err, "task investigate-charge can no longer obtain invoiceId from orders.get")
	assert.ErrorContains(t, err, "task investigate-charge can no longer read amount")
}

func TestCheckAgainstDeploymentConfirmationOff(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "orders.yaml")
	require.NoError(t, os.WriteFile(spec, []byte(confirmationSpec), 0o600))
	caseBody := "name: drop\ninput: delete order 10490\nexpect:\n  operation: orders.delete\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "case.yaml"), []byte(caseBody), 0o600))
	offPath := filepath.Join(dir, "off.yaml")
	require.NoError(t, os.WriteFile(offPath, []byte(fmt.Sprintf("confirmation: false\ncontracts:\n  - %s\n", spec)), 0o600))
	onPath := filepath.Join(dir, "on.yaml")
	require.NoError(t, os.WriteFile(onPath, []byte(fmt.Sprintf("contracts:\n  - %s\n", spec)), 0o600))

	loop, _, err := buildLoop(nil, offPath, "", "", "")
	require.NoError(t, err)
	snap := filepath.Join(dir, "surface.json")
	body, err := json.Marshal(snapshotFile{Operations: map[string]catalog.OpFact{
		"orders.delete": {Destructive: true, Confirmation: true},
	}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(snap, body, 0o600))

	err = diffAgainst(checkCmd{against: snap, config: offPath, cases: []string{filepath.Join(dir, "case.yaml")}}, loop.Catalog, nil)
	require.NoError(t, err)

	held, _, err := buildLoop(nil, onPath, "", "", "")
	require.NoError(t, err)
	held.Catalog.ClearConfirmation()
	err = diffAgainst(checkCmd{against: snap, config: onPath, cases: []string{filepath.Join(dir, "case.yaml")}}, held.Catalog, nil)
	require.ErrorContains(t, err, "orders.delete lost confirmation")
}

func TestCheckAgainstGitRef(t *testing.T) {
	dir := t.TempDir()
	caseBody := []byte("name: get-order\ninput: get order\nexpect:\n  operation: orders.get\n")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "cases"), 0o750))
	files := map[string]string{
		"orders.yaml":    headSpec,
		"agent.yaml":     "operations: []\n",
		"veto.yaml":      "contracts:\n  - orders.yaml\nagent_file: agent.yaml\n",
		"cases/get.yaml": string(caseBody),
	}
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	commitBaseline(t, dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "orders.yaml"), []byte(nextSpec), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cases", "get.yaml"), []byte("name: get-order\ninput: get order\nexpect:\n  operation: orders.purge\n"), 0o600))
	cfgPath := filepath.Join(dir, "veto.yaml")
	cases := filepath.Join(dir, "cases")
	cat := loadChecked(t, cfgPath)
	cmd := checkCmd{against: "HEAD", config: cfgPath, cases: []string{cases}}
	err := diffAgainst(cmd, cat, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "customers.get")
	require.ErrorContains(t, err, "orders.purge")
	require.ErrorContains(t, err, "get-order")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte("operations:\n  - operation: orders.purge\n    confirmation: false\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cases", "get.yaml"), caseBody, 0o600))
	cat = loadChecked(t, cfgPath)
	err = diffAgainst(cmd, cat, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "customers.get")
	assert.NotContains(t, err.Error(), "orders.purge")
	assert.NotContains(t, err.Error(), "get-order")
}

func TestCheckAgainstGitRefAppliesEachSideSelection(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "veto.yaml")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "orders.yaml"), []byte(headSpec), 0o600))
	require.NoError(t, os.WriteFile(cfgPath, []byte("read_only: true\ncontracts:\n  - orders.yaml\n"), 0o600))
	commitBaseline(t, dir)

	cmd := checkCmd{against: "HEAD", config: cfgPath}
	require.NoError(t, diffAgainst(cmd, loadDeployed(t, cfgPath), nil))

	require.NoError(t, os.WriteFile(cfgPath, []byte("contracts:\n  - orders.yaml\n"), 0o600))
	require.ErrorContains(t, diffAgainst(cmd, loadDeployed(t, cfgPath), nil), "orders.purge")
}

func commitBaseline(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{{"init"}, {"add", "."}, {"commit", "-m", "baseline"}} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
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
}

func loadDeployed(t *testing.T, cfgPath string) *catalog.Catalog {
	t.Helper()
	cat := loadChecked(t, cfgPath)
	src, err := resolve(cfgPath, nil, "", "")
	require.NoError(t, err)
	applyDeployment(cat, src.cfg)
	return cat
}

func loadChecked(t *testing.T, cfgPath string) *catalog.Catalog {
	t.Helper()
	src, err := resolve(cfgPath, nil, "", "")
	contracts, relations, agentPath := src.contracts, src.relations, src.agent
	require.NoError(t, err)
	cat, err := loadCatalog(contracts, relations)
	require.NoError(t, err)
	require.NoError(t, applyAgent(cat, agentPath))
	return cat
}

const headSpec = `openapi: 3.0.3
info:
  title: Orders
  version: "1"
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
          schema:
            type: string
      responses:
        "200":
          description: One order
          links:
            customer:
              operationId: customers.get
  /customers/{id}:
    get:
      operationId: customers.get
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: One customer
  /purge/{id}:
    delete:
      operationId: orders.purge
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
  title: Orders
  version: "1"
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
          schema:
            type: string
      responses:
        "200":
          description: One order
  /purge/{id}:
    get:
      operationId: orders.purge
      summary: Fetch
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: One order
`
