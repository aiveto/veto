package policy

import (
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

func TestConfirmationKeepsItsOwnParams(t *testing.T) {
	s := NewState()
	params := map[string]string{"id": "1"}
	id, err := s.RequestConfirmation("orders.delete", params)
	require.NoError(t, err)
	params["id"] = "changed"
	got := s.Pending(id)
	require.NotNil(t, got)
	assert.Equal(t, "1", got.Params["id"])
	assert.False(t, consumed(t, s, id, "orders.delete", map[string]string{"id": "1"}))
	approved, err := s.Approve(id)
	require.NoError(t, err)
	assert.NotEqual(t, id, approved)
	assert.Nil(t, s.Pending(approved))
	assert.False(t, consumed(t, s, id, "orders.delete", map[string]string{"id": "1"}))
	assert.True(t, consumed(t, s, approved, "orders.delete", map[string]string{"id": "1"}))
	assert.False(t, consumed(t, s, approved, "orders.delete", map[string]string{"id": "1"}))
}

func TestSignedApprovalIsIssuedByApprove(t *testing.T) {
	params := map[string]string{"id": "1"}
	dir := t.TempDir()
	now := func() time.Time { return time.Unix(1_000, 0) }
	issued := withSigner(t, dir, []byte("secret"), now)
	pending, err := issued.RequestConfirmation("orders.delete", params)
	require.NoError(t, err)
	require.NotNil(t, issued.Pending(pending))
	assert.False(t, strings.HasPrefix(pending, "v1."))
	approved, err := issued.Approve(pending)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(approved, "v1."))
	assert.Nil(t, issued.Pending(approved))
	assert.False(t, consumed(t, issued, pending, "orders.delete", map[string]string{"id": "1"}))
	params["id"] = "2"
	other := withSigner(t, dir, []byte("secret"), now)
	assert.False(t, consumed(t, other, approved, "orders.delete", params))
	assert.True(t, consumed(t, other, approved, "orders.delete", map[string]string{"id": "1"}))
	assert.False(t, consumed(t, other, approved, "orders.delete", map[string]string{"id": "1"}))
	restarted := withSigner(t, dir, []byte("secret"), now)
	assert.False(t, consumed(t, restarted, approved, "orders.delete", map[string]string{"id": "1"}))
	other.now = func() time.Time { return time.Unix(1_000, 0).Add(time.Minute) }
	fresh, err := issued.RequestConfirmation("orders.delete", map[string]string{"id": "1"})
	require.NoError(t, err)
	freshID, err := issued.Approve(fresh)
	require.NoError(t, err)
	assert.False(t, consumed(t, other, freshID, "orders.delete", map[string]string{"id": "1"}))
	wrong := withSigner(t, dir, []byte("other"), now)
	again, err := issued.RequestConfirmation("orders.delete", map[string]string{"id": "1"})
	require.NoError(t, err)
	token, err := issued.Approve(again)
	require.NoError(t, err)
	assert.False(t, consumed(t, wrong, token, "orders.delete", map[string]string{"id": "1"}))
}

func TestApproveOnAnotherStateIsTheOnlyWayToRun(t *testing.T) {
	dir := t.TempDir()
	caller := NewState()
	caller.SetNonceDir(dir)
	pending, err := caller.RequestConfirmation("orders.delete", map[string]string{"id": "9"})
	require.NoError(t, err)
	assert.False(t, consumed(t, caller, pending, "orders.delete", map[string]string{"id": "9"}))
	approver := NewState()
	approver.SetNonceDir(dir)
	approved, err := approver.Approve(pending)
	require.NoError(t, err)
	assert.NotEqual(t, pending, approved)
	assert.False(t, consumed(t, caller, pending, "orders.delete", map[string]string{"id": "9"}))
	assert.True(t, consumed(t, caller, approved, "orders.delete", map[string]string{"id": "9"}))
	assert.False(t, consumed(t, caller, approved, "orders.delete", map[string]string{"id": "9"}))
}

