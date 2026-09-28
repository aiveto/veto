package policy

import (
	"context"
	"fmt"
	"testing"

	"github.com/aiveto/veto/catalog"
)

func TestWrapCallsBuiltinUnlessItStops(t *testing.T) {
	del := &catalog.Operation{ID: "assets.delete", RequiresConfirmation: true}
	pass := Wrap(func(ctx context.Context, op *catalog.Operation) (Decision, bool, error) {
		return DecisionAllow, false, nil
	})
	got, err := pass.Check(context.Background(), del)
	if err != nil || got != DecisionConfirmationNeeded {
		t.Fatalf("builtin did not run: %s %v", got, err)
	}

	stop := Wrap(func(ctx context.Context, op *catalog.Operation) (Decision, bool, error) {
		return DecisionDeny, true, nil
	})
	got, err = stop.Check(context.Background(), del)
	if err != nil || got != DecisionDeny {
		t.Fatalf("stop: %s %v", got, err)
	}

	broken := Wrap(func(ctx context.Context, op *catalog.Operation) (Decision, bool, error) {
		return "", false, fmt.Errorf("nope")
	})
	if _, err := broken.Check(context.Background(), del); err == nil {
		t.Fatal("error did not skip builtin")
	}

	if got, err := Wrap(nil).Check(context.Background(), del); err != nil || got != DecisionConfirmationNeeded {
		t.Fatalf("nil around: %s %v", got, err)
	}
	var zero Wrapped
	if got, err := zero.Check(context.Background(), del); err != nil || got != DecisionConfirmationNeeded {
		t.Fatalf("zero wrap: %s %v", got, err)
	}
}
