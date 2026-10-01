package memory_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiveto/veto/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogRejectsAnOversizedItemWithoutTruncating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turns.log")
	log, err := memory.NewLog(path)
	require.NoError(t, err)
	require.NoError(t, log.Store(t.Context(), memory.Item{ID: "1", Content: "kept"}))
	err = log.Store(t.Context(), memory.Item{ID: "2", Content: strings.Repeat("x", 1<<20+100)})
	require.Error(t, err)
	again, err := memory.NewLog(path)
	require.NoError(t, err)
	recent, err := again.Recent(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, recent, 1)
	assert.Equal(t, "kept", recent[0].Content)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "kept")
	assert.NotContains(t, string(raw), strings.Repeat("x", 100))
}