func TestRequestConfirmationReturnsTheStoreError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confirmations"), []byte("not-a-dir"), 0o600))
	s := NewState()
	s.SetNonceDir(dir)
	id, err := s.RequestConfirmation("orders.delete", map[string]string{"id": "1"})
	require.Error(t, err)
	assert.Empty(t, id)
	assert.Nil(t, s.Pending(id))
	_, approveErr := s.Approve(id)
	require.Error(t, approveErr)
}

func TestCallersDoNotShareApprovalIDsOrTokens(t *testing.T) {
	dir := t.TempDir()
	now := func() time.Time { return time.Unix(1_000, 0) }
	s := withSigner(t, dir, []byte("secret"), now)
	params := map[string]string{"id": "1"}
	ada, err := s.RequestFor("ada", "orders.delete", params)
	require.NoError(t, err)
	grace, err := s.RequestFor("grace", "orders.delete", params)
	require.NoError(t, err)
	assert.NotEqual(t, ada, grace)
	adaToken, err := s.Approve(ada)
	require.NoError(t, err)
	graceToken, err := s.Approve(grace)
	require.NoError(t, err)
	assert.NotEqual(t, adaToken, graceToken)
	assert.False(t, consumedFor(t, s, "grace", ada, "orders.delete", params))
	assert.False(t, consumedFor(t, s, "grace", adaToken, "orders.delete", params))
	assert.False(t, consumedFor(t, s, "ada", graceToken, "orders.delete", params))
	assert.False(t, consumedFor(t, s, "", adaToken, "orders.delete", params))
	assert.True(t, consumedFor(t, s, "ada", adaToken, "orders.delete", params))
	assert.False(t, consumedFor(t, s, "ada", adaToken, "orders.delete", params))
	assert.True(t, consumedFor(t, s, "grace", graceToken, "orders.delete", params))
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

func TestUnsignedApprovalIsOneUseAcrossStates(t *testing.T) {
	params := map[string]string{"id": "1"}
	for round := 0; round < 20; round++ {
		dir := t.TempDir()
		issued := NewState()
		issued.SetNonceDir(dir)
		pending, err := issued.RequestConfirmation("orders.delete", params)
		require.NoError(t, err)
		approved, err := issued.Approve(pending)
		require.NoError(t, err)
		var wins atomic.Int32
		var wg sync.WaitGroup
		for range 24 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				other := NewState()
				other.SetNonceDir(dir)
				ok, err := other.ConsumeFor("", approved, "orders.delete", params)
				if err == nil && ok {
					wins.Add(1)
				}
			}()
		}
		wg.Wait()
		assert.Equal(t, int32(1), wins.Load(), "round %d", round)
	}
}

func TestUnsignedApprovalIsOneUseAcrossProcesses(t *testing.T) {
	if os.Getenv("VETO_CLAIM_CHILD") == "1" {
		s := NewState()
		s.SetNonceDir(os.Getenv("VETO_CLAIM_DIR"))
		ok, err := s.ConsumeFor("", os.Getenv("VETO_CLAIM_ID"), "orders.delete", map[string]string{"id": "1"})
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
	pending, err := issued.RequestConfirmation("orders.delete", map[string]string{"id": "1"})
	require.NoError(t, err)
	approved, err := issued.Approve(pending)
	require.NoError(t, err)

	var wins atomic.Int32
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestUnsignedApprovalIsOneUseAcrossProcesses$", "-test.count=1")
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
		}()
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

func consumed(t *testing.T, s *State, id, op string, params map[string]string) bool {
	t.Helper()
	return consumedFor(t, s, "", id, op, params)
}

func consumedFor(t *testing.T, s *State, caller, id, op string, params map[string]string) bool {
	t.Helper()
	ok, err := s.ConsumeFor(caller, id, op, params)
	require.NoError(t, err)
	return ok
}
