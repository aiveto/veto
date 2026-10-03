package auth

import (
	"context"
	"fmt"

	"github.com/aiveto/veto/catalog"
)

func (r *Resolver) fetchClient(ctx context.Context, s Scheme, a catalog.Auth, need []string) (Material, error) {
	secret := r.env(s.ClientSecretEnv)
	if secret == "" || s.TokenURL == "" || s.ClientID == "" {
		return Material{}, fmt.Errorf("%s client secret is unset", a.Name)
	}
	tok, err := clientCredentialsToken(ctx, r.http, s.ClientID, secret, s.TokenURL, s.Audience, need)
	if err != nil {
		return Material{}, err
	}
	scopes, err := grantedScopes(tok.Scope, need)
	if err != nil {
		return Material{}, err
	}
	_ = scopes
	exp := expiryFrom(r.now(), tok.ExpiresIn)
	return placeToken(a, s.Header, tok.AccessToken, exp), nil
}
