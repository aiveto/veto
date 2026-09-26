package policy

import "testing"

func TestConfirmationKeepsItsOwnParams(t *testing.T) {
	s := NewState()
	params := map[string]string{"id": "1"}
	id := s.RequestConfirmation("assets.delete", params)
	params["id"] = "changed"
	got := s.Pending(id)
	if got == nil || got.Params["id"] != "1" {
		t.Fatalf("stored params changed with the caller map: %+v", got)
	}
}
