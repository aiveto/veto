package policy

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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

	confirmation struct {
		ID          string            `json:"id"`
		OperationID string            `json:"operation_id"`
		Params      map[string]string `json:"params,omitempty"`
		Status      string            `json:"status"`
		Expiry      int64             `json:"expiry,omitempty"`
		ApprovedID  string            `json:"approved_id,omitempty"`
		Caller      string            `json:"caller,omitempty"`
	}

	// Consumed nonces are files in the signer directory. Two machines that share the secret and not that directory can each accept the token until expiry.
	State struct {
		mu       sync.Mutex
		pending  map[string]confirmation
		approved map[string]string
		secret   []byte
		nonceDir string
		ttl      time.Duration
		now      func() time.Time
	}

	// Input is the call under policy. Caller is the caller or tenant.
	Input struct {
		Params    map[string]string
		Arguments map[string]any
		Caller    string
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
	if base == DecisionConfirmationNeeded || (op != nil && op.RequiresConfirmation) {
		return DecisionConfirmationNeeded, nil
	}
	if stopped {
		if allowNeedsConfirmation(around, op) {
			return DecisionConfirmationNeeded, nil
		}
		return around, nil
	}
	return base, nil
}

func allowNeedsConfirmation(decision Decision, op *catalog.Operation) bool {
	return decision == DecisionAllow && op != nil && op.RequiresConfirmation
}

const (
	statusPending  = "pending"
	statusApproved = "approved"
	statusConsumed = "consumed"
)

func NewState() *State {
	return &State{
		pending:  map[string]confirmation{},
		approved: map[string]string{},
		now:      time.Now,
	}
}

// ApplyEnv points state at the approval directory serve and approve share.
// A signing secret makes the approved id a token. The pending id stays a handle.
func ApplyEnv(s *State) error {
	if s == nil {
		return fmt.Errorf("missing approval state")
	}
	dir := os.Getenv("VETO_APPROVAL_NONCE_DIR")
	secret := os.Getenv("VETO_APPROVAL_SECRET")
	if dir == "" && secret == "" {
		var err error
		dir, err = DefaultApprovalDir()
		if err != nil {
			return err
		}
	}
	if dir != "" {
		s.SetNonceDir(dir)
	}
	if secret != "" {
		return s.SetSigner([]byte(secret), 0)
	}
	return nil
}

func DefaultApprovalDir() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("approval dir: %w", err)
	}
	return filepath.Join(root, "veto", "approvals"), nil
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

// RequestConfirmation records a pending call. The id it returns does not authorize HTTP.
// A store error means the file was not written, so there is no pending id to approve.
func (s *State) RequestConfirmation(opID string, params map[string]string) (string, error) {
	return s.RequestFor("", opID, params)
}

// RequestFor records a pending call for one caller. The id it returns does not authorize HTTP.
// A store error means the file was not written, so there is no pending id to approve.
func (s *State) RequestFor(caller, opID string, params map[string]string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := uuid.NewString()
	rec := confirmation{
		ID:          id,
		OperationID: opID,
		Params:      cloneParams(params),
		Status:      statusPending,
		Caller:      caller,
	}
	if len(s.secret) > 0 {
		rec.Expiry = s.deadline().Unix()
	}
	if err := s.storeLocked(rec); err != nil {
		delete(s.pending, id)
		return "", err
	}
	return id, nil
}

// Approve records a separate decision. The returned id is what a later invoke accepts once.
func (s *State) Approve(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.loadLocked(id)
	if !ok || rec.Status == statusConsumed || expired(rec, s.clock()) {
		return "", fmt.Errorf("unknown approval")
	}
	if rec.Status == statusApproved && rec.ApprovedID != "" {
		return rec.ApprovedID, nil
	}
	if rec.Status != statusPending {
		return "", fmt.Errorf("unknown approval")
	}
	approved := uuid.NewString()
	if len(s.secret) > 0 {
		exp := s.deadline()
		if rec.Expiry != 0 {
			exp = time.Unix(rec.Expiry, 0)
		}
		approved = signApproval(s.secret, rec.Caller, rec.OperationID, rec.Params, exp)
	}
	rec.Status = statusApproved
	rec.ApprovedID = approved
	if err := s.storeLocked(rec); err != nil {
		return "", err
	}
	return approved, nil
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
	return s.ConsumeFor("", approvalID, opID, params)
}

// ConsumeFor accepts an approved id once, and only for the caller that received it.
func (s *State) ConsumeFor(caller, approvalID, opID string, params map[string]string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.findApprovedLocked(approvalID)
	if !ok || rec.Status != statusApproved || rec.ApprovedID != approvalID {
		return false, nil
	}
	if rec.Caller != caller || rec.OperationID != opID || !sameParams(rec.Params, params) || expired(rec, s.clock()) {
		return false, nil
	}
	claimed := false
	if len(s.secret) > 0 {
		ok, err := s.consumeSigned(caller, approvalID, opID, params, s.clock())
		if err != nil || !ok {
			return ok, err
		}
	} else if s.nonceDir != "" {
		ok, err := s.claimLocked(rec.ID)
		if err != nil || !ok {
			return ok, err
		}
		claimed = true
	}
	rec.Status = statusConsumed
	if err := s.storeLocked(rec); err != nil {
		if claimed {
			_ = os.Remove(filepath.Join(s.nonceDir, "confirmations", rec.ID+".claimed"))
		}
		return false, err
	}
	delete(s.approved, approvalID)
	return true, nil
}

