package eval_test

import (
	"testing"

	"github.com/aiveto/veto/eval"
)

func TestDriftLocksOperationAndConfirmation(t *testing.T) {
	base := []eval.CaseExpect{
		{Name: "delete", Operation: "assets.delete", ConfirmationRequired: true},
		{Name: "gone", Operation: "assets.get"},
	}
	next := []eval.CaseExpect{
		{Name: "delete", Operation: "assets.delete", ConfirmationRequired: false},
		{Name: "added", Operation: "assets.list"},
	}
	got := eval.Drift(base, next)
	if len(got) != 2 {
		t.Fatalf("drift: %v", got)
	}
	if len(eval.Drift(base, base)) != 0 {
		t.Fatal("unchanged cases drifted")
	}
}
