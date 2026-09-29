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
		{name: "present", pack: "user: delete asset 123", want: "assets.delete"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := agent.NewScripted().Complete(context.Background(), agent.Request{
				UserMessage: "delete asset 123",
				Context:     tc.pack,
			})
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.OperationID)
		})
	}
}
