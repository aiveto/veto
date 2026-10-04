package policy_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/policy"
)

func TestConfirmAskNamesCallerOperationAndParams(t *testing.T) {
	assert.Equal(t, "confirm orders.delete id=123. The call does not run until you accept.", policy.ConfirmAsk("", "orders.delete", map[string]string{"id": "123"}))
	assert.Equal(t, "ada: confirm orders.delete id=123. The call does not run until you accept.", policy.ConfirmAsk("ada", "orders.delete", map[string]string{"id": "123"}))
}

func TestPermissionAndConfirmation(t *testing.T) {
	op := &catalog.Operation{
		ID:                   "orders.delete",
		Permissions:          []string{"order.delete"},
		RequiresConfirmation: true,
	}
	cases := []struct {
		name  string
		allow map[string]bool
		want  policy.Decision
	}{
		{name: "nil allow confirms", want: policy.DecisionConfirmationNeeded},
		{name: "empty allow denies", allow: map[string]bool{}, want: policy.DecisionDeny},
		{name: "granted permission confirms", allow: map[string]bool{"order.delete": true}, want: policy.DecisionConfirmationNeeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hook := policy.Builtin{Caller: "local", Allow: tc.allow}
			got, err := policy.Check(context.Background(), hook, op)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
