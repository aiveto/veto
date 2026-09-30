package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const defaultRedirect = "http://127.0.0.1:53682/callback"

type LoginOptions struct {
	Scheme      Scheme
	Dir         string
	Device      bool
	Open        func(string) error
	HTTP        *http.Client
	RedirectURL string
	Out         io.Writer
}

// Login runs authorization code with PKCE, or device code when there is no browser.
// It stores the refresh token and does not run during invoke.
func Login(ctx context.Context, opt LoginOptions) error {
	if opt.Dir == "" {
		opt.Dir = DefaultTokenDir()
	}
	if opt.HTTP == nil {
		opt.HTTP = WithEnvProxy(&http.Client{Timeout: 30 * time.Second})
	} else {
		opt.HTTP = WithEnvProxy(opt.HTTP)
	}
	if opt.Out == nil {
		opt.Out = os.Stderr
	}
	if opt.Open == nil {
		opt.Open = OpenBrowser
	}
	scheme := opt.Scheme
	if err := fillEndpoints(ctx, opt.HTTP, &scheme); err != nil {
		return err
	}
	if scheme.ClientID == "" {
		return fmt.Errorf("client id is unset")
	}
	if scheme.TokenURL == "" {
		return fmt.Errorf("token url is unset")
	}
	if opt.Device {
		return deviceLogin(ctx, opt, scheme)
	}
	if scheme.AuthorizationURL == "" {
		return fmt.Errorf("authorization url is unset")
	}
	return codeLogin(ctx, opt, scheme)
}

func HasBrowser() bool {
	if os.Getenv("VETO_NO_BROWSER") == "1" {
		return false
	}
	switch runtime.GOOS {
	case "darwin", "windows":
		return true
	default:
		return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("BROWSER") != ""
	}
}

func OpenBrowser(raw string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", raw)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", raw)
	default:
		cmd = exec.Command("xdg-open", raw)
	}
	return cmd.Start()
}

// SetToken stores a token the operator already holds. The file mode is 0600.
func SetToken(dir, scheme, token string) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("token is empty")
	}
	if dir == "" {
		dir = DefaultTokenDir()
	}
	return writeToken(dir, scheme, storedToken{AccessToken: strings.TrimSpace(token)})
}

