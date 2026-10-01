package mcpserver

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// CallerHeader is the credential of the process calling veto.
	// The value identifies that caller. It is not an upstream API token.
	CallerHeader = "Veto-Caller"

	// DefaultAddr is the listen address for serve --http.
	DefaultAddr = "127.0.0.1:7433"
)

// Identity is one caller of the HTTP server. Token is the credential. ID is the name stored with an approval.
type Identity struct {
	ID    string
	Token string
}

// Handler is the Streamable HTTP MCP endpoint. A request without a known caller credential is rejected first.
func Handler(srv *Server, opt Options, ids []Identity) (http.Handler, error) {
	if err := checkIdentities(ids); err != nil {
		return nil, err
	}
	server := newMCP(srv, opt)
	inner := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil)
	verified := mcpauth.RequireBearerToken(func(_ context.Context, token string, _ *http.Request) (*mcpauth.TokenInfo, error) {
		id, ok := matchIdentity(token, ids)
		if !ok {
			return nil, fmt.Errorf("%w", mcpauth.ErrInvalidToken)
		}
		return &mcpauth.TokenInfo{UserID: id}, nil
	}, &mcpauth.RequireBearerTokenOptions{AllowMissingExpiration: true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Del("Authorization")
		r.Header.Del(CallerHeader)
		inner.ServeHTTP(w, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cred := r.Header.Get(CallerHeader)
		if cred == "" {
			http.Error(w, "caller credential required", http.StatusUnauthorized)
			return
		}
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+cred)
		r.Header.Del(CallerHeader)
		verified.ServeHTTP(w, r)
	}), nil
}

// Identities reads caller credentials from the environment variables named in names.
// names maps a caller id to an environment variable. The credential values are not returned in errors.
func Identities(names map[string]string, getenv func(string) string) ([]Identity, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("caller credential required")
	}
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	out := make([]Identity, 0, len(names))
	for id, env := range names {
		if id == "" || env == "" {
			return nil, fmt.Errorf("caller credential required")
		}
		token := getenv(env)
		if token == "" {
			return nil, fmt.Errorf("caller %s credential is unset", id)
		}
		out = append(out, Identity{ID: id, Token: token})
	}
	if err := checkIdentities(out); err != nil {
		return nil, err
	}
	return out, nil
}

func checkIdentities(ids []Identity) error {
	if len(ids) == 0 {
		return fmt.Errorf("caller credential required")
	}
	seen := map[string]string{}
	for _, id := range ids {
		if id.ID == "" || id.Token == "" {
			return fmt.Errorf("caller credential required")
		}
		if other, ok := seen[id.Token]; ok {
			return fmt.Errorf("caller %s uses the same credential as %s", id.ID, other)
		}
		seen[id.Token] = id.ID
	}
	return nil
}

func matchIdentity(token string, ids []Identity) (string, bool) {
	if token == "" {
		return "", false
	}
	got := []byte(token)
	var id string
	found := 0
	for _, c := range ids {
		if subtle.ConstantTimeCompare(got, []byte(c.Token)) == 1 {
			id = c.ID
			found = 1
		}
	}
	if found != 1 {
		return "", false
	}
	return id, true
}

// Serve listens on addr until ctx is done. An empty addr uses DefaultAddr.
func Serve(ctx context.Context, addr string, h http.Handler) error {
	if addr == "" {
		addr = DefaultAddr
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	return serveListener(ctx, ln, h)
}

// Listen serves h and returns the bound address. The server stops when ctx is done.
func Listen(ctx context.Context, addr string, h http.Handler) (string, error) {
	if addr == "" {
		addr = DefaultAddr
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("listen: %w", err)
	}
	bound := ln.Addr().String()
	go func() {
		_ = serveListener(ctx, ln, h)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, dialErr := net.DialTimeout("tcp", bound, 20*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return bound, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("listen: %w", dialErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func serveListener(ctx context.Context, ln net.Listener, h http.Handler) error {
	if ctx == nil {
		ctx = context.Background()
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	errc := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errc <- err
	}()
	select {
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
		return <-errc
	case err := <-errc:
		return err
	}
}
