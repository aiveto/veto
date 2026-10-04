package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/result"
)

type (
	source interface {
		ready(ctx context.Context, r *Resolver, s Scheme) bool
		unset(r *Resolver, s Scheme) error
		refreshable() bool
		blockers(r *Resolver, s Scheme) []string
		fetch(ctx context.Context, r *Resolver, s Scheme, a catalog.Auth, in fetchIn) (Material, error)
	}

	cacheKeyed interface {
		cacheExtra(ctx context.Context, r *Resolver, s Scheme, method, endpoint string) (string, error)
	}

	userHeaderSource interface {
		suppliesUserHeader(s Scheme, header string) bool
	}

	fetchIn struct {
		operationID string
		method      string
		endpoint    string
		need        []string
		force       bool
	}

	envSource      struct{}
	loginSource    struct{}
	clientSource   struct{}
	invokeSource   struct{}
	commandSource  struct{}
	exchangeSource struct{}
)

func sourceOf(kind string) source {
	switch kind {
	case "", "env":
		return envSource{}
	case "login":
		return loginSource{}
	case "client_credentials":
		return clientSource{}
	case "invoke":
		return invokeSource{}
	case "command":
		return commandSource{}
	case "token_exchange":
		return exchangeSource{}
	default:
		return nil
	}
}

func (envSource) ready(_ context.Context, r *Resolver, s Scheme) bool {
	if s.Env != "" && r.env(s.Env) != "" {
		return true
	}
	tok, err := readToken(r.dir, s.Name)
	return err == nil && tok.AccessToken != ""
}

func (envSource) unset(_ *Resolver, s Scheme) error {
	return result.AuthError{Name: s.Name}
}

func (envSource) refreshable() bool { return false }

func (envSource) blockers(r *Resolver, s Scheme) []string {
	if s.Env == "" {
		return []string{fmt.Sprintf("auth scheme %s has no env var", s.Name)}
	}
	if (envSource{}).ready(context.Background(), r, s) {
		return nil
	}
	return []string{s.Env + " is unset"}
}

func (envSource) fetch(_ context.Context, r *Resolver, s Scheme, a catalog.Auth, _ fetchIn) (Material, error) {
	return r.fetchEnv(s, a)
}

func (loginSource) ready(_ context.Context, r *Resolver, s Scheme) bool {
	tok, err := readToken(r.dir, s.Name)
	if err != nil {
		return false
	}
	if tok.RefreshToken != "" {
		return true
	}
	return tok.AccessToken != "" && usable(tok.ExpiresAt, r.now())
}

func (loginSource) unset(_ *Resolver, s Scheme) error {
	return result.AuthError{Name: s.Name, Detail: "has no stored token"}
}

func (loginSource) refreshable() bool { return true }

func (loginSource) blockers(r *Resolver, s Scheme) []string {
	if HasRefreshToken(r.dir, s.Name) || HasAccessToken(r.dir, s.Name) {
		return nil
	}
	return []string{fmt.Sprintf("auth scheme %s has no stored token", s.Name)}
}

func (loginSource) fetch(ctx context.Context, r *Resolver, s Scheme, a catalog.Auth, in fetchIn) (Material, error) {
	return r.fetchLogin(ctx, s, a, in.need, in.force)
}

func (loginSource) suppliesUserHeader(s Scheme, header string) bool {
	return s.UserHeader == "" || s.UserHeader == header
}

func (clientSource) ready(_ context.Context, r *Resolver, s Scheme) bool {
	return s.TokenURL != "" && s.ClientID != "" && s.ClientSecretEnv != "" && r.env(s.ClientSecretEnv) != ""
}

func (clientSource) unset(_ *Resolver, s Scheme) error {
	return result.AuthError{Name: s.Name, Detail: "client secret is unset"}
}

func (clientSource) refreshable() bool { return true }

func (clientSource) blockers(r *Resolver, s Scheme) []string {
	if s.ClientSecretEnv == "" {
		return []string{fmt.Sprintf("auth scheme %s has no client secret env", s.Name)}
	}
	if r.env(s.ClientSecretEnv) == "" {
		return []string{s.ClientSecretEnv + " is unset"}
	}
	return nil
}

