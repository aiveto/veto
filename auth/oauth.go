package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const expirySkew = 30 * time.Second

type (
	Endpoints struct {
		Authorization string
		Token         string
		Device        string
	}

	tokenResponse struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
		Error        string `json:"error"`
	}
)

func Discover(ctx context.Context, client *http.Client, issuer string) (Endpoints, error) {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	if issuer == "" {
		return Endpoints{}, fmt.Errorf("issuer is unset")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return Endpoints{}, fmt.Errorf("discover issuer: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Endpoints{}, fmt.Errorf("discover issuer: request failed")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Endpoints{}, fmt.Errorf("discover issuer: request failed")
	}
	if resp.StatusCode != http.StatusOK {
		return Endpoints{}, fmt.Errorf("discover issuer: status %d", resp.StatusCode)
	}
	var doc struct {
		Authorization string `json:"authorization_endpoint"`
		Token         string `json:"token_endpoint"`
		Device        string `json:"device_authorization_endpoint"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return Endpoints{}, fmt.Errorf("discover issuer: invalid document")
	}
	if doc.Authorization == "" || doc.Token == "" {
		return Endpoints{}, fmt.Errorf("discover issuer: endpoints missing")
	}
	return Endpoints{Authorization: doc.Authorization, Token: doc.Token, Device: doc.Device}, nil
}

func fillEndpoints(ctx context.Context, client *http.Client, scheme *Scheme) error {
	if scheme.Issuer == "" {
		return nil
	}
	needAuth := scheme.AuthorizationURL == ""
	needToken := scheme.TokenURL == ""
	needDevice := scheme.DeviceAuthorizationURL == ""
	if !needAuth && !needToken && !needDevice {
		return nil
	}
	ep, err := Discover(ctx, client, scheme.Issuer)
	if err != nil {
		return err
	}
	if needAuth {
		scheme.AuthorizationURL = ep.Authorization
	}
	if needToken {
		scheme.TokenURL = ep.Token
	}
	if needDevice {
		scheme.DeviceAuthorizationURL = ep.Device
	}
	return nil
}

func postForm(ctx context.Context, client *http.Client, endpoint string, form url.Values, secrets []string) (tokenResponse, error) {
	if endpoint == "" {
		return tokenResponse{}, fmt.Errorf("token url is unset")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, fmt.Errorf("token endpoint: request failed")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("token endpoint: %s", Redact(err.Error(), secrets, nil))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return tokenResponse{}, fmt.Errorf("token endpoint: request failed")
	}
	if resp.StatusCode != http.StatusOK {
		return tokenResponse{}, fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}
	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return tokenResponse{}, fmt.Errorf("token endpoint returned invalid JSON")
	}
	if tok.Error != "" || tok.AccessToken == "" {
		return tokenResponse{}, fmt.Errorf("token endpoint rejected the request")
	}
	return tok, nil
}

func grantedScopes(responseScope string, requested []string) ([]string, error) {
	got := requested
	if strings.TrimSpace(responseScope) != "" {
		got = strings.Fields(responseScope)
	}
	if !covers(got, requested) {
		return nil, fmt.Errorf("token scopes are narrower than the operation")
	}
	return append([]string(nil), got...), nil
}

func covers(have, need []string) bool {
	if len(need) == 0 {
		return true
	}
	set := map[string]bool{}
	for _, s := range have {
		set[s] = true
	}
	for _, s := range need {
		if !set[s] {
			return false
		}
	}
	return true
}

func expiryFrom(now time.Time, expiresIn int) time.Time {
	if expiresIn <= 0 {
		return time.Time{}
	}
	return now.Add(time.Duration(expiresIn) * time.Second)
}

func fresh(exp, now time.Time) bool {
	if exp.IsZero() {
		return true
	}
	return now.Add(expirySkew).Before(exp)
}

func usable(exp, now time.Time) bool {
	if exp.IsZero() {
		return true
	}
	return now.Before(exp)
}

func pkce() (verifier, challenge string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("pkce: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func randomState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("login state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func scopeParam(scopes []string) string {
	return strings.Join(scopes, " ")
}
