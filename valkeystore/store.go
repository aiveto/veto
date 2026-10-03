// Package valkeystore is a policy.Store for more than one veto process.
package valkeystore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aiveto/veto/policy"
	"github.com/valkey-io/valkey-go"
)

const (
	recPrefix      = "veto:approval:rec:"
	approvedPrefix = "veto:approval:ok:"
	claimPrefix    = "veto:approval:claim:"
	claimTTL       = 24 * time.Hour
)

// Store keeps confirmation records in Valkey or Redis. Claim is SET NX.
type Store struct {
	c valkey.Client
}

// Dial opens a RESP server. url is a redis://, valkey://, or host:port.
func Dial(url string) (*Store, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, errors.New("approval store url is unset")
	}
	if !strings.Contains(url, "://") {
		url = "redis://" + url
	}
	opt, err := valkey.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("approval store: %w", err)
	}
	opt.DisableCache = true
	c, err := valkey.NewClient(opt)
	if err != nil {
		return nil, fmt.Errorf("approval store: %w", err)
	}
	if err := c.Do(context.Background(), c.B().Ping().Build()).Error(); err != nil {
		c.Close()
		return nil, fmt.Errorf("approval store: %w", err)
	}
	return &Store{c: c}, nil
}

func (s *Store) Put(rec policy.Record) error {
	ctx := context.Background()
	body, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("approval: %w", err)
	}
	ttl := recordTTL(rec)
	if err := s.set(ctx, recPrefix+rec.ID, string(body), ttl); err != nil {
		return err
	}
	if rec.ApprovedID != "" && rec.Status == "approved" {
		if err := s.set(ctx, approvedPrefix+rec.ApprovedID, rec.ID, ttl); err != nil {
			return err
		}
	}
	if rec.Status == "consumed" && rec.ApprovedID != "" {
		_ = s.c.Do(ctx, s.c.B().Del().Key(approvedPrefix+rec.ApprovedID).Build()).Error()
	}
	return nil
}

func (s *Store) Get(id string) (policy.Record, bool) {
	return s.load(recPrefix + id)
}

func (s *Store) FindApproved(approvedID string) (policy.Record, bool) {
	id, err := s.c.Do(context.Background(), s.c.B().Get().Key(approvedPrefix+approvedID).Build()).ToString()
	if err != nil {
		return policy.Record{}, false
	}
	rec, ok := s.load(recPrefix + id)
	if !ok || rec.ApprovedID != approvedID || rec.Status != "approved" {
		return policy.Record{}, false
	}
	return rec, true
}

func (s *Store) Claim(id string) (bool, error) {
	resp := s.c.Do(context.Background(), s.c.B().Set().Key(claimPrefix+id).Value("1").Nx().ExSeconds(ttlSeconds(claimTTL)).Build())
	if err := resp.Error(); err != nil {
		if valkey.IsValkeyNil(err) {
			return false, nil
		}
		return false, fmt.Errorf("approval store: %w", err)
	}
	return true, nil
}

func (s *Store) Remove(rec policy.Record) {
	keys := []string{recPrefix + rec.ID, claimPrefix + rec.ID}
	if rec.ApprovedID != "" {
		keys = append(keys, approvedPrefix+rec.ApprovedID)
	}
	_ = s.c.Do(context.Background(), s.c.B().Del().Key(keys...).Build()).Error()
}

func (s *Store) Close() {
	if s == nil || s.c == nil {
		return
	}
	s.c.Close()
}

func (s *Store) set(ctx context.Context, key, val string, ttl time.Duration) error {
	if err := s.c.Do(ctx, s.c.B().Set().Key(key).Value(val).ExSeconds(ttlSeconds(ttl)).Build()).Error(); err != nil {
		return fmt.Errorf("approval store: %w", err)
	}
	return nil
}

func (s *Store) load(key string) (policy.Record, bool) {
	raw, err := s.c.Do(context.Background(), s.c.B().Get().Key(key).Build()).AsBytes()
	if err != nil {
		return policy.Record{}, false
	}
	var rec policy.Record
	if json.Unmarshal(raw, &rec) != nil || rec.ID == "" {
		return policy.Record{}, false
	}
	return rec, true
}

func recordTTL(rec policy.Record) time.Duration {
	if rec.Expiry == 0 {
		return 24 * time.Hour
	}
	d := time.Until(time.Unix(rec.Expiry, 0))
	if d <= 0 {
		return time.Minute
	}
	return d
}

func ttlSeconds(d time.Duration) int64 {
	s := int64(d / time.Second)
	if s < 1 {
		return 1
	}
	return s
}
