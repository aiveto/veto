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
		"eval case delete changed expectation",
		"eval case gone was removed",
	}, eval.Drift(base, next))
	assert.Empty(t, eval.Drift(base, base))
	quiet := base[0]
	quiet.NoHTTP = true
	assert.Equal(t, []string{
		"eval case delete changed expectation",
	}, eval.Drift([]eval.CaseExpect{base[0]}, []eval.CaseExpect{quiet}))
	packed := base[0]
	packed.PackContains = []string{"orders.delete"}
	assert.Equal(t, []string{
		"eval case delete changed expectation",
	}, eval.Drift([]eval.CaseExpect{base[0]}, []eval.CaseExpect{packed}))
	related := base[0]
	related.Related = []string{"customers.get"}
	assert.Equal(t, []string{
		"eval case delete changed expectation",
	}, eval.Drift([]eval.CaseExpect{base[0]}, []eval.CaseExpect{related}))
}
