package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

const expirySkew = 30 * time.Second

type (
	Endpoints struct {
		Authorization string
		Token         string
		Device        string
	}

	tokenResponse struct {
		AccessToken  string            `json:"access_token"`
		RefreshToken string            `json:"refresh_token"`
		ExpiresIn    int               `json:"expires_in"`
		Scope        string            `json:"scope"`
		Error        string            `json:"error"`
		Fields       map[string]string `json:"-"`
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
	tok, err := decodeTokenResponse(body)
	if err != nil {
		return tokenResponse{}, err
	}
	if tok.Error != "" || !tok.hasToken() {
		return tokenResponse{}, fmt.Errorf("token endpoint rejected the request")
	}
	return tok, nil
}

func decodeTokenResponse(body []byte) (tokenResponse, error) {
	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return tokenResponse{}, fmt.Errorf("token endpoint returned invalid JSON")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return tokenResponse{}, fmt.Errorf("token endpoint returned invalid JSON")
	}
	tok.Fields = map[string]string{}
	for k, v := range raw {
		var s string
		if json.Unmarshal(v, &s) == nil && s != "" {
			tok.Fields[k] = s
		}
	}
	if tok.AccessToken != "" && tok.Fields["access_token"] == "" {
		tok.Fields["access_token"] = tok.AccessToken
	}
	if tok.RefreshToken != "" && tok.Fields["refresh_token"] == "" {
		tok.Fields["refresh_token"] = tok.RefreshToken
	}
	return tok, nil
}

func (t tokenResponse) hasToken() bool {
	if t.AccessToken != "" {
		return true
	}
	for k, v := range t.Fields {
		if v == "" {
			continue
		}
		switch k {
		case "token_type", "scope", "error", "error_description":
			continue
		default:
			return true
		}
	}
	return false
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

func oauthConfig(scheme Scheme, redirect, secret string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     scheme.ClientID,
		ClientSecret: secret,
		RedirectURL:  redirect,
		Scopes:       scheme.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:       scheme.AuthorizationURL,
			DeviceAuthURL: scheme.DeviceAuthorizationURL,
			TokenURL:      scheme.TokenURL,
			AuthStyle:     oauth2.AuthStyleInParams,
		},
	}
}

func refreshToken(ctx context.Context, client *http.Client, clientID, secret, tokenURL, refresh string, secrets []string) (tokenResponse, error) {
	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: secret,
		Endpoint: oauth2.Endpoint{
			TokenURL:  tokenURL,
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
	ctx, cap := withOAuthClient(ctx, client)
	tok, err := cfg.TokenSource(ctx, &oauth2.Token{
		AccessToken:  "expired",
		RefreshToken: refresh,
		Expiry:       time.Unix(1, 0),
	}).Token()
	if err != nil {
		return tokenResponse{}, fmt.Errorf("token endpoint: %s", Redact(err.Error(), secrets, nil))
	}
	return capturedToken(tok, cap.take())
}

func clientCredentialsToken(ctx context.Context, client *http.Client, clientID, secret, tokenURL, audience string, scopes []string) (tokenResponse, error) {
	cfg := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: secret,
		TokenURL:     tokenURL,
		Scopes:       scopes,
		AuthStyle:    oauth2.AuthStyleInParams,
	}
	if audience != "" {
		cfg.EndpointParams = url.Values{"audience": {audience}}
	}
	ctx, cap := withOAuthClient(ctx, client)
	tok, err := cfg.Token(ctx)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("token endpoint: %s", Redact(err.Error(), []string{secret}, nil))
	}
	return capturedToken(tok, cap.take())
}

func capturedToken(tok *oauth2.Token, body []byte) (tokenResponse, error) {
	parsed, err := decodeTokenResponse(body)
	if err != nil {
		return tokenResponse{}, err
	}
	if tok != nil {
		if parsed.AccessToken == "" {
			parsed.AccessToken = tok.AccessToken
		}
		if parsed.RefreshToken == "" {
			parsed.RefreshToken = tok.RefreshToken
		}
		if parsed.ExpiresIn == 0 && tok.ExpiresIn > 0 {
			parsed.ExpiresIn = int(tok.ExpiresIn)
		}
	}
	if parsed.Error != "" || !parsed.hasToken() {
		return tokenResponse{}, fmt.Errorf("token endpoint rejected the request")
	}
	return parsed, nil
}

type bodyCapture struct {
	base http.RoundTripper
	mu   sync.Mutex
	last []byte
}

func withOAuthClient(ctx context.Context, base *http.Client) (context.Context, *bodyCapture) {
	cap := &bodyCapture{}
	if base == nil {
		base = &http.Client{}
	}
	cap.base = base.Transport
	client := *base
	client.Transport = cap
	return context.WithValue(ctx, oauth2.HTTPClient, &client), cap
}

func (c *bodyCapture) RoundTrip(req *http.Request) (*http.Response, error) {
	base := c.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if rerr != nil {
		return nil, rerr
	}
	c.mu.Lock()
	c.last = append([]byte(nil), raw...)
	c.mu.Unlock()
	// net/http sniffs a JSON body with no Content-Type as text/plain.
	// x/oauth2 then parses text/plain as a form and misses access_token.
	media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	trimmed := bytes.TrimSpace(raw)
	if (media == "" || media == "text/plain") && len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		resp.Header.Set("Content-Type", "application/json")
	}
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	return resp, nil
}

func (c *bodyCapture) take() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.last...)
}
