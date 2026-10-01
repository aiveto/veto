package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestInitWritesStarterAndRefusesOverwrite(t *testing.T) {
	t.Run("writes contracts and a relations stub", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, writeStarter(dir, []string{"orders.yaml", "customers.yaml"}))
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
	})

	t.Run("refuses to overwrite veto.yaml", func(t *testing.T) {
		dir := t.TempDir()
		kept := []byte("keep: true\n")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "veto.yaml"), kept, 0o600))
		err := writeStarter(dir, []string{"orders.yaml"})
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
		require.NoError(t, writeStarter(dir, []string{"orders.yaml", "customers.yaml"}))
		got, err := os.ReadFile(filepath.Join(dir, "relations.yaml"))
		require.NoError(t, err)
		assert.Equal(t, kept, got)
	})
}