func codeLogin(ctx context.Context, opt LoginOptions, scheme Scheme) error {
	verifier, challenge, err := pkce()
	if err != nil {
		return err
	}
	state, err := randomState()
	if err != nil {
		return err
	}
	redirect := opt.RedirectURL
	if redirect == "" {
		redirect = scheme.RedirectURL
	}
	if redirect == "" {
		redirect = defaultRedirect
	}
	var usedRedirect string
	code, err := waitForCode(ctx, redirect, state, func(listen string) error {
		usedRedirect = listen
		authURL, err := authorizeURL(scheme, listen, state, challenge)
		if err != nil {
			return err
		}
		fmt.Fprintf(opt.Out, "Open this URL to sign in:\n%s\n", authURL)
		if err := opt.Open(authURL); err != nil {
			return fmt.Errorf("open browser: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", usedRedirect)
	form.Set("client_id", scheme.ClientID)
	form.Set("code_verifier", verifier)
	secret := secretFromEnv(scheme.ClientSecretEnv)
	if secret != "" {
		form.Set("client_secret", secret)
	}
	tok, err := postForm(ctx, opt.HTTP, scheme.TokenURL, form, []string{secret, code, verifier})
	if err != nil {
		return err
	}
	return saveMinted(opt.Dir, scheme, tok, scheme.Scopes)
}

func deviceLogin(ctx context.Context, opt LoginOptions, scheme Scheme) error {
	if scheme.DeviceAuthorizationURL == "" {
		return fmt.Errorf("device authorization endpoint is unset")
	}
	form := url.Values{}
	form.Set("client_id", scheme.ClientID)
	if scopes := scopeParam(scheme.Scopes); scopes != "" {
		form.Set("scope", scopes)
	}
	secret := secretFromEnv(scheme.ClientSecretEnv)
	if secret != "" {
		form.Set("client_secret", secret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, scheme.DeviceAuthorizationURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("device authorization: request failed")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := opt.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("device authorization: %s", Redact(err.Error(), []string{secret}, nil))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("device authorization: request failed")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("device authorization returned %d", resp.StatusCode)
	}
	var dev struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		Interval                int    `json:"interval"`
		Error                   string `json:"error"`
	}
	if err := json.Unmarshal(body, &dev); err != nil || dev.Error != "" || dev.DeviceCode == "" {
		return fmt.Errorf("device authorization rejected the request")
	}
	show := dev.VerificationURI
	if dev.VerificationURIComplete != "" {
		show = dev.VerificationURIComplete
	}
	fmt.Fprintf(opt.Out, "Open %s\nEnter code %s\n", show, dev.UserCode)
	interval := time.Duration(dev.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for {
		form := url.Values{}
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		form.Set("device_code", dev.DeviceCode)
		form.Set("client_id", scheme.ClientID)
		if secret != "" {
			form.Set("client_secret", secret)
		}
		tok, pending, err := pollDevice(ctx, opt.HTTP, scheme.TokenURL, form, []string{secret, dev.DeviceCode})
		if err != nil {
			return err
		}
		if !pending {
			return saveMinted(opt.Dir, scheme, tok, scheme.Scopes)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("login timed out")
		case <-timer.C:
		}
	}
}

func pollDevice(ctx context.Context, client *http.Client, endpoint string, form url.Values, secrets []string) (tokenResponse, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, false, fmt.Errorf("token endpoint: request failed")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return tokenResponse{}, false, fmt.Errorf("token endpoint: %s", Redact(err.Error(), secrets, nil))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return tokenResponse{}, false, fmt.Errorf("token endpoint: request failed")
	}
	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return tokenResponse{}, false, fmt.Errorf("token endpoint returned invalid JSON")
	}
	if tok.Error == "authorization_pending" || tok.Error == "slow_down" {
		return tokenResponse{}, true, nil
	}
	if resp.StatusCode != http.StatusOK || tok.Error != "" || tok.AccessToken == "" {
		return tokenResponse{}, false, fmt.Errorf("token endpoint rejected the request")
	}
	return tok, false, nil
}

func saveMinted(dir string, scheme Scheme, tok tokenResponse, requested []string) error {
	scopes, err := grantedScopes(tok.Scope, requested)
	if err != nil {
		return err
	}
	now := time.Now()
	stored := storedToken{
		RefreshToken: tok.RefreshToken,
		AccessToken:  tok.AccessToken,
		ExpiresAt:    expiryFrom(now, tok.ExpiresIn),
		Scopes:       scopes,
		Audience:     scheme.Audience,
		TokenURL:     scheme.TokenURL,
		ClientID:     scheme.ClientID,
	}
	if stored.RefreshToken == "" && stored.AccessToken == "" {
		return fmt.Errorf("token endpoint rejected the request")
	}
	return writeToken(dir, scheme.Name, stored)
}

func authorizeURL(scheme Scheme, redirect, state, challenge string) (string, error) {
	u, err := url.Parse(scheme.AuthorizationURL)
	if err != nil {
		return "", fmt.Errorf("authorization url: %w", err)
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", scheme.ClientID)
	q.Set("redirect_uri", redirect)
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	if scopes := scopeParam(scheme.Scopes); scopes != "" {
		q.Set("scope", scopes)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func waitForCode(ctx context.Context, redirect, state string, open func(listen string) error) (string, error) {
	u, err := url.Parse(redirect)
	if err != nil {
		return "", fmt.Errorf("redirect url: %w", err)
	}
	ln, err := net.Listen("tcp", u.Host)
	if err != nil {
		return "", fmt.Errorf("listen on redirect URL: %w", err)
	}
	got := make(chan string, 1)
	failed := make(chan error, 1)
	mux := http.NewServeMux()
	path := u.Path
	if path == "" {
		path = "/"
	}
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			select {
			case failed <- fmt.Errorf("login state mismatch"):
			default:
			}
			return
		}
		if r.URL.Query().Get("error") != "" {
			http.Error(w, "login failed", http.StatusBadRequest)
			select {
			case failed <- fmt.Errorf("login was denied"):
			default:
			}
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			select {
			case failed <- fmt.Errorf("login returned no code"):
			default:
			}
			return
		}
		_, _ = io.WriteString(w, "Signed in. You can close this window.\n")
		select {
		case got <- code:
		default:
		}
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Shutdown(context.Background()) }()
	listen := redirect
	if u.Port() == "" || strings.HasSuffix(u.Host, ":0") {
		listen = fmt.Sprintf("%s://%s%s", u.Scheme, ln.Addr().String(), path)
	}
	if err := open(listen); err != nil {
		return "", err
	}
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("login timed out")
	case err := <-failed:
		return "", err
	case code := <-got:
		return code, nil
	}
}

func secretFromEnv(name string) string {
	if name == "" {
		return ""
	}
	return os.Getenv(name)
}
