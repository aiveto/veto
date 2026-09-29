package opa

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const refundRego = `package veto

import rego.v1

default decision := "allow"
default reason := ""

decision := "deny" if {
	input.operation == "payments.refund"
	to_number(input.params.amount) > 100
}

reason := "refund over limit" if {
	decision == "deny"
}
`

func TestRefundLimitKeepsBuiltinUnderTheLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "refund.rego")
	require.NoError(t, os.WriteFile(path, []byte(refundRego), 0o644))
	eng, err := New(context.Background(), path, "", policy.Builtin{Allow: map[string]bool{"pay.refund": true}})
	require.NoError(t, err)
	eng.Principal = "ada"
	eng.Environment = "prod"

	cases := []struct {
		name   string
		amount string
		op     *catalog.Operation
		next   policy.Hook
		eval   policy.Decision
		check  policy.Decision
		reason string
	}{
		{
			name:   "over the limit denies",
			amount: "150",
			op:     &catalog.Operation{ID: "payments.refund"},
			next:   policy.Builtin{},
			eval:   policy.DecisionDeny,
			check:  policy.DecisionDeny,
			reason: "refund over limit",
		},
		{
			name:   "under the limit confirms",
			amount: "40",
			op:     &catalog.Operation{ID: "payments.refund", RequiresConfirmation: true, Permissions: []string{"pay.refund"}},
			next:   policy.Builtin{Allow: map[string]bool{"pay.refund": true}},
			eval:   policy.DecisionAllow,
			check:  policy.DecisionConfirmationNeeded,
		},
		{
			name:   "under the limit a missing permission denies",
			amount: "40",
			op:     &catalog.Operation{ID: "payments.refund", Permissions: []string{"pay.refund"}},
			next:   policy.Builtin{Allow: map[string]bool{}},
			eval:   policy.DecisionAllow,
			check:  policy.DecisionDeny,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng.next = tc.next
			ctx := policy.WithInput(context.Background(), policy.Input{Params: map[string]string{"amount": tc.amount}})
			got, reason, err := eng.evaluate(ctx, tc.op)
			require.NoError(t, err)
			assert.Equal(t, tc.eval, got)
			if tc.reason != "" {
				assert.Equal(t, tc.reason, reason)
			}
			got, err = eng.Check(ctx, tc.op)
			require.NoError(t, err)
			assert.Equal(t, tc.check, got)
		})
	}
}
