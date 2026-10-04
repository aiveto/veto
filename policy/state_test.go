package policy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetSignerRejectsAnEmptySecret(t *testing.T) {
	s := NewState()
	require.Error(t, s.SetSigner(nil, 0))
	var missing *State
	require.Error(t, missing.SetSigner([]byte("secret"), 0))
}

func TestOpenDialsTheStoreURL(t *testing.T) {
	s := NewState()
	got := &captureStore{}
	require.NoError(t, s.Open(t.Context(), StoreOptions{
		URL: "redis://example",
		Dial: func(context.Context, string) (Store, error) {
			return got, nil
		},
	}))
	id, err := s.RequestFor(t.Context(), "ada", "orders.delete", map[string]string{"id": "1"})
	require.NoError(t, err)
	assert.Equal(t, 1, got.puts)
	pending := s.Pending(t.Context(), id)
	require.NotNil(t, pending)
	assert.Equal(t, "ada", pending.Caller)
}

func TestOpenRequiresADialerForAURL(t *testing.T) {
	s := NewState()
	require.Error(t, s.Open(t.Context(), StoreOptions{URL: "redis://example"}))
}

func TestSetStoreUsesTheGivenStore(t *testing.T) {
	s := NewState()
	got := &captureStore{}
	s.SetStore(got)
	_, err := s.RequestFor(t.Context(), "", "orders.delete", map[string]string{"id": "1"})
	require.NoError(t, err)
	assert.Equal(t, 1, got.puts)
}

func TestSetSignerKeepsANonMemoryStore(t *testing.T) {
	s := NewState()
	got := &captureStore{}
	s.SetStore(got)
	require.NoError(t, s.SetSigner([]byte("secret"), time.Minute))
	_, err := s.RequestFor(t.Context(), "", "orders.delete", map[string]string{"id": "1"})
	require.NoError(t, err)
	assert.Equal(t, 1, got.puts)
}

type captureStore struct {
	Memory
	puts int
}

func (c *captureStore) Put(ctx context.Context, rec Record) error {
	c.puts++
	return c.Memory.Put(ctx, rec)
}

func TestApproveUnknownIsASentinel(t *testing.T) {
	s := NewState()
	_, err := s.Approve(t.Context(), "missing")
	require.ErrorIs(t, err, ErrUnknownApproval)
}

func TestApproveReturnsAStoreError(t *testing.T) {
	want := errors.New("store down")
	s := NewState()
	s.SetStore(&errGetStore{err: want})
	_, err := s.Approve(t.Context(), "pending-1")
	require.ErrorIs(t, err, want)
	require.NotErrorIs(t, err, ErrUnknownApproval)
}

func TestDistinctApprovalsDoNotShareOneStoreLock(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	s := NewState()
	s.SetStore(&gateStore{started: started, release: release})
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for _, caller := range []string{"ada", "grace"} {
		wg.Go(func() {
			_, err := s.RequestFor(t.Context(), caller, "orders.delete", map[string]string{"id": caller})
			errCh <- err
		})
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first put did not start")
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("unrelated put waited on the other store call")
	}
	close(release)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
}

type errGetStore struct {
	Memory
	err error
}

func (s *errGetStore) Get(context.Context, string) (Record, bool, error) {
	return Record{}, false, s.err
}

type gateStore struct {
	Memory
	mu      sync.Mutex
	started chan struct{}
	release chan struct{}
}

func (s *gateStore) Put(ctx context.Context, rec Record) error {
	s.started <- struct{}{}
	<-s.release
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Memory.Put(ctx, rec)
}

