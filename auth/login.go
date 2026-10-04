package auth

import (
	"context"
	"errors"
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

	"golang.org/x/oauth2"
)

const defaultRedirect = "http://127.0.0.1:53682/callback"

type LoginOptions struct {
	Scheme      Scheme
	Dir         string
	Device      bool
	Open        func(context.Context, string) error
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
		return errors.New("client id is unset")
	}
	if scheme.TokenURL == "" {
		return errors.New("token url is unset")
	}
	if opt.Device {
		return deviceLogin(ctx, opt, scheme)
	}
	if scheme.AuthorizationURL == "" {
		return errors.New("authorization url is unset")
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

func OpenBrowser(ctx context.Context, raw string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{raw}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", raw}
	default:
		name, args = "xdg-open", []string{raw}
	}
	return exec.CommandContext(ctx, name, args...).Start()
}

// SetToken stores a token the operator already holds. The file mode is 0600.
func SetToken(dir, scheme, token string) error {
	if strings.TrimSpace(token) == "" {
		return errors.New("token is empty")
	}
	if dir == "" {
		dir = DefaultTokenDir()
	}
	return writeToken(dir, scheme, storedToken{AccessToken: strings.TrimSpace(token)})
}

func codeLogin(ctx context.Context, opt LoginOptions, scheme Scheme) error {
	verifier := oauth2.GenerateVerifier()
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
	secret := secretFromEnv(scheme.ClientSecretEnv)
	var cfg *oauth2.Config
	code, err := waitForCode(ctx, redirect, state, func(listen string) error {
		cfg = oauthConfig(scheme, listen, secret)
		authURL := cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
		if _, err := fmt.Fprintf(opt.Out, "Open this URL to sign in:\n%s\n", authURL); err != nil {
			return fmt.Errorf("write login url: %w", err)
		}
		if err := opt.Open(ctx, authURL); err != nil {
			return fmt.Errorf("open browser: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	ctx, captured := withOAuthClient(ctx, opt.HTTP)
	tok, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return fmt.Errorf("token endpoint: %s", Redact(err.Error(), []string{secret, code, verifier}, nil))
	}
	parsed, err := capturedToken(tok, captured.take())
	if err != nil {
		return err
	}
	return saveMinted(opt.Dir, scheme, parsed, scheme.Scopes)
}

func deviceLogin(ctx context.Context, opt LoginOptions, scheme Scheme) error {
	if scheme.DeviceAuthorizationURL == "" {
		return errors.New("device authorization endpoint is unset")
	}
	secret := secretFromEnv(scheme.ClientSecretEnv)
	cfg := oauthConfig(scheme, "", secret)
	ctx, captured := withOAuthClient(ctx, opt.HTTP)
	var opts []oauth2.AuthCodeOption
	if secret != "" {
		opts = append(opts, oauth2.SetAuthURLParam("client_secret", secret))
	}
	dev, err := cfg.DeviceAuth(ctx, opts...)
	if err != nil {
		return fmt.Errorf("device authorization: %s", Redact(err.Error(), []string{secret}, nil))
	}
	show := dev.VerificationURI
	if dev.VerificationURIComplete != "" {
		show = dev.VerificationURIComplete
	}
	if _, err := fmt.Fprintf(opt.Out, "Open %s\nEnter code %s\n", show, dev.UserCode); err != nil {
		return fmt.Errorf("write device code: %w", err)
	}
	tok, err := cfg.DeviceAccessToken(ctx, dev)
	if err != nil {
		if ctx.Err() != nil {
			return errors.New("login timed out")
		}
		return fmt.Errorf("token endpoint: %s", Redact(err.Error(), []string{secret, dev.DeviceCode}, nil))
	}
	parsed, err := capturedToken(tok, captured.take())
	if err != nil {
		return err
	}
	return saveMinted(opt.Dir, scheme, parsed, scheme.Scopes)
}

func saveMinted(dir string, scheme Scheme, tok tokenResponse, requested []string) error {
	scopes, err := grantedScopes(tok.Scope, requested)
	if err != nil {
		return err
	}
	stored := absorb(storedToken{}, tok, scheme, scopes, expiryFrom(time.Now(), tok.ExpiresIn), scheme.TokenURL, scheme.ClientID)
	if stored.RefreshToken == "" && stored.AccessToken == "" {
		return errors.New("token endpoint rejected the request")
	}
	return writeToken(dir, scheme.Name, stored)
}

func waitForCode(ctx context.Context, redirect, state string, open func(listen string) error) (string, error) {
	u, err := url.Parse(redirect)
	if err != nil {
		return "", fmt.Errorf("redirect url: %w", err)
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", u.Host)
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
			case failed <- errors.New("login state mismatch"):
			default:
			}
			return
		}
		if r.URL.Query().Get("error") != "" {
			http.Error(w, "login failed", http.StatusBadRequest)
			select {
			case failed <- errors.New("login was denied"):
			default:
			}
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			select {
			case failed <- errors.New("login returned no code"):
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
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shut, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	listen := redirect
	if u.Port() == "" || strings.HasSuffix(u.Host, ":0") {
		listen = fmt.Sprintf("%s://%s%s", u.Scheme, ln.Addr().String(), path)
	}
	if err := open(listen); err != nil {
		return "", err
	}
	select {
	case <-ctx.Done():
		return "", errors.New("login timed out")
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
