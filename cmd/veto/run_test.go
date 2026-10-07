package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskParamsKeepTheValueOffTheError(t *testing.T) {
	_, err := taskParams([]string{"=secret"})
	require.EqualError(t, err, "param needs a name and a value")
	assert.NotContains(t, err.Error(), "secret")

	_, err = taskParams([]string{"id=10482", "id=9"})
	require.EqualError(t, err, "param id is repeated")
	assert.NotContains(t, err.Error(), "10482")
}

func TestRunRefusesAFlowThatIsNotATask(t *testing.T) {
	err := runTask(catalogFlags{config: filepath.Join("..", "..", "testdata", "veto.yaml")}, "list-then-get", nil)
	require.EqualError(t, err, "unknown task list-then-get")
}
