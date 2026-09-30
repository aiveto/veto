package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type storedToken struct {
	RefreshToken string    `json:"refresh_token,omitempty"`
	AccessToken  string    `json:"access_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Scopes       []string  `json:"scopes,omitempty"`
	Audience     string    `json:"audience,omitempty"`
	TokenURL     string    `json:"token_url,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
}

func DefaultTokenDir() string {
	if d, err := os.UserConfigDir(); err == nil && d != "" {
		return filepath.Join(d, "veto", "tokens")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".veto", "tokens")
	}
	return filepath.Join(home, ".veto", "tokens")
}

func writeToken(dir, scheme string, tok storedToken) error {
	path, err := tokenPath(dir, scheme)
	if err != nil {
		return err
	}
	if err := ensureDir(dir); err != nil {
		return err
	}
	raw, err := json.Marshal(tok)
	if err != nil {
		return fmt.Errorf("store token: %w", err)
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(dir, ".token-*")
	if err != nil {
		return fmt.Errorf("store token: %w", err)
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("store token: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("store token: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("store token: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("store token: %w", err)
	}
	ok = true
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("store token: %w", err)
	}
	return nil
}

// HasAccessToken reports a pasted or minted access token on disk.
func HasAccessToken(dir, scheme string) bool {
	tok, err := readToken(dir, scheme)
	return err == nil && tok.AccessToken != ""
}

// HasRefreshToken reports a stored refresh token.
func HasRefreshToken(dir, scheme string) bool {
	tok, err := readToken(dir, scheme)
	return err == nil && tok.RefreshToken != ""
}

func readToken(dir, scheme string) (storedToken, error) {
	path, err := tokenPath(dir, scheme)
	if err != nil {
		return storedToken{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return storedToken{}, err
	}
	var tok storedToken
	if err := json.Unmarshal(raw, &tok); err != nil {
		return storedToken{}, fmt.Errorf("read token: %w", err)
	}
	return tok, nil
}

func tokenPath(dir, scheme string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("token directory is unset")
	}
	if scheme == "" || strings.Contains(scheme, "..") {
		return "", fmt.Errorf("invalid auth scheme")
	}
	for _, r := range scheme {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return "", fmt.Errorf("invalid auth scheme")
		}
	}
	return filepath.Join(dir, scheme+".json"), nil
}

func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("token directory: %w", err)
	}
	st, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("token directory: %w", err)
	}
	if st.Mode().Perm()&0o077 == 0 {
		return nil
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("token directory: %w", err)
	}
	return nil
}
