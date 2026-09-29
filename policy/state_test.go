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
	id := s.RequestConfirmation("assets.delete", params)
	params["id"] = "changed"
	got := s.Pending(id)
	require.NotNil(t, got)
	assert.Equal(t, "1", got.Params["id"])
	assert.True(t, s.ConsumeConfirmation(id, "assets.delete", map[string]string{"id": "1"}))
	assert.False(t, s.ConsumeConfirmation(id, "assets.delete", map[string]string{"id": "1"}))
}

func TestSignedApprovalIsNotStored(t *testing.T) {
	params := map[string]string{"id": "1"}
	issued := NewState()
	issued.SetSigner([]byte("secret"), time.Minute)
	issued.now = func() time.Time { return time.Unix(1_000, 0) }
	token := issued.RequestConfirmation("assets.delete", params)
	assert.Nil(t, issued.Pending(token))
	params["id"] = "2"
	other := NewState()
	other.SetSigner([]byte("secret"), time.Minute)
	other.now = issued.now
	assert.False(t, other.ConsumeConfirmation(token, "assets.delete", params))
	assert.True(t, other.ConsumeConfirmation(token, "assets.delete", map[string]string{"id": "1"}))
	assert.True(t, other.ConsumeConfirmation(token, "assets.delete", map[string]string{"id": "1"}))
	other.now = func() time.Time { return time.Unix(1_000, 0).Add(time.Minute) }
	fresh := issued.RequestConfirmation("assets.delete", map[string]string{"id": "1"})
	assert.False(t, other.ConsumeConfirmation(fresh, "assets.delete", map[string]string{"id": "1"}))
	wrong := NewState()
	wrong.SetSigner([]byte("other"), time.Minute)
	wrong.now = issued.now
	assert.False(t, wrong.ConsumeConfirmation(token, "assets.delete", map[string]string{"id": "1"}))
}