func (s *State) Pending(id string) *PendingConfirmation {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.loadLocked(id)
	if !ok || rec.Status != statusPending {
		return nil
	}
	return &PendingConfirmation{ID: rec.ID, OperationID: rec.OperationID, Params: cloneParams(rec.Params)}
}

func expired(rec confirmation, now time.Time) bool {
	return rec.Expiry != 0 && !now.Before(time.Unix(rec.Expiry, 0))
}

func (s *State) storeLocked(rec confirmation) error {
	if err := s.writeLocked(rec); err != nil {
		return err
	}
	if s.pending == nil {
		s.pending = map[string]confirmation{}
	}
	if s.approved == nil {
		s.approved = map[string]string{}
	}
	s.pending[rec.ID] = rec
	if rec.ApprovedID != "" && rec.Status == statusApproved {
		s.approved[rec.ApprovedID] = rec.ID
	}
	if rec.Status == statusConsumed {
		delete(s.approved, rec.ApprovedID)
	}
	return nil
}

func (s *State) writeLocked(rec confirmation) error {
	if s.nonceDir == "" {
		return nil
	}
	if !plainID(rec.ID) {
		return fmt.Errorf("approval id")
	}
	dir := filepath.Join(s.nonceDir, "confirmations")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("approval dir: %w", err)
	}
	body, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("approval: %w", err)
	}
	path := filepath.Join(dir, rec.ID+".json")
	tmp, err := os.CreateTemp(dir, rec.ID+".*.tmp")
	if err != nil {
		return fmt.Errorf("approval: %w", err)
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(body)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmpName)
		if werr != nil {
			return fmt.Errorf("approval: %w", werr)
		}
		return fmt.Errorf("approval: %w", cerr)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("approval: %w", err)
	}
	return nil
}

func (s *State) claimLocked(id string) (bool, error) {
	if !plainID(id) {
		return false, fmt.Errorf("approval id")
	}
	dir := filepath.Join(s.nonceDir, "confirmations")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, fmt.Errorf("approval dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, id+".claimed"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("approval: %w", err)
	}
	if err := f.Close(); err != nil {
		return false, fmt.Errorf("approval: %w", err)
	}
	return true, nil
}

func (s *State) loadLocked(id string) (confirmation, bool) {
	if s.nonceDir != "" && plainID(id) {
		if rec, ok := s.readFile(id); ok {
			s.remember(rec)
			return rec, true
		}
	}
	rec, ok := s.pending[id]
	return rec, ok
}

func (s *State) findApprovedLocked(approved string) (confirmation, bool) {
	if approved == "" {
		return confirmation{}, false
	}
	if s.nonceDir != "" {
		entries, err := os.ReadDir(filepath.Join(s.nonceDir, "confirmations"))
		if err != nil {
			return confirmation{}, false
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			id := strings.TrimSuffix(entry.Name(), ".json")
			rec, ok := s.readFile(id)
			if ok && rec.ApprovedID == approved {
				s.remember(rec)
				return rec, true
			}
		}
		return confirmation{}, false
	}
	id, ok := s.approved[approved]
	if !ok {
		return confirmation{}, false
	}
	rec, ok := s.pending[id]
	return rec, ok
}

func (s *State) remember(rec confirmation) {
	if s.pending == nil {
		s.pending = map[string]confirmation{}
	}
	s.pending[rec.ID] = rec
	if rec.ApprovedID != "" && rec.Status == statusApproved {
		if s.approved == nil {
			s.approved = map[string]string{}
		}
		s.approved[rec.ApprovedID] = rec.ID
	}
}

func (s *State) readFile(id string) (confirmation, bool) {
	body, err := os.ReadFile(filepath.Join(s.nonceDir, "confirmations", id+".json"))
	if err != nil {
		return confirmation{}, false
	}
	var rec confirmation
	if err := json.Unmarshal(body, &rec); err != nil || rec.ID != id {
		return confirmation{}, false
	}
	return rec, true
}

func plainID(id string) bool {
	return plainNonce(id) && !strings.Contains(id, ".")
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

func signApproval(secret []byte, caller, opID string, params map[string]string, exp time.Time) string {
	unix := exp.Unix()
	nonce := uuid.NewString()
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(approvalPayload(caller, opID, params, unix, nonce)))
	sum := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("v1.%d.%s.%s", unix, nonce, sum)
}

func (s *State) consumeSigned(caller, token, opID string, params map[string]string, now time.Time) (bool, error) {
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
	_, _ = mac.Write([]byte(approvalPayload(caller, opID, params, unix, parts[2])))
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

func approvalPayload(caller, opID string, params map[string]string, exp int64, nonce string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(opID)
	b.WriteByte('\n')
	b.WriteString(caller)
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
