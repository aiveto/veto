package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	statusPending      = "pending"
	statusApproved     = "approved"
	statusConsumed     = "consumed"
	defaultApprovalTTL = 15 * time.Minute
)

// ErrUnknownApproval is an id approve cannot find, or one that is already consumed or expired.
var ErrUnknownApproval = errors.New("unknown approval")

type (
	confirmation struct {
		ID          string            `json:"id"`
		OperationID string            `json:"operation_id"`
		Params      map[string]string `json:"params,omitempty"`
		Status      string            `json:"status"`
		Expiry      int64             `json:"expiry,omitempty"`
		ApprovedID  string            `json:"approved_id,omitempty"`
		Caller      string            `json:"caller,omitempty"`
	}

	// State is the confirmation use case. Memory is the store. SetNonceDir and SetSigner attach adapters.
	State struct {
		mu      sync.Mutex
		records store
		tokens  signer
		ttl     time.Duration
		now     func() time.Time
	}
)

func NewState() *State {
	return &State{records: &memoryStore{}, now: time.Now}
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
	s.records = &fileStore{dir: dir}
	s.mu.Unlock()
}

func (s *State) SetSigner(secret []byte, ttl time.Duration) error {
	if s == nil {
		return errors.New("missing approval state")
	}
	if len(secret) == 0 {
		return errors.New("approval secret is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := persistDir(s.records)
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
	if persistDir(s.records) == "" {
		s.records = &fileStore{dir: dir}
	}
	s.tokens = hmacSigner{secret: append([]byte(nil), secret...), dir: dir}
	s.ttl = ttl
	if s.ttl <= 0 {
		s.ttl = defaultApprovalTTL
	}
	if s.now == nil {
		s.now = time.Now
	}
	return nil
}

func persistDir(records store) string {
	f, ok := records.(*fileStore)
	if !ok {
		return ""
	}
	return f.dir
}

func defaultNonceDir(secret []byte) (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("approval nonce dir: %w", err)
	}
	sum := sha256.Sum256(secret)
	return filepath.Join(root, "veto", "approval-nonces", hex.EncodeToString(sum[:16])), nil
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
		Expiry:      s.deadline().Unix(),
	}
	if err := s.records.put(rec); err != nil {
		s.records.remove(rec)
		return "", err
	}
	return id, nil
}

// Approve records a separate decision. The returned id is what a later invoke accepts once.
func (s *State) Approve(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records.get(id)
	if !ok || rec.Status == statusConsumed || expired(rec, s.clock()) {
		if ok && expired(rec, s.clock()) {
			s.records.remove(rec)
		}
		return "", ErrUnknownApproval
	}
	if rec.Status == statusApproved && rec.ApprovedID != "" {
		return rec.ApprovedID, nil
	}
	if rec.Status != statusPending {
		return "", ErrUnknownApproval
	}
	approved := uuid.NewString()
	if s.tokens != nil {
		exp := s.deadline()
		if rec.Expiry != 0 {
			exp = time.Unix(rec.Expiry, 0)
		}
		approved = s.tokens.sign(rec.Caller, rec.OperationID, rec.Params, exp)
	}
	rec.Status = statusApproved
	rec.ApprovedID = approved
	if err := s.records.put(rec); err != nil {
		return "", err
	}
	return approved, nil
}

// ConsumeFor accepts an approved id once, and only for the caller that received it.
func (s *State) ConsumeFor(caller, approvalID, opID string, params map[string]string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	rec, ok := s.records.findApproved(approvalID, now)
	if !ok || rec.Status != statusApproved || rec.ApprovedID != approvalID {
		return false, nil
	}
	if rec.Caller != caller || rec.OperationID != opID || !maps.Equal(rec.Params, params) || expired(rec, now) {
		if expired(rec, now) {
			s.records.remove(rec)
		}
		return false, nil
	}
	claimed := false
	if s.tokens != nil {
		ok, err := s.tokens.consume(caller, approvalID, opID, params, now)
		if err != nil || !ok {
			return ok, err
		}
	} else {
		ok, err := s.records.claim(rec.ID)
		if err != nil || !ok {
			return ok, err
		}
		claimed = persistDir(s.records) != ""
	}
	rec.Status = statusConsumed
	if err := s.records.put(rec); err != nil {
		if claimed {
			_ = os.Remove(filepath.Join(persistDir(s.records), "confirmations", rec.ID+".claimed"))
		}
		return false, err
	}
	return true, nil
}

func (s *State) Pending(id string) *PendingConfirmation {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records.get(id)
	if !ok || rec.Status != statusPending {
		return nil
	}
	return &PendingConfirmation{ID: rec.ID, OperationID: rec.OperationID, Params: cloneParams(rec.Params)}
}

func expired(rec confirmation, now time.Time) bool {
	return rec.Expiry != 0 && !now.Before(time.Unix(rec.Expiry, 0))
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
		ttl = defaultApprovalTTL
	}
	return s.clock().Add(ttl)
}

func cloneParams(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	return maps.Clone(in)
}
