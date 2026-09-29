package policy

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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
	// A signer secret makes approval ids HMAC tokens. Those are not stored.
	State struct {
		mu      sync.Mutex
		pending map[string]PendingConfirmation
		secret  []byte
		ttl     time.Duration
		now     func() time.Time
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
	return &State{pending: map[string]PendingConfirmation{}, now: time.Now}
}

// SetSigner issues approval ids as HMAC tokens. The caller holds the token.
// An empty secret leaves process maps in place. A non-positive ttl uses 15 minutes.
func (s *State) SetSigner(secret []byte, ttl time.Duration) {
	if s == nil || len(secret) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secret = append([]byte(nil), secret...)
	s.ttl = ttl
	if s.ttl <= 0 {
		s.ttl = 15 * time.Minute
	}
	if s.now == nil {
		s.now = time.Now
	}
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

// RequestConfirmation returns an approval id.
// With a signer, the id is an HMAC of the operation, params, and expiry, and nothing is stored.
func (s *State) RequestConfirmation(opID string, params map[string]string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.secret) > 0 {
		return signApproval(s.secret, opID, params, s.deadline())
	}
	id := uuid.NewString()
	s.pending[id] = PendingConfirmation{ID: id, OperationID: opID, Params: cloneParams(params)}
	return id
}

// ConsumeConfirmation accepts approvalID for opID and params.
// A signed token is checked and not remembered. A process id is used once.
func (s *State) ConsumeConfirmation(approvalID, opID string, params map[string]string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.secret) > 0 {
		return validApproval(s.secret, approvalID, opID, params, s.clock())
	}
	p, ok := s.pending[approvalID]
	if !ok || p.OperationID != opID || !sameParams(p.Params, params) {
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

func (s *State) clock() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

func (s *State) deadline() time.Time {
	ttl := s.ttl
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return s.clock().Add(ttl)
}

func signApproval(secret []byte, opID string, params map[string]string, exp time.Time) string {
	unix := exp.Unix()
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(approvalPayload(opID, params, unix)))
	sum := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("v1.%d.%s", unix, sum)
}

func validApproval(secret []byte, token, opID string, params map[string]string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return false
	}
	unix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return false
	}
	if !now.Before(time.Unix(unix, 0)) {
		return false
	}
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(approvalPayload(opID, params, unix)))
	return hmac.Equal(got, mac.Sum(nil))
}

func approvalPayload(opID string, params map[string]string, exp int64) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(opID)
	b.WriteByte('\n')
	b.WriteString(strconv.FormatInt(exp, 10))
	for _, k := range keys {
		b.WriteByte('\n')
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
	}
	return b.String()
}

func sameParams(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
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
