package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aiveto/veto/agent"
)

func TestScriptedRequiresTheMessageInThePack(t *testing.T) {
	cases := []struct {
		name string
		pack string
		want string
	}{
		{name: "omitted", want: ""},
		{name: "present", pack: "user: delete order 123", want: "orders.delete"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := agent.NewScripted().Complete(context.Background(), agent.Request{
				UserMessage: "delete order 123",
				Context:     tc.pack,
			})
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.OperationID)
		})
	}
}

func TestWithOperationDoesNotChangeTheOriginal(t *testing.T) {
	base := agent.NewScripted()
	next := base.WithOperation("assets.delete")
	got, err := next.Complete(t.Context(), agent.Request{UserMessage: "delete order 9", Context: "delete order 9"})
	require.NoError(t, err)
	assert.Equal(t, "assets.delete", got.OperationID)
	orig, err := base.Complete(t.Context(), agent.Request{UserMessage: "delete order 9", Context: "delete order 9"})
	require.NoError(t, err)
	assert.Equal(t, "orders.delete", orig.OperationID)
}
