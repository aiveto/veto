package valkeystore

import (
	"testing"

	"github.com/aiveto/veto/policy"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimIsOnceAcrossClients(t *testing.T) {
	srv := miniredis.RunT(t)
	a, err := Dial("redis://" + srv.Addr())
	require.NoError(t, err)
	t.Cleanup(a.Close)
	b, err := Dial(srv.Addr())
	require.NoError(t, err)
	t.Cleanup(b.Close)

	rec := policy.Record{ID: "pending-1", OperationID: "orders.delete", Status: "pending", Params: map[string]string{"id": "1"}}
	require.NoError(t, a.Put(rec))
	got, ok := b.Get(rec.ID)
	require.True(t, ok)
	assert.Equal(t, rec.ID, got.ID)

	rec.Status = "approved"
	rec.ApprovedID = "ok-1"
	require.NoError(t, a.Put(rec))
	found, ok := b.FindApproved("ok-1")
	require.True(t, ok)
	assert.Equal(t, rec.ID, found.ID)

	ok, err = a.Claim(rec.ID)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = b.Claim(rec.ID)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestDialRejectsAnEmptyURL(t *testing.T) {
	_, err := Dial("  ")
	require.Error(t, err)
}
