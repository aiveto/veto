package policy_test

import (
	"context"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/policy"
)

func TestMissingPermissionDeniesAndDefaultAllows(t *testing.T) {
	op := &catalog.Operation{
		ID:                   "assets.delete",
		Permissions:          []string{"asset.delete"},
		RequiresConfirmation: true,
	}
	got, err := policy.Check(context.Background(), policy.Builtin{Caller: "local"}, op)
	if err != nil || got != policy.DecisionConfirmationNeeded {
		t.Fatalf("default: %s %v", got, err)
	}
	got, err = policy.Check(context.Background(), policy.Builtin{Caller: "local", Allow: map[string]bool{}}, op)
	if err != nil || got != policy.DecisionDeny {
		t.Fatalf("missing: %s %v", got, err)
	}
	got, err = policy.Check(context.Background(), policy.Builtin{Allow: map[string]bool{"asset.delete": true}}, op)
	if err != nil || got != policy.DecisionConfirmationNeeded {
		t.Fatalf("granted: %s %v", got, err)
	}
}
