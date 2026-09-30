package policy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfirmationKeepsItsOwnParams(t *testing.T) {
	s := NewState()
	params := map[string]string{"id": "1"}
	id := s.RequestConfirmation("orders.delete", params)
	params["id"] = "changed"
	got := s.Pending(id)
	require.NotNil(t, got)
	assert.Equal(t, "1", got.Params["id"])
	assert.True(t, consumed(t, s, id, "orders.delete", map[string]string{"id": "1"}))
	assert.False(t, consumed(t, s, id, "orders.delete", map[string]string{"id": "1"}))
}

func TestSignedApprovalIsNotStored(t *testing.T) {
	params := map[string]string{"id": "1"}
	dir := t.TempDir()
	now := func() time.Time { return time.Unix(1_000, 0) }
	issued := withSigner(t, dir, []byte("secret"), now)
	token := issued.RequestConfirmation("orders.delete", params)
	assert.Nil(t, issued.Pending(token))
	params["id"] = "2"
	other := withSigner(t, dir, []byte("secret"), now)
	assert.False(t, consumed(t, other, token, "orders.delete", params))
	assert.True(t, consumed(t, other, token, "orders.delete", map[string]string{"id": "1"}))
	assert.False(t, consumed(t, other, token, "orders.delete", map[string]string{"id": "1"}))
	restarted := withSigner(t, dir, []byte("secret"), now)
	assert.False(t, consumed(t, restarted, token, "orders.delete", map[string]string{"id": "1"}))
	other.now = func() time.Time { return time.Unix(1_000, 0).Add(time.Minute) }
	fresh := issued.RequestConfirmation("orders.delete", map[string]string{"id": "1"})
	assert.False(t, consumed(t, other, fresh, "orders.delete", map[string]string{"id": "1"}))
	wrong := withSigner(t, dir, []byte("other"), now)
	assert.False(t, consumed(t, wrong, token, "orders.delete", map[string]string{"id": "1"}))
}

func TestDefaultNonceDirFollowsTheSecret(t *testing.T) {
	a, err := defaultNonceDir([]byte("secret"))
	require.NoError(t, err)
	b, err := defaultNonceDir([]byte("secret"))
	require.NoError(t, err)
	c, err := defaultNonceDir([]byte("other"))
	require.NoError(t, err)
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, c)
	assert.Contains(t, a, "approval-nonces")
}

func withSigner(t *testing.T, dir string, secret []byte, now func() time.Time) *State {
	t.Helper()
	s := NewState()
	s.SetNonceDir(dir)
	require.NoError(t, s.SetSigner(secret, time.Minute))
	if now != nil {
		s.now = now
	}
	return s
}

func consumed(t *testing.T, s *State, id, op string, params map[string]string) bool {
	t.Helper()
	ok, err := s.ConsumeConfirmation(id, op, params)
	require.NoError(t, err)
	return ok
}
