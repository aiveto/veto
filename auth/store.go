package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/aiveto/veto/internal/atomicfile"
)

type storedToken struct {
	RefreshToken string            `json:"refresh_token,omitempty"`
	AccessToken  string            `json:"access_token,omitempty"`
	ExpiresAt    time.Time         `json:"expires_at,omitzero"`
	Scopes       []string          `json:"scopes,omitempty"`
	Audience     string            `json:"audience,omitempty"`
	TokenURL     string            `json:"token_url,omitempty"`
	ClientID     string            `json:"client_id,omitempty"`
	Fields       map[string]string `json:"fields,omitempty"`
	Issuer       string            `json:"issuer,omitempty"`
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
	raw, err := jsonv2.Marshal(tok)
	if err != nil {
		return fmt.Errorf("store token: %w", err)
	}
	if err := atomicfile.Write(path, append(raw, '\n'), 0o600); err != nil {
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
	if err := jsonv2.Unmarshal(raw, &tok); err != nil {
		return storedToken{}, fmt.Errorf("read token: %w", err)
	}
	return tok, nil
}

func authFieldName(s Scheme) string {
	if s.AuthToken != "" {
		return s.AuthToken
	}
	return "access_token"
}

func userFieldName(s Scheme) string {
	if s.UserToken != "" {
		return s.UserToken
	}
	return "id_token"
}

func tokenField(tok storedToken, name string) string {
	if name == "" {
		return ""
	}
	if tok.Fields != nil && tok.Fields[name] != "" {
		return tok.Fields[name]
	}
	switch name {
	case "access_token":
		return tok.AccessToken
	case "refresh_token":
		return tok.RefreshToken
	default:
		return ""
	}
}

func absorb(prev storedToken, tok tokenResponse, s Scheme, scopes []string, exp time.Time, tokenURL, clientID string) storedToken {
	fields := map[string]string{}
	for k, v := range prev.Fields {
		if v != "" {
			fields[k] = v
		}
	}
	for k, v := range tok.Fields {
		if v != "" {
			fields[k] = v
		}
	}
	refresh := tok.RefreshToken
	if refresh == "" {
		refresh = prev.RefreshToken
	}
	if refresh != "" {
		fields["refresh_token"] = refresh
	}
	if tok.AccessToken != "" && fields["access_token"] == "" {
		fields["access_token"] = tok.AccessToken
	}
	name := authFieldName(s)
	access := fields[name]
	if access == "" && name == "access_token" {
		access = tok.AccessToken
		if access == "" {
			access = prev.AccessToken
		}
	}
	if access != "" {
		fields[name] = access
	}
	issuer := s.Issuer
	if issuer == "" {
		issuer = prev.Issuer
	}
	if tokenURL == "" {
		tokenURL = prev.TokenURL
	}
	if clientID == "" {
		clientID = prev.ClientID
	}
	audience := s.Audience
	if audience == "" {
		audience = prev.Audience
	}
	return storedToken{
		RefreshToken: refresh,
		AccessToken:  access,
		ExpiresAt:    exp,
		Scopes:       scopes,
		Audience:     audience,
		TokenURL:     tokenURL,
		ClientID:     clientID,
		Fields:       fields,
		Issuer:       issuer,
	}
}

func tokenPath(dir, scheme string) (string, error) {
	if dir == "" {
		return "", errors.New("token directory is unset")
	}
	if scheme == "" || strings.Contains(scheme, "..") {
		return "", errors.New("invalid auth scheme")
	}
	for _, r := range scheme {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return "", errors.New("invalid auth scheme")
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
