package policy

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/telemetry"
	"github.com/google/uuid"
)

type inputKey struct{}

const (
	DecisionAllow              Decision = "allow"
	DecisionDeny               Decision = "deny"
	DecisionConfirmationNeeded Decision = "confirmation_required"
)

type (
	Decision string

	PendingConfirmation struct {
		ID          string
		OperationID string
		Params      map[string]string
	}

	// Consumed nonces are files in the signer directory. Two machines that share the secret and not that directory can each accept the token until expiry.
	State struct {
		mu       sync.Mutex
		pending  map[string]PendingConfirmation
		secret   []byte
		nonceDir string
		ttl      time.Duration
		now      func() time.Time
	}

	Input struct {
		Params map[string]string
	}

	Hook interface {
		Check(ctx context.Context, op *catalog.Operation) (Decision, error)
	}

	Around func(ctx context.Context, op *catalog.Operation) (Decision, bool, error)

	// Allow nil permits every declared permission.
	Builtin struct {
		Caller string
		Allow  map[string]bool
	}

	Wrapped struct {
		next   Hook
		around Around
	}
)

func Wrap(around Around) Wrapped {
	return Wrapped{next: Builtin{}, around: around}
}

func (w Wrapped) Check(ctx context.Context, op *catalog.Operation) (Decision, error) {
	if w.around != nil {
		decision, stop, err := w.around(ctx, op)
		if err != nil {
			return decision, err
		}
		if stop && !allowNeedsConfirmation(decision, op) {
			return decision, nil
		}
	}
	next := w.next
	if next == nil {
		next = Builtin{}
	}
	return next.Check(ctx, op)
}

func allowNeedsConfirmation(decision Decision, op *catalog.Operation) bool {
	return decision == DecisionAllow && op != nil && op.RequiresConfirmation
}

func NewState() *State {
	return &State{pending: map[string]PendingConfirmation{}, now: time.Now}
}

func (s *State) SetNonceDir(dir string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.nonceDir = dir
	s.mu.Unlock()
}

func (s *State) SetSigner(secret []byte, ttl time.Duration) error {
	if s == nil || len(secret) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.nonceDir
	if dir == "" {
		var err error
		dir, err = defaultNonceDir(secret)
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("approval nonce dir: %w", err)
	}
	s.secret = append([]byte(nil), secret...)
	s.nonceDir = dir
	s.ttl = ttl
	if s.ttl <= 0 {
		s.ttl = 15 * time.Minute
	}
	if s.now == nil {
		s.now = time.Now
	}
	return nil
}

func defaultNonceDir(secret []byte) (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("approval nonce dir: %w", err)
	}
	sum := sha256.Sum256(secret)
	return filepath.Join(root, "veto", "approval-nonces", hex.EncodeToString(sum[:16])), nil
}

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

func WithInput(ctx context.Context, in Input) context.Context {
	return context.WithValue(ctx, inputKey{}, in)
}

func InputFrom(ctx context.Context) Input {
	in, _ := ctx.Value(inputKey{}).(Input)
	if in.Params == nil {
		in.Params = map[string]string{}
	}
	return in
}

func (s *State) ConsumeConfirmation(approvalID, opID string, params map[string]string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.secret) > 0 {
		return s.consumeSigned(approvalID, opID, params, s.clock())
	}
	p, ok := s.pending[approvalID]
	if !ok || p.OperationID != opID || !sameParams(p.Params, params) {
		return false, nil
	}
	delete(s.pending, approvalID)
	return true, nil
}

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

func Check(ctx context.Context, hook Hook, op *catalog.Operation) (Decision, error) {
	if hook == nil {
		hook = Builtin{}
	}
	return hook.Check(ctx, op)
}

func signApproval(secret []byte, opID string, params map[string]string, exp time.Time) string {
	unix := exp.Unix()
	nonce := uuid.NewString()
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(approvalPayload(opID, params, unix, nonce)))
	sum := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("v1.%d.%s.%s", unix, nonce, sum)
}

func (s *State) consumeSigned(token, opID string, params map[string]string, now time.Time) (bool, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != "v1" || !plainNonce(parts[2]) {
		return false, nil
	}
	unix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || !now.Before(time.Unix(unix, 0)) {
		return false, nil
	}
	got, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return false, nil
	}
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(approvalPayload(opID, params, unix, parts[2])))
	if !hmac.Equal(got, mac.Sum(nil)) {
		return false, nil
	}
	if s.nonceDir == "" {
		return false, fmt.Errorf("approval nonce dir is not set")
	}
	f, err := os.OpenFile(filepath.Join(s.nonceDir, parts[2]), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("approval nonce: %w", err)
	}
	_, werr := fmt.Fprintf(f, "%d\n", unix)
	cerr := f.Close()
	if werr != nil {
		return false, fmt.Errorf("approval nonce: %w", werr)
	}
	if cerr != nil {
		return false, fmt.Errorf("approval nonce: %w", cerr)
	}
	return true, nil
}

func plainNonce(nonce string) bool {
	if nonce == "" || len(nonce) > 128 {
		return false
	}
	for _, r := range nonce {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func approvalPayload(opID string, params map[string]string, exp int64, nonce string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(opID)
	b.WriteByte('\n')
	b.WriteString(strconv.FormatInt(exp, 10))
	b.WriteByte('\n')
	b.WriteString(nonce)
	for _, k := range keys {
		b.WriteByte('\n')
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
	}
	return b.String()
}

func sameParams(a, b map[string]string) bool {
	return maps.Equal(a, b)
}

func cloneParams(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	return maps.Clone(in)
}
