package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aiveto/veto/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpaqueUserTokenSkipsJWKS(t *testing.T) {
	var hits atomic.Int32
	jwks := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	defer jwks.Close()
	dir := t.TempDir()
	require.NoError(t, writeToken(dir, "user", storedToken{
		AccessToken: "app-token",
		ExpiresAt:   time.Now().Add(time.Hour),
		ClientID:    "veto",
		Issuer:      "https://idp.example",
		JWKSURI:     jwks.URL,
		Fields: map[string]string{
			"access_token": "app-token",
			"id_token":     "opaque-user",
		},
	}))
	mat, err := New(Options{
		Schemes: []Scheme{{
			Name: "user", Source: "login", ClientID: "veto", Issuer: "https://idp.example",
			UserHeader: "X-User-Token", JWKSURI: jwks.URL,
		}},
		Dir:  dir,
		HTTP: jwks.Client(),
	}).Material(context.Background(), &catalog.Operation{ID: "approvals.list"}, catalog.Auth{
		Name: "user", Kind: "oauth2", Header: "Authorization",
	}, "GET", "https://api.example/approvals", false)
	require.NoError(t, err)
	assert.Equal(t, "Bearer app-token", mat.Headers["Authorization"])
	assert.Equal(t, "opaque-user", mat.Headers["X-User-Token"])
	assert.NotEqual(t, "app-token", mat.Headers["X-User-Token"])
	assert.Equal(t, int32(0), hits.Load())
}

func TestUserJWTChecksIssuerAudienceAndExpiry(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "kid-1", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	defer jwks.Close()
	now := time.Now()
	good := signTestJWT(t, key, "kid-1", "https://idp.example", "veto", now.Add(time.Hour))
	cases := []struct {
		name string
		tok  string
		want string
	}{
		{name: "issuer", tok: signTestJWT(t, key, "kid-1", "https://evil.example", "veto", now.Add(time.Hour)), want: "issuer rejected"},
		{name: "audience", tok: signTestJWT(t, key, "kid-1", "https://idp.example", "other", now.Add(time.Hour)), want: "audience rejected"},
		{name: "expired", tok: signTestJWT(t, key, "kid-1", "https://idp.example", "veto", now.Add(-time.Hour)), want: "expired"},
		{name: "signature", tok: good[:len(good)-4] + "aaaa", want: "signature rejected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := placeStored(t, jwks, tc.tok)
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.want)
			assert.NotContains(t, err.Error(), tc.tok)
		})
	}
	mat, err := placeStored(t, jwks, good)
	require.NoError(t, err)
	assert.Equal(t, "Bearer app-token", mat.Headers["Authorization"])
	assert.Equal(t, good, mat.Headers["X-User-Token"])
}

func placeStored(t *testing.T, jwks *httptest.Server, userTok string) (Material, error) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, writeToken(dir, "user", storedToken{
		AccessToken: "app-token",
		ExpiresAt:   time.Now().Add(time.Hour),
		ClientID:    "veto",
		Issuer:      "https://idp.example",
		JWKSURI:     jwks.URL,
		Fields: map[string]string{
			"access_token": "app-token",
			"id_token":     userTok,
		},
	}))
	return New(Options{
		Schemes: []Scheme{{
			Name: "user", Source: "login", ClientID: "veto", Issuer: "https://idp.example",
			UserHeader: "X-User-Token", JWKSURI: jwks.URL,
		}},
		Dir:  dir,
		HTTP: jwks.Client(),
	}).Material(context.Background(), &catalog.Operation{ID: "approvals.list"}, catalog.Auth{
		Name: "user", Kind: "oauth2", Header: "Authorization",
	}, "GET", "https://api.example/approvals", false)
}

func signTestJWT(t *testing.T, key *rsa.PrivateKey, kid, iss, aud string, exp time.Time) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "RS256", "kid": kid})
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]any{"iss": iss, "aud": aud, "sub": "ada", "exp": exp.Unix()})
	require.NoError(t, err)
	enc := base64.RawURLEncoding
	signing := enc.EncodeToString(header) + "." + enc.EncodeToString(payload)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	require.NoError(t, err)
	return signing + "." + enc.EncodeToString(sig)
}
