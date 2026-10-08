package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiveto/veto/agentmeta"
	"github.com/aiveto/veto/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestInitNamesTheOpenAPIFile(t *testing.T) {
	require.EqualError(t, initArgs(nil, nil), "OpenAPI file required, for example: veto init orders.yaml")
	require.NoError(t, initArgs(nil, []string{"orders.yaml"}))
}

func TestInitWritesStarterAndRefusesOverwrite(t *testing.T) {
	t.Run("writes contracts and a relations stub", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "orders.yaml"), []byte(initBareSpec("orders.create")), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "customers.yaml"), []byte(initBareSpec("customers.create")), 0o600))
		notes, err := writeStarter(dir, []string{"orders.yaml", "customers.yaml"})
		require.NoError(t, err)
		assert.Contains(t, notes, "no relations. A response field is not a call until relations.yaml names the operation.")
		raw, err := os.ReadFile(filepath.Join(dir, "veto.yaml"))
		require.NoError(t, err)
		var doc struct {
			Contracts     []string `yaml:"contracts"`
			RelationsFile string   `yaml:"relations_file"`
		}
		require.NoError(t, yaml.Unmarshal(raw, &doc))
		assert.Equal(t, []string{"orders.yaml", "customers.yaml"}, doc.Contracts)
		assert.Equal(t, "relations.yaml", doc.RelationsFile)
		rel, err := os.ReadFile(filepath.Join(dir, "relations.yaml"))
		require.NoError(t, err)
		assert.Equal(t, "relations: []\n", string(rel))
		_, statErr := os.Stat(filepath.Join(dir, "flow.yaml"))
		assert.True(t, os.IsNotExist(statErr))
		_, agentErr := os.Stat(filepath.Join(dir, "agent.yaml"))
		assert.True(t, os.IsNotExist(agentErr))
	})

	t.Run("refuses to overwrite veto.yaml", func(t *testing.T) {
		dir := t.TempDir()
		kept := []byte("keep: true\n")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "veto.yaml"), kept, 0o600))
		_, err := writeStarter(dir, []string{"orders.yaml"})
		require.ErrorContains(t, err, "veto.yaml already exists")
		got, readErr := os.ReadFile(filepath.Join(dir, "veto.yaml"))
		require.NoError(t, readErr)
		assert.Equal(t, kept, got)
		_, statErr := os.Stat(filepath.Join(dir, "relations.yaml"))
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("leaves an existing relations file", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "orders.yaml"), []byte(initBareSpec("orders.create")), 0o600))
		kept := []byte("relations: []\n")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "relations.yaml"), kept, 0o600))
		_, err := writeStarter(dir, []string{"orders.yaml"})
		require.NoError(t, err)
		got, err := os.ReadFile(filepath.Join(dir, "relations.yaml"))
		require.NoError(t, err)
		assert.Equal(t, kept, got)
	})

	t.Run("a missing file writes nothing", func(t *testing.T) {
		dir := t.TempDir()
		_, err := writeStarter(dir, []string{"nope.yaml"})
		require.ErrorContains(t, err, "nope.yaml")
		_, statErr := os.Stat(filepath.Join(dir, "veto.yaml"))
		assert.True(t, os.IsNotExist(statErr))
		_, statErr = os.Stat(filepath.Join(dir, "relations.yaml"))
		assert.True(t, os.IsNotExist(statErr))
	})
}

