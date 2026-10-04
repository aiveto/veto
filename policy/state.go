package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/aiveto/veto/jsonopts"
	"github.com/google/uuid"
)

const (
	// StatusPending is a recorded call that has not been approved.
	StatusPending = "pending"
	// StatusApproved is a yes that ConsumeFor accepts once.
	StatusApproved = "approved"
	// StatusConsumed is a yes that already ran.
	StatusConsumed     = "consumed"
	defaultApprovalTTL = 15 * time.Minute
)

// ErrUnknownApproval is an id approve cannot find, or one that is already consumed or expired.
var ErrUnknownApproval = errors.New("unknown approval")

type (
	// State is the confirmation use case. Memory is the store. SetStore, SetNonceDir, and SetSigner attach adapters.
	State struct {
		mu      sync.Mutex
		held    sync.Map
		records Store
		tokens  signer
		ttl     time.Duration
		now     func() time.Time
		json    jsonopts.Set
	}

	// StoreOptions attaches a store, a files dir, and a signer. Dial opens a URL.
	StoreOptions struct {
		URL    string
		Dir    string
		Secret []byte
		TTL    time.Duration
		JSON   jsonopts.Set
		Dial   func(context.Context, string) (Store, error)
	}
)

// NewState uses Memory.
func NewState() *State {
	return &State{records: &Memory{}, now: time.Now}
}

// DefaultApprovalDir is the user-config path serve and approve share.
func DefaultApprovalDir() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("approval dir: %w", err)
	}
	return filepath.Join(root, "veto", "approvals"), nil
}

// Open points state at the store, files dir, and signer. Empty URL keeps Files or Memory.
func (s *State) Open(ctx context.Context, opt StoreOptions) error {
	if s == nil {
		return errors.New("missing approval state")
	}
	s.json = opt.JSON
	if opt.URL != "" {
		if opt.Dial == nil {
			return errors.New("approval store dial is unset")
		}
		st, err := opt.Dial(ctx, opt.URL)
		if err != nil {
			return err
		}
		s.SetStore(st)
	} else if opt.Dir != "" {
		s.SetStore(&Files{Dir: opt.Dir, JSON: opt.JSON})
	}
	if len(opt.Secret) == 0 {
		return nil
	}
	return s.SetSigner(opt.Secret, opt.TTL)
}

// SetStore replaces the confirmation store. Nil uses Memory.
func (s *State) SetStore(store Store) {
	if s == nil {
		return
	}
	if store == nil {
		store = &Memory{}
	}
	s.mu.Lock()
	s.records = store
	s.mu.Unlock()
}

// SetNonceDir uses Files in dir.
func (s *State) SetNonceDir(dir string) {
	s.SetStore(&Files{Dir: dir})
}

// SetSigner attaches HMAC. Empty secret is an error. Unset ttl is 15 minutes.
func (s *State) SetSigner(secret []byte, ttl time.Duration) error {
	if s == nil {
		return errors.New("missing approval state")
	}
	if len(secret) == 0 {
		return errors.New("approval secret is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records.(*Memory); ok {
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
		s.records = &Files{Dir: dir, JSON: s.json}
	}
	s.tokens = hmacSigner{secret: append([]byte(nil), secret...)}
	s.ttl = ttl
	if s.ttl <= 0 {
		s.ttl = defaultApprovalTTL
	}
	if s.now == nil {
		s.now = time.Now
	}
	return nil
}

func persistDir(records Store) string {
	f, ok := records.(*Files)
	if !ok {
		return ""
	}
	return f.Dir
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
func (s *State) RequestFor(ctx context.Context, caller, opID string, params map[string]string) (string, error) {
	store, _, deadline := s.view()
	id := uuid.NewString()
	rec := Record{
		ID:          id,
		OperationID: opID,
		Params:      cloneParams(params),
		Status:      StatusPending,
		Caller:      caller,
		Expiry:      deadline().Unix(),
	}
	if err := store.Put(ctx, rec); err != nil {
		store.Remove(ctx, rec)
		return "", err
	}
	return id, nil
}

// Approve records a separate decision. The returned id is what a later invoke accepts once.
func (s *State) Approve(ctx context.Context, id string) (string, error) {
	unlock := s.lockID(id)
	defer unlock()
	store, tokens, deadline := s.view()
	rec, ok, err := store.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if !ok || rec.Status == StatusConsumed || expired(rec, s.clock()) {
		if ok && expired(rec, s.clock()) {
			store.Remove(ctx, rec)
		}
		return "", ErrUnknownApproval
	}
	if rec.Status == StatusApproved && rec.ApprovedID != "" {
		return rec.ApprovedID, nil
	}
	if rec.Status != StatusPending {
		return "", ErrUnknownApproval
	}
	approved := uuid.NewString()
	if tokens != nil {
		exp := deadline()
		if rec.Expiry != 0 {
			exp = time.Unix(rec.Expiry, 0)
		}
		approved = tokens.sign(rec.Caller, rec.OperationID, rec.Params, exp)
	}
	rec.Status = StatusApproved
	rec.ApprovedID = approved
	if err := store.Put(ctx, rec); err != nil {
		return "", err
	}
	return approved, nil
}

// ConsumeFor accepts an approved id once, and only for the caller that received it.
func (s *State) ConsumeFor(ctx context.Context, caller, approvalID, opID string, params map[string]string) (bool, error) {
	unlock := s.lockID(approvalID)
	defer unlock()
	store, tokens, _ := s.view()
	now := s.clock()
	rec, ok, err := store.FindApproved(ctx, approvalID)
	if err != nil {
		return false, err
	}
	if !ok || rec.Status != StatusApproved || rec.ApprovedID != approvalID {
		return false, nil
	}
	if rec.Caller != caller || rec.OperationID != opID || !maps.Equal(rec.Params, params) || expired(rec, now) {
		if expired(rec, now) {
			store.Remove(ctx, rec)
		}
		return false, nil
	}
	if tokens != nil {
		ok, err := tokens.consume(caller, approvalID, opID, params, now)
		if err != nil || !ok {
			return ok, err
		}
	}
	ok, err = store.Claim(ctx, rec.ID)
	if err != nil || !ok {
		return ok, err
	}
	rec.Status = StatusConsumed
	if err := store.Put(ctx, rec); err != nil {
		return false, err
	}
	return true, nil
}

func (s *State) Pending(ctx context.Context, id string) *PendingConfirmation {
	store, _, _ := s.view()
	rec, ok, err := store.Get(ctx, id)
	if err != nil || !ok || rec.Status != StatusPending {
		return nil
	}
	return &PendingConfirmation{ID: rec.ID, OperationID: rec.OperationID, Params: cloneParams(rec.Params), Caller: rec.Caller}
}

func (s *State) view() (Store, signer, func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	store := s.records
	if store == nil {
		store = &Memory{}
		s.records = store
	}
	return store, s.tokens, s.deadline
}

func (s *State) lockID(id string) func() {
	v, _ := s.held.LoadOrStore(id, &sync.Mutex{})
	m, ok := v.(*sync.Mutex)
	if !ok {
		m = &sync.Mutex{}
		s.held.Store(id, m)
	}
	m.Lock()
	return m.Unlock
}

func expired(rec Record, now time.Time) bool {
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