func TestConcurrentMemoryRequestsDoNotRace(t *testing.T) {
	s := NewState()
	var wg sync.WaitGroup
	errCh := make(chan error, 32)
	for i := range 32 {
		wg.Go(func() {
			id, err := s.RequestFor(t.Context(), "ada", "orders.delete", map[string]string{"id": string(rune('a' + i%26))})
			if err != nil {
				errCh <- err
				return
			}
			if s.Pending(t.Context(), id) == nil {
				errCh <- errors.New("missing pending")
			}
		})
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
}

func TestConfirmationKeepsItsOwnParams(t *testing.T) {
	s := NewState()
	params := map[string]string{"id": "1"}
	id, err := s.RequestFor(t.Context(), "", "orders.delete", params)
	require.NoError(t, err)
	params["id"] = "changed"
	got := s.Pending(t.Context(), id)
	require.NotNil(t, got)
	assert.Equal(t, "1", got.Params["id"])
	assert.False(t, consumed(t, s, id, map[string]string{"id": "1"}))
	approved, err := s.Approve(t.Context(), id)
	require.NoError(t, err)
	assert.NotEqual(t, id, approved)
	assert.Nil(t, s.Pending(t.Context(), approved))
	assert.False(t, consumed(t, s, id, map[string]string{"id": "1"}))
	assert.True(t, consumed(t, s, approved, map[string]string{"id": "1"}))
	assert.False(t, consumed(t, s, approved, map[string]string{"id": "1"}))
}

func TestSignedApprovalIsIssuedByApprove(t *testing.T) {
	params := map[string]string{"id": "1"}
	dir := t.TempDir()
	now := func() time.Time { return time.Unix(1_000, 0) }
	issued := withSigner(t, dir, []byte("secret"), now)
	pending, err := issued.RequestFor(t.Context(), "", "orders.delete", params)
	require.NoError(t, err)
	require.NotNil(t, issued.Pending(t.Context(), pending))
	assert.False(t, strings.HasPrefix(pending, "v1."))
	approved, err := issued.Approve(t.Context(), pending)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(approved, "v1."))
	assert.Nil(t, issued.Pending(t.Context(), approved))
	assert.False(t, consumed(t, issued, pending, map[string]string{"id": "1"}))
	params["id"] = "2"
	other := withSigner(t, dir, []byte("secret"), now)
	assert.False(t, consumed(t, other, approved, params))
	assert.True(t, consumed(t, other, approved, map[string]string{"id": "1"}))
	assert.False(t, consumed(t, other, approved, map[string]string{"id": "1"}))
	restarted := withSigner(t, dir, []byte("secret"), now)
	assert.False(t, consumed(t, restarted, approved, map[string]string{"id": "1"}))
	other.now = func() time.Time { return time.Unix(1_000, 0).Add(time.Minute) }
	fresh, err := issued.RequestFor(t.Context(), "", "orders.delete", map[string]string{"id": "1"})
	require.NoError(t, err)
	freshID, err := issued.Approve(t.Context(), fresh)
	require.NoError(t, err)
	assert.False(t, consumed(t, other, freshID, map[string]string{"id": "1"}))
	wrong := withSigner(t, dir, []byte("other"), now)
	again, err := issued.RequestFor(t.Context(), "", "orders.delete", map[string]string{"id": "1"})
	require.NoError(t, err)
	token, err := issued.Approve(t.Context(), again)
	require.NoError(t, err)
	assert.False(t, consumed(t, wrong, token, map[string]string{"id": "1"}))
}

func TestApproveOnAnotherStateIsTheOnlyWayToRun(t *testing.T) {
	dir := t.TempDir()
	caller := NewState()
	caller.SetNonceDir(dir)
	pending, err := caller.RequestFor(t.Context(), "", "orders.delete", map[string]string{"id": "9"})
	require.NoError(t, err)
	assert.False(t, consumed(t, caller, pending, map[string]string{"id": "9"}))
	approver := NewState()
	approver.SetNonceDir(dir)
	approved, err := approver.Approve(t.Context(), pending)
	require.NoError(t, err)
	assert.NotEqual(t, pending, approved)
	assert.False(t, consumed(t, caller, pending, map[string]string{"id": "9"}))
	assert.True(t, consumed(t, caller, approved, map[string]string{"id": "9"}))
	assert.False(t, consumed(t, caller, approved, map[string]string{"id": "9"}))
}

