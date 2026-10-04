package auth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadTokenAcceptsAPriorFile(t *testing.T) {
	dir := t.TempDir()
	raw := []byte(`{"access_token":"t","scopes":null,"fields":null}` + "\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "login.json"), raw, 0o600))
	tok, err := readToken(dir, "login")
	require.NoError(t, err)
	assert.Equal(t, "t", tok.AccessToken)
	assert.Empty(t, tok.Scopes)
}

func TestWriteTokenUsesV2(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeToken(dir, "login", storedToken{AccessToken: "t", Scopes: nil}))
	raw, err := os.ReadFile(filepath.Join(dir, "login.json"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), `"scopes":null`)
	tok, err := readToken(dir, "login")
	require.NoError(t, err)
	assert.Equal(t, "t", tok.AccessToken)
	assert.Empty(t, tok.Scopes)
}
