package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aiveto/veto/agent"
)

func TestScriptedRequiresAPack(t *testing.T) {
	_, err := agent.NewScripted().Complete(context.Background(), agent.Request{UserMessage: "delete order 123"})
	assert.ErrorIs(t, err, agent.ErrEmptyPack)
}

func TestScriptedRequiresTheMessageInThePack(t *testing.T) {
	cases := []struct {
		name string
		pack string
		want string
	}{
		{name: "absent", pack: "rules: search then describe then invoke", want: ""},
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

func TestScriptedReadsTheWholeOrderID(t *testing.T) {
	cases := []struct {
		msg string
		id  string
	}{
		{msg: "delete order 123", id: "123"},
		{msg: "delete order 10abc", id: "10abc"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			got, err := agent.NewScripted().Complete(t.Context(), agent.Request{
				UserMessage: tc.msg,
				Context:     tc.msg,
			})
			require.NoError(t, err)
			assert.Equal(t, "orders.delete", got.OperationID)
			assert.Equal(t, tc.id, got.Params["id"])
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
