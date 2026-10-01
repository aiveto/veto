package policy

import (
	"strings"
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
	assert.False(t, consumed(t, s, id, "orders.delete", map[string]string{"id": "1"}))
	approved, err := s.Approve(id)
	require.NoError(t, err)
	assert.NotEqual(t, id, approved)
	assert.Nil(t, s.Pending(approved))
	assert.False(t, consumed(t, s, id, "orders.delete", map[string]string{"id": "1"}))
	assert.True(t, consumed(t, s, approved, "orders.delete", map[string]string{"id": "1"}))
	assert.False(t, consumed(t, s, approved, "orders.delete", map[string]string{"id": "1"}))
}

func TestSignedApprovalIsIssuedByApprove(t *testing.T) {
	params := map[string]string{"id": "1"}
	dir := t.TempDir()
	now := func() time.Time { return time.Unix(1_000, 0) }
	issued := withSigner(t, dir, []byte("secret"), now)
	pending := issued.RequestConfirmation("orders.delete", params)
	require.NotNil(t, issued.Pending(pending))
	assert.False(t, strings.HasPrefix(pending, "v1."))
	approved, err := issued.Approve(pending)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(approved, "v1."))
	assert.Nil(t, issued.Pending(approved))
	assert.False(t, consumed(t, issued, pending, "orders.delete", map[string]string{"id": "1"}))
	params["id"] = "2"
	other := withSigner(t, dir, []byte("secret"), now)
	assert.False(t, consumed(t, other, approved, "orders.delete", params))
	assert.True(t, consumed(t, other, approved, "orders.delete", map[string]string{"id": "1"}))
	assert.False(t, consumed(t, other, approved, "orders.delete", map[string]string{"id": "1"}))
	restarted := withSigner(t, dir, []byte("secret"), now)
	assert.False(t, consumed(t, restarted, approved, "orders.delete", map[string]string{"id": "1"}))
	other.now = func() time.Time { return time.Unix(1_000, 0).Add(time.Minute) }
	fresh := issued.RequestConfirmation("orders.delete", map[string]string{"id": "1"})
	freshID, err := issued.Approve(fresh)
	require.NoError(t, err)
	assert.False(t, consumed(t, other, freshID, "orders.delete", map[string]string{"id": "1"}))
	wrong := withSigner(t, dir, []byte("other"), now)
	again := issued.RequestConfirmation("orders.delete", map[string]string{"id": "1"})
	token, err := issued.Approve(again)
	require.NoError(t, err)
	assert.False(t, consumed(t, wrong, token, "orders.delete", map[string]string{"id": "1"}))
}

func TestApproveOnAnotherStateIsTheOnlyWayToRun(t *testing.T) {
	dir := t.TempDir()
	caller := NewState()
	caller.SetNonceDir(dir)
	pending := caller.RequestConfirmation("orders.delete", map[string]string{"id": "9"})
	assert.False(t, consumed(t, caller, pending, "orders.delete", map[string]string{"id": "9"}))
	approver := NewState()
	approver.SetNonceDir(dir)
	approved, err := approver.Approve(pending)
	require.NoError(t, err)
	assert.NotEqual(t, pending, approved)
	assert.False(t, consumed(t, caller, pending, "orders.delete", map[string]string{"id": "9"}))
	assert.True(t, consumed(t, caller, approved, "orders.delete", map[string]string{"id": "9"}))
	assert.False(t, consumed(t, caller, approved, "orders.delete", map[string]string{"id": "9"}))
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
