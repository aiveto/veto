package policy

import (
	"context"
	"fmt"
	"sync"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/telemetry"
	"github.com/google/uuid"
)

// Decision is the outcome of a policy check.
type Decision string

const (
	DecisionAllow              Decision = "allow"
	DecisionDeny               Decision = "deny"
	DecisionConfirmationNeeded Decision = "confirmation_required"
)

// PendingConfirmation records an approval token for a destructive invoke.
type PendingConfirmation struct {
	ID          string
	OperationID string
	Params      map[string]string
}

// State holds per-run policy and confirmation state.
type State struct {
	mu      sync.Mutex
	pending map[string]PendingConfirmation
}

// NewState creates empty run policy state.
func NewState() *State {
	return &State{pending: map[string]PendingConfirmation{}}
}

// Hook evaluates whether an operation may run.
type Hook interface {
	Check(ctx context.Context, op *catalog.Operation) (Decision, error)
}

// Builtin allows a call unless the operation requires confirmation.
type Builtin struct{}

func (Builtin) Check(ctx context.Context, op *catalog.Operation) (Decision, error) {
	_ = ctx
	if op == nil {
		return DecisionDeny, fmt.Errorf("missing operation")
	}
	span := telemetry.StartSpan(ctx, "policy.decision")
	defer span.End()
	if op.RequiresConfirmation {
		span.SetAttributes(telemetry.Attr("decision", string(DecisionConfirmationNeeded)))
		return DecisionConfirmationNeeded, nil
	}
	for _, p := range op.Permissions {
		if p != "" {
			// Builtin allows all declared permissions in MVP.
			_ = p
		}
	}
	span.SetAttributes(telemetry.Attr("decision", string(DecisionAllow)))
	return DecisionAllow, nil
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