func TestRequestConfirmationReturnsTheStoreError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confirmations"), []byte("not-a-dir"), 0o600))
	s := NewState()
	s.SetNonceDir(dir)
	id, err := s.RequestFor(t.Context(), "", "orders.delete", map[string]string{"id": "1"})
	require.Error(t, err)
	assert.Empty(t, id)
	assert.Nil(t, s.Pending(t.Context(), id))
	_, approveErr := s.Approve(t.Context(), id)
	require.Error(t, approveErr)
}

func TestCallersDoNotShareApprovalIDsOrTokens(t *testing.T) {
	dir := t.TempDir()
	now := func() time.Time { return time.Unix(1_000, 0) }
	s := withSigner(t, dir, []byte("secret"), now)
	params := map[string]string{"id": "1"}
	ada, err := s.RequestFor(t.Context(), "ada", "orders.delete", params)
	require.NoError(t, err)
	grace, err := s.RequestFor(t.Context(), "grace", "orders.delete", params)
	require.NoError(t, err)
	assert.NotEqual(t, ada, grace)
	adaToken, err := s.Approve(t.Context(), ada)
	require.NoError(t, err)
	graceToken, err := s.Approve(t.Context(), grace)
	require.NoError(t, err)
	assert.NotEqual(t, adaToken, graceToken)
	assert.False(t, consumedFor(t, s, "grace", ada, params))
	assert.False(t, consumedFor(t, s, "grace", adaToken, params))
	assert.False(t, consumedFor(t, s, "ada", graceToken, params))
	assert.False(t, consumedFor(t, s, "", adaToken, params))
	assert.True(t, consumedFor(t, s, "ada", adaToken, params))
	assert.False(t, consumedFor(t, s, "ada", adaToken, params))
	assert.True(t, consumedFor(t, s, "grace", graceToken, params))
}

func TestDefaultNonceDirFollowsTheSecret(t *testing.T) {
	a, err := defaultNonceDir([]byte("secret"))
	require.NoError(t, err)
	b, err := defaultNonceDir([]byte("secret"))
	require.NoError(t, err)
	c, err := defaultNonceDir([]byte("other"))
	require.NoError(t, err)
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, c)
	assert.Contains(t, a, "approval-nonces")
}

