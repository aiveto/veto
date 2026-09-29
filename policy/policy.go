package policy

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/telemetry"
	"github.com/google/uuid"
)

const (
	DecisionAllow              Decision = "allow"
	DecisionDeny               Decision = "deny"
	DecisionConfirmationNeeded Decision = "confirmation_required"
)

type (
	// Decision is the outcome of a policy check.
	Decision string

	// PendingConfirmation records an approval token for a destructive invoke.
	PendingConfirmation struct {
		ID          string
		OperationID string
		Params      map[string]string
	}

	// State holds per-run policy and confirmation state.
	State struct {
		mu      sync.Mutex
		pending map[string]PendingConfirmation
	}

	// Hook evaluates whether an operation may run.
	Hook interface {
		Check(ctx context.Context, op *catalog.Operation) (Decision, error)
	}

	// Around is one check in front of Builtin. A true stop skips Builtin.
	Around func(ctx context.Context, op *catalog.Operation) (Decision, bool, error)

	// Builtin allows a call unless the operation requires confirmation.
	// Allow nil permits every declared permission. A set denies a permission that is missing.
	Builtin struct {
		Caller string
		Allow  map[string]bool
	}

	// Wrapped calls Around, then Builtin, unless Around stops.
	Wrapped struct {
		next   Hook
		around Around
	}
)

// Wrap returns a hook whose next is Builtin. A nil around is Builtin alone.
func Wrap(around Around) Wrapped {
	return Wrapped{next: Builtin{}, around: around}
}

// Check runs around and then Builtin. stop or an error skips Builtin.
func (w Wrapped) Check(ctx context.Context, op *catalog.Operation) (Decision, error) {
	if w.around != nil {
		decision, stop, err := w.around(ctx, op)
		if err != nil || stop {
			return decision, err
		}
	}
	next := w.next
	if next == nil {
		next = Builtin{}
	}
	return next.Check(ctx, op)
}

// NewState creates empty run policy state.
func NewState() *State {
	return &State{pending: map[string]PendingConfirmation{}}
}

func (b Builtin) Check(ctx context.Context, op *catalog.Operation) (Decision, error) {
	if op == nil {
		return DecisionDeny, fmt.Errorf("missing operation")
	}
	span := telemetry.StartSpan(ctx, "policy.decision")
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

// ConfirmSentence is the line a human confirms and the pack repeats.
func ConfirmSentence(operationID string, params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("confirm ")
	b.WriteString(operationID)
	for _, k := range keys {
		b.WriteByte(' ')
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
	}
	return b.String()
}

// Check runs the hook and returns a decision for op.
func Check(ctx context.Context, hook Hook, op *catalog.Operation) (Decision, error) {
	if hook == nil {
		hook = Builtin{}
	}
	return hook.Check(ctx, op)
}

// RequestConfirmation stores pending approval and returns its id.
func (s *State) RequestConfirmation(opID string, params map[string]string) string {
	id := uuid.NewString()
	s.mu.Lock()
	s.pending[id] = PendingConfirmation{ID: id, OperationID: opID, Params: cloneParams(params)}
	s.mu.Unlock()
	return id
}

// ConsumeConfirmation marks approval id as used if it matches op.
func (s *State) ConsumeConfirmation(approvalID, opID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[approvalID]
	if !ok || p.OperationID != opID {
		return false
	}
	delete(s.pending, approvalID)
	return true
}

// Pending returns a copy of the confirmation for id, if any.
func (s *State) Pending(id string) *PendingConfirmation {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[id]
	if !ok {
		return nil
	}
	p.Params = cloneParams(p.Params)
	return &p
}

func cloneParams(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
