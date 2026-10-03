package auth

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/aiveto/veto/catalog"
)

func (r *Resolver) fetchExchange(ctx context.Context, s Scheme, a catalog.Auth, need []string) (Material, error) {
	secret := r.env(s.ClientSecretEnv)
	if secret == "" || s.TokenURL == "" || s.ClientID == "" {
		return Material{}, fmt.Errorf("%s client secret is unset", a.Name)
	}
	subject := r.subjectToken(ctx, s)
	if subject == "" {
		return Material{}, fmt.Errorf("%s subject token is unset", a.Name)
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	form.Set("subject_token", subject)
	tokenType := s.SubjectTokenType
	if tokenType == "" {
		tokenType = "urn:ietf:params:oauth:token-type:access_token"
	}
	form.Set("subject_token_type", tokenType)
	form.Set("client_id", s.ClientID)
	form.Set("client_secret", secret)
	if s.Audience != "" {
		form.Set("audience", s.Audience)
	}
	if scopes := scopeParam(need); scopes != "" {
		form.Set("scope", scopes)
	}
	tok, err := postForm(ctx, r.http, s.TokenURL, form, []string{secret, subject})
	if err != nil {
		return Material{}, err
	}
	if _, err := grantedScopes(tok.Scope, need); err != nil {
		return Material{}, err
	}
	return placeToken(a, s.Header, tok.AccessToken, expiryFrom(r.now(), tok.ExpiresIn)), nil
}

func (r *Resolver) subjectToken(ctx context.Context, s Scheme) string {
	if s.Subject == "invoke" {
		return UserToken(ctx)
	}
	if s.Subject == "" {
		return ""
	}
	stored, err := readToken(r.dir, s.Subject)
	if err != nil {
		return ""
	}
	if subjectExpired(stored, r.now()) {
		if stored.RefreshToken == "" {
			return ""
		}
		login := Scheme{Name: s.Subject}
		if other, ok := r.schemes[s.Subject]; ok {
			login = other
		}
		login.Name = s.Subject
		if _, err := r.fetchLogin(ctx, login, catalog.Auth{Name: s.Subject}, nil, true); err != nil {
			return ""
		}
		stored, err = readToken(r.dir, s.Subject)
		if err != nil {
			return ""
		}
	}
	if other, ok := r.schemes[s.Subject]; ok {
		if v := tokenField(stored, authFieldName(other)); v != "" {
			return v
		}
	}
	return tokenField(stored, "access_token")
}

func subjectExpired(stored storedToken, now time.Time) bool {
	return !stored.ExpiresAt.IsZero() && !now.Before(stored.ExpiresAt)
}