func TestSignedApprovalExpiresAndIsSwept(t *testing.T) {
	dir := t.TempDir()
	when := time.Unix(1_700_000_000, 0)
	s := NewState()
	s.SetNonceDir(dir)
	require.NoError(t, s.SetSigner([]byte("secret"), time.Minute))
	s.now = func() time.Time { return when }
	params := map[string]string{"id": "1"}
	cases := []struct {
		name string
		age  time.Duration
		want bool
	}{
		{name: "inside ttl", age: 30 * time.Second, want: true},
		{name: "at ttl", age: time.Minute, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s.now = func() time.Time { return when }
			pending, err := s.RequestFor(t.Context(), "", "orders.delete", params)
			require.NoError(t, err)
			approved, err := s.Approve(t.Context(), pending)
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(approved, "v1."))
			s.now = func() time.Time { return when.Add(tc.age) }
			ok, err := s.ConsumeFor(t.Context(), "", approved, "orders.delete", params)
			require.NoError(t, err)
			assert.Equal(t, tc.want, ok)
			if tc.want {
				return
			}
			_, err = os.Stat(filepath.Join(dir, "confirmations", pending+".json"))
			assert.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func TestSignedApprovalIsOneUseAcrossStates(t *testing.T) {
	params := map[string]string{"id": "1"}
	now := func() time.Time { return time.Unix(1_700_000_000, 0) }
	for round := range 20 {
		dir := t.TempDir()
		issued := withSigner(t, dir, []byte("secret"), now)
		pending, err := issued.RequestFor(t.Context(), "", "orders.delete", params)
		require.NoError(t, err)
		approved, err := issued.Approve(t.Context(), pending)
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(approved, "v1."))
		var wins atomic.Int32
		var wg sync.WaitGroup
		for range 24 {
			wg.Go(func() {
				other := withSigner(t, dir, []byte("secret"), now)
				ok, err := other.ConsumeFor(t.Context(), "", approved, "orders.delete", params)
				if err == nil && ok {
					wins.Add(1)
				}
			})
		}
		wg.Wait()
		assert.Equal(t, int32(1), wins.Load(), "round %d", round)
	}
}

func TestUnsignedApprovalExpiresAndIsSwept(t *testing.T) {
	dir := t.TempDir()
	when := time.Unix(1_700_000_000, 0)
	s := NewState()
	s.SetNonceDir(dir)
	s.now = func() time.Time { return when }
	params := map[string]string{"id": "1"}
	pending, err := s.RequestFor(t.Context(), "", "orders.delete", params)
	require.NoError(t, err)
	raw, err := os.ReadFile(filepath.Join(dir, "confirmations", pending+".json"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"expiry":`)
	approved, err := s.Approve(t.Context(), pending)
	require.NoError(t, err)
	s.now = func() time.Time { return when.Add(16 * time.Minute) }
	ok, err := s.ConsumeFor(t.Context(), "", approved, "orders.delete", params)
	require.NoError(t, err)
	assert.False(t, ok)
	_, err = os.Stat(filepath.Join(dir, "confirmations", pending+".json"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestUnsignedApprovalIsOneUseAcrossStates(t *testing.T) {
	params := map[string]string{"id": "1"}
	for round := range 20 {
		dir := t.TempDir()
		issued := NewState()
		issued.SetNonceDir(dir)
		pending, err := issued.RequestFor(t.Context(), "", "orders.delete", params)
		require.NoError(t, err)
		approved, err := issued.Approve(t.Context(), pending)
		require.NoError(t, err)
		var wins atomic.Int32
		var wg sync.WaitGroup
		for range 24 {
			wg.Go(func() {
				other := NewState()
				other.SetNonceDir(dir)
				ok, err := other.ConsumeFor(t.Context(), "", approved, "orders.delete", params)
				if err == nil && ok {
					wins.Add(1)
				}
			})
		}
		wg.Wait()
		assert.Equal(t, int32(1), wins.Load(), "round %d", round)
	}
}

func TestUnsignedApprovalIsOneUseAcrossProcesses(t *testing.T) {
	if os.Getenv("VETO_CLAIM_CHILD") == "1" {
		s := NewState()
		s.SetNonceDir(os.Getenv("VETO_CLAIM_DIR"))
		ok, err := s.ConsumeFor(context.Background(), "", os.Getenv("VETO_CLAIM_ID"), "orders.delete", map[string]string{"id": "1"})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if ok {
			os.Exit(0)
		}
		os.Exit(3)
	}
	dir := t.TempDir()
	issued := NewState()
	issued.SetNonceDir(dir)
	pending, err := issued.RequestFor(t.Context(), "", "orders.delete", map[string]string{"id": "1"})
	require.NoError(t, err)
	approved, err := issued.Approve(t.Context(), pending)
	require.NoError(t, err)

	var wins atomic.Int32
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestUnsignedApprovalIsOneUseAcrossProcesses$", "-test.count=1")
			cmd.Env = append(os.Environ(), "VETO_CLAIM_CHILD=1", "VETO_CLAIM_DIR="+dir, "VETO_CLAIM_ID="+approved)
			err := cmd.Run()
			if err == nil {
				wins.Add(1)
				return
			}
			var exit *exec.ExitError
			if errors.As(err, &exit) && exit.ExitCode() == 3 {
				return
			}
			errCh <- err
		})
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), wins.Load())
}

func withSigner(t *testing.T, dir string, secret []byte, now func() time.Time) *State {
	t.Helper()
	s := NewState()
	s.SetNonceDir(dir)
	require.NoError(t, s.SetSigner(secret, time.Minute))
	if now != nil {
		s.now = now
	}
	return s
}

func consumed(t *testing.T, s *State, id string, params map[string]string) bool {
	t.Helper()
	return consumedFor(t, s, "", id, params)
}

func consumedFor(t *testing.T, s *State, caller, id string, params map[string]string) bool {
	t.Helper()
	ok, err := s.ConsumeFor(t.Context(), caller, id, "orders.delete", params)
	require.NoError(t, err)
	return ok
}