func TestInitWritesAReadOnlyTask(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "orders.yaml"), []byte(initOrderSpec), 0o600))
	notes, err := writeStarter(dir, []string{"orders.yaml"})
	require.NoError(t, err)
	assert.Contains(t, notes, "preview orders.get: operation orders.get: id required")
	raw, err := os.ReadFile(filepath.Join(dir, "veto.yaml"))
	require.NoError(t, err)
	var doc struct {
		FlowFile  string `yaml:"flow_file"`
		AgentFile string `yaml:"agent_file"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &doc))
	assert.Equal(t, "flow.yaml", doc.FlowFile)
	assert.Equal(t, "agent.yaml", doc.AgentFile)
	flowRaw, err := os.ReadFile(filepath.Join(dir, "flow.yaml"))
	require.NoError(t, err)
	def, err := flow.Parse(flowRaw)
	require.NoError(t, err)
	assert.Equal(t, "read-orders-get", def.Name)
	assert.Equal(t, "Get order by id", def.Question)
	assert.Equal(t, []string{"customerId"}, def.Answer)
	assert.Equal(t, []flow.Step{{Operation: "orders.get"}}, def.Steps)
	agentFile, err := agentmeta.Load(filepath.Join(dir, "agent.yaml"))
	require.NoError(t, err)
	require.Len(t, agentFile.Operations, 1)
	assert.Equal(t, "orders.get", agentFile.Operations[0].Operation)
	require.NotNil(t, agentFile.Operations[0].Exposure)
	assert.Equal(t, "direct", *agentFile.Operations[0].Exposure)
	agentRaw, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(agentRaw), "null")
	assert.NotContains(t, string(agentRaw), "permissions")
	assert.Contains(t, notes, "no relations. A response field is not a call until relations.yaml names the operation.")
	assert.Contains(t, notes, "question: Get order by id")
	assert.Contains(t, notes, "veto run read-orders-get --param id=")
	assert.Contains(t, notes, "veto invoke orders.get --param id=")
}

func TestInitPrefersARunnableReadFromTheFirstFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "orders.yaml"), []byte(initListSpec("orders.list", "/orders", "List orders on the desk")), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "customers.yaml"), []byte(initListSpec("customers.list", "/customers", "List customers")), 0o600))
	notes, err := writeStarter(dir, []string{"orders.yaml", "customers.yaml"})
	require.NoError(t, err)
	flowRaw, err := os.ReadFile(filepath.Join(dir, "flow.yaml"))
	require.NoError(t, err)
	def, err := flow.Parse(flowRaw)
	require.NoError(t, err)
	assert.Equal(t, "orders.list", def.Steps[0].Operation)
	assert.Equal(t, "List orders on the desk", def.Question)
	assert.Contains(t, notes, "veto run read-orders-list")
	assert.Contains(t, notes, "veto invoke orders.list")
	assert.NotContains(t, notes, "--param")
}

func TestInitNamesAnUnsetCredentialAndDoesNotCall(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("init called upstream without a credential")
	}))
	t.Cleanup(srv.Close)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "orders.yaml"), []byte(initAuthSpec(srv.URL)), 0o600))
	t.Setenv("BEARER_AUTH", "")
	notes, err := writeStarter(dir, []string{"orders.yaml"})
	require.NoError(t, err)
	assert.Contains(t, notes, "BEARER_AUTH is unset")
	assert.Contains(t, notes, "Export BEARER_AUTH to the token bearerAuth sends.")
	assert.Contains(t, notes, "preview orders.get: ok")
	assert.Contains(t, notes, "veto run read-orders-get")
	assert.NotContains(t, notes, "--param")
	assert.NotContains(t, notes, "orders.get: ok")
	raw, err := os.ReadFile(filepath.Join(dir, "veto.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "bearerAuth: BEARER_AUTH")
	assert.NotContains(t, string(raw), "s3cret")
}

func TestInitRunsTheReadWhenTheCredentialIsSet(t *testing.T) {
	dir := t.TempDir()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"customerId":"cus_secret"}`); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "orders.yaml"), []byte(initAuthSpec(srv.URL)), 0o600))
	t.Setenv("BEARER_AUTH", "s3cret")
	notes, err := writeStarter(dir, []string{"orders.yaml"})
	require.NoError(t, err)
	joined := strings.Join(notes, "\n")
	assert.Contains(t, notes, "preview orders.get: ok")
	assert.Contains(t, notes, "orders.get: ok")
	assert.NotContains(t, notes, "Export BEARER_AUTH")
	assert.NotContains(t, joined, "cus_secret")
	assert.NotContains(t, joined, "s3cret")
	assert.Equal(t, "Bearer s3cret", gotAuth)
	raw, err := os.ReadFile(filepath.Join(dir, "veto.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "s3cret")
}

func initAuthSpec(server string) string {
	return fmt.Sprintf(`
openapi: 3.0.3
info: {title: Orders, version: "1"}
servers:
  - url: %s
security:
  - bearerAuth: []
paths:
  /orders:
    get:
      operationId: orders.get
      summary: Get order by id
      responses:
        "200":
          description: One order
          content:
            application/json:
              schema:
                type: object
                properties:
                  customerId: {type: string}
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
`, server)
}

func TestResolveDoesNotSearchChildDirectories(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "project")
	require.NoError(t, os.Mkdir(project, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(project, "veto.yaml"), []byte("contracts: [orders.yaml]\n"), 0o600))
	t.Chdir(dir)
	src, err := resolve("", nil, "", "")
	require.NoError(t, err)
	assert.Empty(t, src.contracts)
}

func TestAuthLoginNamesTheEnvVar(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "veto.yaml"), []byte("auth:\n  bearerAuth: BEARER_AUTH\ncontracts: [orders.yaml]\n"), 0o600))
	_, _, err := configuredScheme(filepath.Join(dir, "veto.yaml"), "bearerAuth")
	require.ErrorContains(t, err, "BEARER_AUTH")
	require.ErrorContains(t, err, "auth set --scheme bearerAuth")
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o700))
	t.Chdir(sub)
	_, _, err = configuredScheme("", "bearerAuth")
	require.ErrorContains(t, err, "BEARER_AUTH")
}

func TestResolveFindsVetoYamlInAParentDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "veto.yaml"), []byte("contracts: [orders.yaml]\n"), 0o600))
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o700))
	t.Chdir(sub)
	src, err := resolve("", nil, "", "")
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, "orders.yaml")}, src.contracts)
}

func TestRunUsesVetoYamlInTheCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "orders.yaml"), []byte(initOrderSpec), 0o600))
	_, err := writeStarter(dir, []string{"orders.yaml"})
	require.NoError(t, err)
	t.Chdir(dir)
	err = runTask(catalogFlags{}, "read-orders-get", nil, "", "")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "contract required")
	assert.Contains(t, err.Error(), "id required")
}

func initBareSpec(id string) string {
	return fmt.Sprintf(`
openapi: 3.0.3
info: {title: API, version: "1"}
paths:
  /items:
    post:
      operationId: %s
      responses:
        "204": {description: created}
`, id)
}

func initListSpec(id, path, summary string) string {
	return fmt.Sprintf(`
openapi: 3.0.3
info: {title: API, version: "1"}
paths:
  %s:
    get:
      operationId: %s
      summary: %s
      responses:
        "200":
          description: A list
          content:
            application/json:
              schema:
                type: object
                properties:
                  data: {type: string}
`, path, id, summary)
}

const initOrderSpec = `
openapi: 3.0.3
info: {title: Orders, version: "1"}
paths:
  /orders/{id}:
    get:
      operationId: orders.get
      summary: Get order by id
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      responses:
        "200":
          description: One order
          content:
            application/json:
              schema:
                type: object
                properties:
                  customerId: {type: string}
                  id: {type: string}
`
