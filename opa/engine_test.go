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

const factsRego = `package veto

import rego.v1

default decision := "allow"
default reason := ""

decision := "deny" if {
	input.operation == "bare.call"
	input.method == ""
	input.path == ""
	input.side_effect == ""
	input.caller == ""
	input.environment == ""
	input.resource_group == ""
	count(input.params) == 0
	count(input.permissions) == 0
	count(input.auth_scheme) == 0
	count(input.tags) == 0
}

decision := "deny" if {
	input.operation == "orders.delete"
	input.method == "DELETE"
	input.path == "/orders/{id}"
	input.side_effect == "destructive"
	input.caller == "ada"
	input.environment == "prod"
	input.resource_group == "orders"
	input.params.id == "123"
	input.permissions == ["orders.delete"]
	input.auth_scheme == ["apiKey", "bearerAuth"]
	input.tags == ["orders"]
}
`

func TestInputUsesCallFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "facts.rego")
	require.NoError(t, os.WriteFile(path, []byte(factsRego), 0o644))
	eng, err := New(context.Background(), path, "", policy.Builtin{})
	require.NoError(t, err)

	bare, _, err := eng.evaluate(context.Background(), &catalog.Operation{
		ID:   "bare.call",
		Auth: []catalog.Auth{{Kind: "bearer"}},
	})
	require.NoError(t, err)
	assert.Equal(t, policy.DecisionDeny, bare)

	eng.Environment = "prod"
	eng.Principal = "from-config"

	full := &catalog.Operation{
		ID:           "orders.delete",
		Method:       "DELETE",
		PathTemplate: "/orders/{id}",
		SideEffect:   catalog.SideEffectDestructive,
		Permissions:  []string{"orders.delete"},
		Group:        "orders",
		Tags:         []string{"orders"},
		Auth: []catalog.Auth{
			{Name: "apiKey", Kind: "apiKey"},
			{Name: "bearerAuth", Kind: "bearer"},
		},
	}
	ctx := policy.WithInput(context.Background(), policy.Input{
		Params: map[string]string{"id": "123"},
		Caller: "ada",
	})
	got, _, err := eng.evaluate(ctx, full)
	require.NoError(t, err)
	assert.Equal(t, policy.DecisionDeny, got)

	fallback, _, err := eng.evaluate(policy.WithInput(context.Background(), policy.Input{
		Params: map[string]string{"id": "123"},
	}), full)
	require.NoError(t, err)
	assert.Equal(t, policy.DecisionAllow, fallback)

	eng.Principal = "ada"
	fromConfig, _, err := eng.evaluate(policy.WithInput(context.Background(), policy.Input{
		Params: map[string]string{"id": "123"},
	}), full)
	require.NoError(t, err)
	assert.Equal(t, policy.DecisionDeny, fromConfig)
}
