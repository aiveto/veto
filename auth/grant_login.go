package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aiveto/veto/catalog"
)

func (r *Resolver) fetchLogin(ctx context.Context, s Scheme, a catalog.Auth, need []string, force bool) (Material, error) {
	stored, err := readToken(r.dir, s.Name, r.json)
	if err != nil || (stored.RefreshToken == "" && tokenField(stored, authFieldName(s)) == "") {
		return Material{}, fmt.Errorf("%s has no stored token", a.Name)
	}
	now := r.now()
	if !force && loginSendable(s, a, stored, need, now) {
		mat, perr := r.placeLogin(s, a, stored, stored.ExpiresAt)
		if perr == nil {
			return mat, nil
		}
		if stored.RefreshToken == "" {
			return Material{}, perr
		}
	}
	if stored.RefreshToken == "" {
		if tokenField(stored, authFieldName(s)) == "" || force || !usable(stored.ExpiresAt, now) {
			return Material{}, fmt.Errorf("%s has no stored token", a.Name)
		}
		return r.placeLogin(s, a, stored, stored.ExpiresAt)
	}
	if err := fillEndpoints(ctx, r.http, &s); err != nil {
		if !force && usable(stored.ExpiresAt, now) {
			if mat, perr := r.placeLogin(s, a, stored, stored.ExpiresAt); perr == nil {
				return mat, nil
			}
		}
		return Material{}, err
	}
	tokenURL := s.TokenURL
	if tokenURL == "" {
		tokenURL = stored.TokenURL
	}
	clientID := s.ClientID
	if clientID == "" {
		clientID = stored.ClientID
	}
	secret := r.env(s.ClientSecretEnv)
	secrets := append([]string{secret, stored.RefreshToken, stored.AccessToken}, fieldSecrets(stored.Fields)...)
	tok, err := refreshToken(ctx, r.http, clientID, secret, tokenURL, stored.RefreshToken, secrets)
	if err != nil {
		if !force && usable(stored.ExpiresAt, now) {
			if mat, perr := r.placeLogin(s, a, stored, stored.ExpiresAt); perr == nil {
				return mat, nil
			}
		}
		return Material{}, err
	}
	scopes, err := grantedScopes(tok.Scope, need)
	if err != nil {
		return Material{}, err
	}
	exp := expiryFrom(r.now(), tok.ExpiresIn)
	next := absorb(stored, tok, s, scopes, exp, tokenURL, clientID)
	if err := writeToken(r.dir, s.Name, next, r.json); err != nil {
		return Material{}, err
	}
	return r.placeLogin(s, a, next, next.ExpiresAt)
}

func loginSendable(s Scheme, a catalog.Auth, stored storedToken, need []string, now time.Time) bool {
	if tokenField(stored, authFieldName(s)) == "" || !fresh(stored.ExpiresAt, now) || !covers(stored.Scopes, need) {
		return false
	}
	if userHeaderName(s, a) != "" && tokenField(stored, userFieldName(s)) == "" {
		return false
	}
	return true
}

func (r *Resolver) placeLogin(s Scheme, a catalog.Auth, stored storedToken, exp time.Time) (Material, error) {
	authTok := tokenField(stored, authFieldName(s))
	if authTok == "" {
		return Material{}, fmt.Errorf("%s has no stored token", a.Name)
	}
	mat := placeToken(a, s.Header, authTok, exp)
	header := userHeaderName(s, a)
	if header == "" {
		return mat, nil
	}
	for k := range mat.Headers {
		if strings.EqualFold(k, header) {
			return Material{}, fmt.Errorf("%s user token header matches the auth header", a.Name)
		}
	}
	userTok := tokenField(stored, userFieldName(s))
	if userTok == "" {
		return Material{}, fmt.Errorf("%s user token is unset", a.Name)
	}
	if mat.Headers == nil {
		mat.Headers = map[string]string{}
	}
	mat.Headers[header] = userTok
	mat.Secrets = append(mat.Secrets, userTok)
	return mat, nil
}
