// Package policy allows, checks, and confirms. A destructive call waits for a stored approval.
package policy

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/telemetry"
)

type inputKey struct{}

const (
	// DecisionAllow permits the call.
	DecisionAllow Decision = "allow"
	// DecisionDeny rejects the call.
	DecisionDeny Decision = "deny"
	// DecisionConfirmationNeeded holds the call until an approval is stored.
	DecisionConfirmationNeeded Decision = "confirmation_required"
)

type (
	// Decision is allow, deny, or confirmation required.
	Decision string

	// PendingConfirmation is a held call. The id does not authorize HTTP.
	PendingConfirmation struct {
		ID          string            `json:"ID"`
		OperationID string            `json:"OperationID"`
		Params      map[string]string `json:"Params"`
		Caller      string            `json:"Caller,omitempty"`
	}

	// Input is the call under policy. Caller is the caller or tenant.
	Input struct {
		Params    map[string]string
		Arguments map[string]any
		Caller    string
	}

	// Hook decides one operation.
	Hook interface {
		Check(ctx context.Context, op *catalog.Operation) (Decision, error)
	}

	// Around may stop before the next hook. Stop plus deny wins.
	Around func(ctx context.Context, op *catalog.Operation) (Decision, bool, error)

	// Allow nil permits every declared permission.
	Builtin struct {
		Caller string
		Allow  map[string]bool
	}

	// Wrapped is next with Around in front.
	Wrapped struct {
		next   Hook
		around Around
	}
)

// Wrap puts around in front of next. Nil next is Builtin.
func Wrap(next Hook, around Around) Wrapped {
	if next == nil {
		next = Builtin{}
	}
	return Wrapped{next: next, around: around}
}

func (w Wrapped) Check(ctx context.Context, op *catalog.Operation) (Decision, error) {
	var around Decision
	stopped := false
	if w.around != nil {
		decision, stop, err := w.around(ctx, op)
		if err != nil {
			return decision, err
		}
		around, stopped = decision, stop
		if stop && decision == DecisionDeny {
			return DecisionDeny, nil
		}
	}
	next := w.next
	if next == nil {
		next = Builtin{}
	}
	base, err := next.Check(ctx, op)
	if err != nil {
		return base, err
	}
	if base == DecisionDeny {
		return DecisionDeny, nil
	}
	if withRequired(base, op) == DecisionConfirmationNeeded {
		return DecisionConfirmationNeeded, nil
	}
	if stopped {
		return withRequired(around, op), nil
	}
	return base, nil
}

// WithInput stores the call under policy.
func WithInput(ctx context.Context, in Input) context.Context {
	return context.WithValue(ctx, inputKey{}, in)
}

// InputFrom reads the call stored by WithInput.
func InputFrom(ctx context.Context) Input {
	in, _ := ctx.Value(inputKey{}).(Input)
	if in.Params == nil {
		in.Params = map[string]string{}
	}
	return in
}

func (b Builtin) Check(ctx context.Context, op *catalog.Operation) (Decision, error) {
	if op == nil {
		return DecisionDeny, errors.New("missing operation")
	}
	_, span := telemetry.StartSpan(ctx, "policy.decision")
	defer span.End()
	if b.Allow != nil {
		for _, p := range op.Permissions {
			if p != "" && !b.Allow[p] {
				span.SetAttributes(telemetry.Attr("decision", string(DecisionDeny)))
				return DecisionDeny, nil
			}
		}
	}
	if op.RequiresConfirmation {
		span.SetAttributes(telemetry.Attr("decision", string(DecisionConfirmationNeeded)))
		return DecisionConfirmationNeeded, nil
	}
	span.SetAttributes(telemetry.Attr("decision", string(DecisionAllow)))
	return DecisionAllow, nil
}

// ConfirmSentence is the text shown for a held call.
func ConfirmSentence(operationID string, params map[string]string) string {
	var b strings.Builder
	b.WriteString("confirm ")
	b.WriteString(operationID)
	for _, k := range slices.Sorted(maps.Keys(params)) {
		fmt.Fprintf(&b, " %s=%s", k, params[k])
	}
	return b.String()
}

// ConfirmAsk is the confirmation form. The call does not run until accept.
func ConfirmAsk(caller, operationID string, params map[string]string) string {
	sentence := ConfirmSentence(operationID, params)
	if caller != "" {
		sentence = caller + ": " + sentence
	}
	return sentence + ". The call does not run until you accept."
}

// Check runs hook. Nil hook is Builtin.
func Check(ctx context.Context, hook Hook, op *catalog.Operation) (Decision, error) {
	if hook == nil {
		hook = Builtin{}
	}
	return hook.Check(ctx, op)
}

// Combine returns the stricter of a and b. A required confirmation upgrades an allow.
func Combine(a, b Decision, op *catalog.Operation) Decision {
	return withRequired(stricter(a, b), op)
}

func stricter(a, b Decision) Decision {
	if a == DecisionDeny || b == DecisionDeny {
		return DecisionDeny
	}
	if a == DecisionConfirmationNeeded || b == DecisionConfirmationNeeded {
		return DecisionConfirmationNeeded
	}
	if a != "" {
		return a
	}
	return b
}

func withRequired(d Decision, op *catalog.Operation) Decision {
	if d == DecisionDeny {
		return d
	}
	if op != nil && op.RequiresConfirmation {
		return DecisionConfirmationNeeded
	}
	return d
}
