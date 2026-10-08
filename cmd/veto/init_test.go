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

func TestInitWritesStarterAndRefusesOverwrite(t *testing.T) {
	t.Run("writes contracts and a relations stub", func(t *testing.T) {
		dir := t.TempDir()
		notes, err := writeStarter(dir, []string{"orders.yaml", "customers.yaml"})
		require.NoError(t, err)
		assert.Empty(t, notes)
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
		kept := []byte("relations:\n  - schema: Order\n    field: customerId\n    to: customers.get\n")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "relations.yaml"), kept, 0o600))
		_, err := writeStarter(dir, []string{"orders.yaml", "customers.yaml"})
		require.NoError(t, err)
		got, err := os.ReadFile(filepath.Join(dir, "relations.yaml"))
		require.NoError(t, err)
		assert.Equal(t, kept, got)
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
	assert.Contains(t, notes, "preview orders.get: ok")
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
