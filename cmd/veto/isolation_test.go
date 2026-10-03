package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "veto-test-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("VETO_APPROVAL_NONCE_DIR", filepath.Join(dir, "approvals")); err != nil {
		panic(err)
	}
	if err := os.Setenv("VETO_TOKEN_DIR", filepath.Join(dir, "tokens")); err != nil {
		panic(err)
	}
	if err := os.Unsetenv("VETO_APPROVAL_SECRET"); err != nil {
		panic(err)
	}
	if err := os.Unsetenv("VETO_APPROVAL_STORE"); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestApprovalFilesStayOutOfTheUserDir(t *testing.T) {
	user, err := policy.DefaultApprovalDir()
	require.NoError(t, err)
	before := dirNames(t, user)
	state := policy.NewState()
	require.NoError(t, applyApprovalEnv(state))
	_, err = state.RequestFor(t.Context(), "", "orders.delete", map[string]string{"id": "123"})
	require.NoError(t, err)
	assert.Equal(t, before, dirNames(t, user))
	temp := os.Getenv("VETO_APPROVAL_NONCE_DIR")
	assert.NotEqual(t, user, temp)
	assert.NotEmpty(t, dirNames(t, temp))
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
