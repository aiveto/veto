package policy

import (
	"testing"
	"time"
)

func TestConfirmationKeepsItsOwnParams(t *testing.T) {
	s := NewState()
	params := map[string]string{"id": "1"}
	id := s.RequestConfirmation("assets.delete", params)
	params["id"] = "changed"
	got := s.Pending(id)
	if got == nil || got.Params["id"] != "1" {
		t.Fatalf("stored params changed with the caller map: %+v", got)
	}
	if !s.ConsumeConfirmation(id, "assets.delete", map[string]string{"id": "1"}) {
		t.Fatal("approval did not match the stored call")
	}
	if s.ConsumeConfirmation(id, "assets.delete", map[string]string{"id": "1"}) {
		t.Fatal("approval was reused")
	}
}

func TestSignedApprovalIsNotStored(t *testing.T) {
	params := map[string]string{"id": "1"}
	issued := NewState()
	issued.SetSigner([]byte("secret"), time.Minute)
	issued.now = func() time.Time { return time.Unix(1_000, 0) }
	token := issued.RequestConfirmation("assets.delete", params)
	if issued.Pending(token) != nil {
		t.Fatal("signed approval was stored")
	}
	params["id"] = "2"
	other := NewState()
	other.SetSigner([]byte("secret"), time.Minute)
	other.now = issued.now
	if other.ConsumeConfirmation(token, "assets.delete", params) {
		t.Fatal("token matched different params")
	}
	if !other.ConsumeConfirmation(token, "assets.delete", map[string]string{"id": "1"}) {
		t.Fatal("token was rejected")
	}
	if !other.ConsumeConfirmation(token, "assets.delete", map[string]string{"id": "1"}) {
		t.Fatal("signed token did not survive a second check")
	}
	other.now = func() time.Time { return time.Unix(1_000, 0).Add(time.Minute) }
	fresh := issued.RequestConfirmation("assets.delete", map[string]string{"id": "1"})
	if other.ConsumeConfirmation(fresh, "assets.delete", map[string]string{"id": "1"}) {
		t.Fatal("expired token was accepted")
	}
	wrong := NewState()
	wrong.SetSigner([]byte("other"), time.Minute)
	wrong.now = issued.now
	if wrong.ConsumeConfirmation(token, "assets.delete", map[string]string{"id": "1"}) {
		t.Fatal("token matched a different secret")
	}
}
