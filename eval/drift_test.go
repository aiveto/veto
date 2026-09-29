package eval_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aiveto/veto/eval"
)

func TestDriftLocksOperationAndConfirmation(t *testing.T) {
	base := []eval.CaseExpect{
		{Name: "delete", Operation: "orders.delete", ConfirmationRequired: true},
		{Name: "gone", Operation: "orders.get"},
	}
	next := []eval.CaseExpect{
		{Name: "delete", Operation: "orders.delete", ConfirmationRequired: false},
		{Name: "added", Operation: "orders.list"},
	}
	assert.Equal(t, []string{
		"eval case delete changed operation or confirmation",
		"eval case gone was removed",
	}, eval.Drift(base, next))
	assert.Empty(t, eval.Drift(base, base))
}