func (clientSource) fetch(ctx context.Context, r *Resolver, s Scheme, a catalog.Auth, in fetchIn) (Material, error) {
	if userHeaderName(s, a) != "" {
		return Material{}, fmt.Errorf("%s user token is unset", a.Name)
	}
	return r.fetchClient(ctx, s, a, in.need)
}

func (invokeSource) ready(ctx context.Context, _ *Resolver, _ Scheme) bool {
	return UserToken(ctx) != ""
}

func (invokeSource) unset(_ *Resolver, s Scheme) error {
	return result.AuthError{Name: s.Name}
}

func (invokeSource) refreshable() bool { return false }

func (invokeSource) blockers(*Resolver, Scheme) []string { return nil }

func (invokeSource) fetch(ctx context.Context, _ *Resolver, s Scheme, a catalog.Auth, _ fetchIn) (Material, error) {
	tok := UserToken(ctx)
	if tok == "" {
		return Material{}, fmt.Errorf("%s is unset", a.Name)
	}
	return placeToken(a, s.Header, tok, time.Time{}), nil
}

func (commandSource) ready(_ context.Context, _ *Resolver, s Scheme) bool {
	return len(s.Command) > 0
}

func (commandSource) unset(_ *Resolver, s Scheme) error {
	return result.AuthError{Name: s.Name}
}

func (commandSource) refreshable() bool { return true }

func (commandSource) blockers(_ *Resolver, s Scheme) []string {
	if len(s.Command) == 0 {
		return []string{fmt.Sprintf("auth scheme %s has no command", s.Name)}
	}
	return nil
}

func (commandSource) fetch(ctx context.Context, r *Resolver, s Scheme, _ catalog.Auth, in fetchIn) (Material, error) {
	return r.fetchCommand(ctx, s, in.operationID, in.method, in.endpoint)
}

func (commandSource) cacheExtra(ctx context.Context, _ *Resolver, _ Scheme, method, endpoint string) (string, error) {
	return "\x00" + method + "\x00" + endpoint + "\x00" + UserToken(ctx), nil
}

func (exchangeSource) ready(ctx context.Context, r *Resolver, s Scheme) bool {
	if s.TokenURL == "" || s.ClientID == "" || s.ClientSecretEnv == "" || r.env(s.ClientSecretEnv) == "" {
		return false
	}
	return r.subjectToken(ctx, s) != ""
}

func (exchangeSource) unset(r *Resolver, s Scheme) error {
	if s.ClientSecretEnv == "" || r.env(s.ClientSecretEnv) == "" {
		return result.AuthError{Name: s.Name, Detail: "client secret is unset"}
	}
	return result.AuthError{Name: s.Name, Detail: "subject token is unset"}
}

func (exchangeSource) refreshable() bool { return true }

func (exchangeSource) blockers(r *Resolver, s Scheme) []string {
	if s.ClientSecretEnv == "" || r.env(s.ClientSecretEnv) == "" {
		envName := s.ClientSecretEnv
		if envName == "" {
			envName = "client secret"
		}
		return []string{envName + " is unset"}
	}
	if s.Subject != "" && s.Subject != "invoke" && !HasAccessToken(r.dir, s.Subject) {
		return []string{fmt.Sprintf("auth scheme %s has no stored token", s.Subject)}
	}
	return nil
}

func (exchangeSource) fetch(ctx context.Context, r *Resolver, s Scheme, a catalog.Auth, in fetchIn) (Material, error) {
	if userHeaderName(s, a) != "" {
		return Material{}, fmt.Errorf("%s user token is unset", a.Name)
	}
	return r.fetchExchange(ctx, s, a, in.need)
}

func (exchangeSource) cacheExtra(ctx context.Context, r *Resolver, s Scheme, _, _ string) (string, error) {
	subject := r.subjectToken(ctx, s)
	if subject == "" {
		return "", fmt.Errorf("%s subject token is unset", s.Name)
	}
	return "\x00" + subject, nil
}
