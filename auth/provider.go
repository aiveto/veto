package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/credentials"
)

type (
	schemeProvider struct {
		r *Resolver
		a catalog.Auth
	}
	extraProvider struct {
		r   *Resolver
		a   catalog.Auth
		src credentials.Provider
	}
	fixedProvider struct {
		a     catalog.Auth
		token string
	}
)

func (p schemeProvider) Resolve(ctx context.Context, in credentials.Request) (credentials.Credential, error) {
	mat, err := p.r.materialScheme(ctx, p.a, in, forced(ctx))
	if err != nil {
		return credentials.Credential{}, err
	}
	return credentialOf(mat), nil
}

func (p extraProvider) Resolve(ctx context.Context, in credentials.Request) (credentials.Credential, error) {
	if in.UserToken != "" && UserToken(ctx) == "" {
		ctx = WithUserToken(ctx, in.UserToken)
	}
	if in.Scheme == "" {
		in.Scheme = p.a.Name
	}
	if in.UserToken == "" {
		in.UserToken = UserToken(ctx)
	}
	if len(in.Scopes) == 0 {
		in.Scopes = p.a.Scopes
		if len(in.Scopes) == 0 {
			if cfg, ok := p.r.schemes[p.a.Name]; ok {
				in.Scopes = cfg.Scopes
			}
		}
	}
	if in.Audience == "" {
		if cfg, ok := p.r.schemes[p.a.Name]; ok {
			in.Audience = cfg.Audience
		}
	}
	return p.src.Resolve(ctx, in)
}

func (p fixedProvider) Resolve(context.Context, credentials.Request) (credentials.Credential, error) {
	if p.token == "" {
		name := p.a.Name
		if name == "" {
			name = "credential"
		}
		return credentials.Credential{}, fmt.Errorf("%s is unset", name)
	}
	return credentialOf(placeToken(p.a, "", p.token, time.Time{})), nil
}

func credentialOf(mat Material) credentials.Credential {
	return credentials.Credential{
		Headers:   cloneMap(mat.Headers),
		Query:     cloneMap(mat.Query),
		ExpiresAt: mat.Expires,
		Sign:      mat.Sign,
	}
}

func materialOf(c credentials.Credential) Material {
	return Material{
		Headers: cloneMap(c.Headers),
		Query:   cloneMap(c.Query),
		Expires: c.ExpiresAt,
		Secrets: CredentialSecrets(c),
		Sign:    c.Sign,
	}
}

// CredentialSecrets lists header values, query values, and a bearer token without its prefix.
func CredentialSecrets(c credentials.Credential) []string {
	out := make([]string, 0, len(c.Headers)+len(c.Query))
	for _, v := range c.Headers {
		out = append(out, secretParts(v)...)
	}
	for _, v := range c.Query {
		out = append(out, secretParts(v)...)
	}
	return out
}

func secretParts(v string) []string {
	if v == "" {
		return nil
	}
	out := []string{v}
	const prefix = "bearer "
	if len(v) > len(prefix) && strings.EqualFold(v[:len(prefix)], prefix) {
		raw := strings.TrimSpace(v[len(prefix):])
		if raw != "" && raw != v {
			out = append(out, raw)
		}
	}
	return out
}
