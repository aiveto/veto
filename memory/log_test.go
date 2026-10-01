package memory_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogKeepsTurnsAcrossProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turns.log")
	log, err := memory.NewLog(path)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, log.Store(ctx, memory.Item{ID: "1", Content: "delete order 1"}))
	require.NoError(t, log.Store(ctx, memory.Item{ID: "2", Content: "list orders"}))
	again, err := memory.NewLog(path)
	require.NoError(t, err)
	recent, err := again.Recent(ctx, 1)
	require.NoError(t, err)
	require.Len(t, recent, 1)
	assert.Equal(t, "list orders", recent[0].Content)
	found, err := again.Search(ctx, "delete order")
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, "1", found[0].ID)
	require.NoError(t, again.Delete(ctx, "1"))
	reloaded, err := memory.NewLog(path)
	require.NoError(t, err)
	found, err = reloaded.Search(ctx, "delete order")
	require.NoError(t, err)
	assert.Empty(t, found)
}

func TestLogCanceledContextWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turns.log")
	log, err := memory.NewLog(path)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = log.Store(ctx, memory.Item{ID: "1", Content: "x"})
	require.ErrorIs(t, err, context.Canceled)
	_, err = os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist)
}
