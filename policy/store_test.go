package policy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilesGetNamesMissingAndCorrupt(t *testing.T) {
	store := &Files{Dir: t.TempDir()}
	_, ok, err := store.Get(t.Context(), "pending-1")
	require.NoError(t, err)
	assert.False(t, ok)

	dir := filepath.Join(store.Dir, "confirmations")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pending-1.json"), []byte("{"), 0o600))
	_, ok, err = store.Get(t.Context(), "pending-1")
	require.Error(t, err)
	assert.False(t, ok)
}

func TestFilesGetReadsAPriorFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "confirmations"), 0o700))
	raw := []byte(`{"id":"pending-1","operation_id":"orders.delete","status":"pending","params":null}` + "\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confirmations", "pending-1.json"), raw, 0o600))
	store := &Files{Dir: dir}
	got, ok, err := store.Get(t.Context(), "pending-1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "orders.delete", got.OperationID)
	assert.Empty(t, got.Params)
}

func TestFilesWriteUsesV2(t *testing.T) {
	store := &Files{Dir: t.TempDir()}
	require.NoError(t, store.Put(t.Context(), Record{ID: "pending-1", OperationID: "orders.delete", Status: StatusPending}))
	raw, err := os.ReadFile(filepath.Join(store.Dir, "confirmations", "pending-1.json"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), `"params":null`)
}
