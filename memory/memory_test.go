package memory_test

import (
	"context"
	"testing"

	"github.com/aiveto/veto/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteRemovesIDFromOrder(t *testing.T) {
	ctx := context.Background()
	m := memory.NewLocalMap()
	for _, id := range []string{"a", "b"} {
		require.NoError(t, m.Store(ctx, memory.Item{ID: id, Content: id}))
	}
	require.NoError(t, m.Delete(ctx, "a"))
	require.NoError(t, m.Store(ctx, memory.Item{ID: "a", Content: "a"}))
	recent, err := m.Recent(ctx, 10)
	require.NoError(t, err)
	require.Len(t, recent, 2)
	assert.Equal(t, "b", recent[0].ID)
	assert.Equal(t, "a", recent[1].ID)
}

func TestCanceledContextDoesNoWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := memory.NewLocalMap()
	cases := []struct {
		name string
		call func() error
	}{
		{name: "store", call: func() error { return m.Store(ctx, memory.Item{ID: "a", Content: "a"}) }},
		{name: "search", call: func() error { _, err := m.Search(ctx, "a"); return err }},
		{name: "recent", call: func() error { _, err := m.Recent(ctx, 1); return err }},
		{name: "delete", call: func() error { return m.Delete(ctx, "a") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorIs(t, tc.call(), context.Canceled)
		})
	}
	recent, err := m.Recent(context.Background(), 10)
	require.NoError(t, err)
	assert.Empty(t, recent)
}
