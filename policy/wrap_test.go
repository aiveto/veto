package policy

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aiveto/veto/catalog"
)

func TestWrapCallsBuiltinUnlessItStops(t *testing.T) {
	del := &catalog.Operation{ID: "orders.delete", RequiresConfirmation: true}
	cases := []struct {
		name   string
		around Around
		want   Decision
		err    string
	}{
		{name: "around falls through", around: func(context.Context, *catalog.Operation) (Decision, bool, error) {
			return DecisionAllow, false, nil
		}, want: DecisionConfirmationNeeded},
		{name: "allow stop still confirms a delete", around: func(context.Context, *catalog.Operation) (Decision, bool, error) {
			return DecisionAllow, true, nil
		}, want: DecisionConfirmationNeeded},
		{name: "around stops", around: func(context.Context, *catalog.Operation) (Decision, bool, error) {
			return DecisionDeny, true, nil
		}, want: DecisionDeny},
		{name: "around error skips builtin", around: func(context.Context, *catalog.Operation) (Decision, bool, error) {
			return "", false, fmt.Errorf("nope")
		}, err: "nope"},
		{name: "nil around is builtin", want: DecisionConfirmationNeeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Wrap(nil, tc.around).Check(context.Background(), del)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
	var zero Wrapped
	got, err := zero.Check(context.Background(), del)
	require.NoError(t, err)
	assert.Equal(t, DecisionConfirmationNeeded, got)
}

func TestWrapKeepsAPermissionDenial(t *testing.T) {
	op := &catalog.Operation{ID: "orders.get", Permissions: []string{"orders.read"}}
	next := Builtin{Allow: map[string]bool{}}
	got, err := Wrap(next, func(context.Context, *catalog.Operation) (Decision, bool, error) {
		return DecisionAllow, true, nil
	}).Check(context.Background(), op)
	require.NoError(t, err)
	assert.Equal(t, DecisionDeny, got)
}
