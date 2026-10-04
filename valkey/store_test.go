package valkey

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/policy"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimIsOnceAcrossClients(t *testing.T) {
	srv := miniredis.RunT(t)
	a, err := Dial(t.Context(), "redis://"+srv.Addr())
	require.NoError(t, err)
	t.Cleanup(a.Close)
	b, err := Dial(t.Context(), srv.Addr())
	require.NoError(t, err)
	t.Cleanup(b.Close)

	rec := policy.Record{ID: "pending-1", OperationID: "orders.delete", Status: "pending", Params: map[string]string{"id": "1"}}
	require.NoError(t, a.Put(t.Context(), rec))
	got, ok, err := b.Get(t.Context(), rec.ID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, rec.ID, got.ID)

	rec.Status = "approved"
	rec.ApprovedID = "ok-1"
	require.NoError(t, a.Put(t.Context(), rec))
	found, ok, err := b.FindApproved(t.Context(), "ok-1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, rec.ID, found.ID)

	ok, err = a.Claim(t.Context(), rec.ID)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = b.Claim(t.Context(), rec.ID)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestSharedStateConsumeOnce(t *testing.T) {
	params := map[string]string{"id": "1"}
	for range 16 {
		srv := miniredis.RunT(t)
		store, err := Dial(t.Context(), "redis://"+srv.Addr())
		require.NoError(t, err)
		t.Cleanup(store.Close)
		issued := policy.NewState()
		issued.SetStore(store)
		pending, err := issued.RequestFor(t.Context(), "", "orders.delete", params)
		require.NoError(t, err)
		approved, err := issued.Approve(t.Context(), pending)
		require.NoError(t, err)
		var wins atomic.Int32
		var wg sync.WaitGroup
		for range 16 {
			wg.Go(func() {
				other := policy.NewState()
				other.SetStore(store)
				ok, err := other.ConsumeFor(t.Context(), "", approved, "orders.delete", params)
				if err == nil && ok {
					wins.Add(1)
				}
			})
		}
		wg.Wait()
		assert.Equal(t, int32(1), wins.Load())
	}
}

func TestDialRejectsAnEmptyURL(t *testing.T) {
	_, err := Dial(t.Context(), "  ")
	require.Error(t, err)
}
