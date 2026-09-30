package execute_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApprovalsListFollowsTheUserToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var idp *httptest.Server
	idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"issuer":                        idp.URL,
				"authorization_endpoint":        idp.URL + "/authorize",
				"token_endpoint":                idp.URL + "/token",
				"device_authorization_endpoint": idp.URL + "/device",
				"jwks_uri":                      idp.URL + "/jwks",
			})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(rsaJWKS(&key.PublicKey, "kid-1"))
		case "/device":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "device-secret",
				"user_code":        "ABCD",
				"verification_uri": idp.URL + "/device/verify",
				"interval":         1,
			})
		case "/token":
			jwt := signUserJWT(t, key, "kid-1", idp.URL, "veto", "ada", time.Now().Add(time.Hour))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "app-token",
				"refresh_token": "refresh-secret",
				"expires_in":    3600,
				"person_token":  jwt,
				"session_state": "sess-1",
				"token_type":    "Bearer",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer idp.Close()

	dir := t.TempDir()
	err = auth.Login(context.Background(), auth.LoginOptions{
		Scheme: auth.Scheme{
			Name: "appAuth", Source: "login", ClientID: "veto", Issuer: idp.URL,
			UserToken: "person_token", UserHeader: "X-User-Token", Scopes: []string{"approvals.read"},
		},
		Dir:    dir,
		Device: true,
		HTTP:   idp.Client(),
		Out:    io.Discard,
		Open:   func(string) error { t.Fatal("browser opened"); return nil },
	})
	require.NoError(t, err)
	raw, err := readTokenFile(t, dir, "appAuth")
	require.NoError(t, err)
	assert.Contains(t, raw, "refresh-secret")
	assert.Contains(t, raw, "person_token")
	assert.Contains(t, raw, "sess-1")
	assert.Contains(t, raw, "app-token")

	var gotUser, gotAuth string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = r.Header.Get("X-User-Token")
		gotAuth = r.Header.Get("Authorization")
		rows := []approval{{ID: "a1", Subject: "ada"}, {ID: "b1", Subject: "grace"}}
		sub := jwtSubject(gotUser)
		if sub == "" || gotUser == "app-token" || gotUser == gotAuth {
			_ = json.NewEncoder(w).Encode(rows)
			return
		}
		var mine []approval
		for _, row := range rows {
			if row.Subject == sub {
				mine = append(mine, row)
			}
		}
		_ = json.NewEncoder(w).Encode(mine)
	}))
	defer up.Close()

	rec, err := telemetry.Record()
	require.NoError(t, err)
	defer rec.Stop(context.Background())
	res, err := (execute.Client{
		BaseURL: up.URL,
		Creds: auth.New(auth.Options{Schemes: []auth.Scheme{{
			Name: "appAuth", Source: "login", ClientID: "veto", Issuer: idp.URL,
			UserToken: "person_token", UserHeader: "X-User-Token",
		}}, Dir: dir, HTTP: idp.Client()}),
	}).InvokeHTTPResult(context.Background(), loadSpec(t, approvalsSpec).ByID("approvals.list"), nil)
	require.NoError(t, err)
	assert.NotEmpty(t, gotUser)
	assert.NotEqual(t, "app-token", gotUser)
	assert.NotEqual(t, gotAuth, gotUser)
	assert.Equal(t, "Bearer app-token", gotAuth)
	assert.Equal(t, "ada", jwtSubject(gotUser))
	var got []approval
	require.NoError(t, json.Unmarshal([]byte(res.Body), &got))
	assert.Equal(t, []approval{{ID: "a1", Subject: "ada"}}, got)
	text := spanText(t, rec)
	assert.NotContains(t, text, "app-token")
	assert.NotContains(t, text, gotUser)
	assert.NotContains(t, text, "refresh-secret")
}

func TestMissingUserTokenSkipsUpstream(t *testing.T) {
	var tokenHits, upstreamHits atomic.Int32
	var idp *httptest.Server
	idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"authorization_endpoint":        idp.URL + "/authorize",
				"token_endpoint":                idp.URL + "/token",
				"device_authorization_endpoint": idp.URL + "/device",
			})
		case "/device":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code": "device-secret", "user_code": "ABCD",
				"verification_uri": idp.URL + "/verify", "interval": 1,
			})
		case "/token":
			tokenHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "app-token", "expires_in": 3600})
		default:
			http.NotFound(w, r)
		}
	}))
	defer idp.Close()

	dir := t.TempDir()
	require.NoError(t, auth.Login(context.Background(), auth.LoginOptions{
		Scheme: auth.Scheme{
			Name: "appAuth", Source: "login", ClientID: "veto", Issuer: idp.URL, UserHeader: "X-User-Token",
		},
		Dir: dir, Device: true, HTTP: idp.Client(), Out: io.Discard,
	}))
	tokenHits.Store(0)
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamHits.Add(1)
	}))
	defer up.Close()
	_, err := (execute.Client{
		BaseURL: up.URL,
		Creds: auth.New(auth.Options{Schemes: []auth.Scheme{{
			Name: "appAuth", Source: "login", ClientID: "veto", Issuer: idp.URL, UserHeader: "X-User-Token",
		}}, Dir: dir, HTTP: idp.Client()}),
	}).InvokeHTTPResult(context.Background(), loadSpec(t, approvalsSpec).ByID("approvals.list"), nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "user token is unset")
	assert.NotContains(t, err.Error(), "app-token")
	assert.Equal(t, int32(0), tokenHits.Load())
	assert.Equal(t, int32(0), upstreamHits.Load())
}

func TestContractUserHeaderAndWorkforceStayApart(t *testing.T) {
	t.Run("contract header", func(t *testing.T) {
		var idp *httptest.Server
		idp = httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		idp.Config.Handler = userLoginHandler(idp, map[string]any{
			"access_token": "app-token", "id_token": "opaque-user", "expires_in": 3600, "refresh_token": "refresh-secret",
		})
		defer idp.Close()
		dir := t.TempDir()
		require.NoError(t, auth.Login(context.Background(), auth.LoginOptions{
			Scheme: auth.Scheme{Name: "appAuth", Source: "login", ClientID: "veto", Issuer: idp.URL},
			Dir:    dir, Device: true, HTTP: idp.Client(), Out: io.Discard,
		}))
		var gotUser, gotAuth string
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotUser = r.Header.Get("X-User-Token")
			gotAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer up.Close()
		_, err := (execute.Client{
			BaseURL: up.URL,
			Creds: auth.New(auth.Options{Schemes: []auth.Scheme{{
				Name: "appAuth", Source: "login", ClientID: "veto", Issuer: idp.URL,
			}}, Dir: dir, HTTP: idp.Client()}),
		}).InvokeHTTPResult(context.Background(), loadSpec(t, contractUserSpec).ByID("approvals.list"), nil)
		require.NoError(t, err)
		assert.Equal(t, "opaque-user", gotUser)
		assert.Equal(t, "Bearer app-token", gotAuth)
	})

	t.Run("workforce", func(t *testing.T) {
		var tokenHits, upstreamHits atomic.Int32
		tokenSrv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			tokenHits.Add(1)
		}))
		defer tokenSrv.Close()
		up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			upstreamHits.Add(1)
		}))
		defer up.Close()
		t.Setenv("WORKFORCE_SECRET", "super-secret")
		_, err := (execute.Client{
			BaseURL: up.URL,
			Creds: auth.New(auth.Options{Schemes: []auth.Scheme{{
				Name: "bearerAuth", Source: "client_credentials", TokenURL: tokenSrv.URL,
				ClientID: "job", ClientSecretEnv: "WORKFORCE_SECRET", UserHeader: "X-User-Token",
			}}, Dir: t.TempDir(), HTTP: tokenSrv.Client()}),
		}).InvokeHTTPResult(context.Background(), loadSpec(t, bearerSpec).ByID("orders.get"), map[string]string{"id": "1"})
		require.Error(t, err)
		assert.ErrorContains(t, err, "user token is unset")
		assert.NotContains(t, err.Error(), "super-secret")
		assert.Equal(t, int32(0), tokenHits.Load())
		assert.Equal(t, int32(0), upstreamHits.Load())
	})
}

func TestBadUserTokenSkipsUpstream(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var idp *httptest.Server
	idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	idp.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"authorization_endpoint":        idp.URL + "/authorize",
				"token_endpoint":                idp.URL + "/token",
				"device_authorization_endpoint": idp.URL + "/device",
				"jwks_uri":                      idp.URL + "/jwks",
			})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(rsaJWKS(&key.PublicKey, "kid-1"))
		case "/device":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code": "d", "user_code": "ABCD", "verification_uri": idp.URL + "/v", "interval": 1,
			})
		case "/token":
			jwt := signUserJWT(t, key, "kid-1", idp.URL, "other-client", "ada", time.Now().Add(time.Hour))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "app-token", "id_token": jwt, "expires_in": 3600,
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer idp.Close()
	dir := t.TempDir()
	require.NoError(t, auth.Login(context.Background(), auth.LoginOptions{
		Scheme: auth.Scheme{
			Name: "appAuth", Source: "login", ClientID: "veto", Issuer: idp.URL, UserHeader: "X-User-Token",
		},
		Dir: dir, Device: true, HTTP: idp.Client(), Out: io.Discard,
	}))
	var upstreamHits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamHits.Add(1)
	}))
	defer up.Close()
	_, err = (execute.Client{
		BaseURL: up.URL,
		Creds: auth.New(auth.Options{Schemes: []auth.Scheme{{
			Name: "appAuth", Source: "login", ClientID: "veto", Issuer: idp.URL,
			UserHeader: "X-User-Token", JWKSURI: idp.URL + "/jwks",
		}}, Dir: dir, HTTP: idp.Client()}),
	}).InvokeHTTPResult(context.Background(), loadSpec(t, approvalsSpec).ByID("approvals.list"), nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "audience rejected")
	assert.NotContains(t, err.Error(), "app-token")
	assert.Equal(t, int32(0), upstreamHits.Load())
}

type approval struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
}

func userLoginHandler(idp *httptest.Server, token map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"authorization_endpoint":        idp.URL + "/authorize",
				"token_endpoint":                idp.URL + "/token",
				"device_authorization_endpoint": idp.URL + "/device",
			})
		case "/device":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code": "d", "user_code": "ABCD", "verification_uri": idp.URL + "/v", "interval": 1,
			})
		case "/token":
			_ = json.NewEncoder(w).Encode(token)
		default:
			http.NotFound(w, r)
		}
	}
}

func readTokenFile(t *testing.T, dir, scheme string) (string, error) {
	t.Helper()
	body, err := os.ReadFile(dir + "/" + scheme + ".json")
	return string(body), err
}

func signUserJWT(t *testing.T, key *rsa.PrivateKey, kid, iss, aud, sub string, exp time.Time) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"})
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]any{"iss": iss, "aud": aud, "sub": sub, "exp": exp.Unix()})
	require.NoError(t, err)
	enc := base64.RawURLEncoding
	signing := enc.EncodeToString(header) + "." + enc.EncodeToString(payload)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	require.NoError(t, err)
	return signing + "." + enc.EncodeToString(sig)
}

func rsaJWKS(pub *rsa.PublicKey, kid string) map[string]any {
	return map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256",
		"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}}}
}

func jwtSubject(tok string) string {
	parts := splitDot(tok)
	if len(parts) != 3 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var p struct {
		Sub string `json:"sub"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return ""
	}
	return p.Sub
}

func splitDot(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

const approvalsSpec = `openapi: 3.0.3
info: {title: Approvals, version: "1"}
servers:
  - url: http://127.0.0.1:9
paths:
  /approvals:
    get:
      operationId: approvals.list
      security:
        - appAuth: [approvals.read]
          userAuth: []
      responses:
        "200": {description: ok}
components:
  securitySchemes:
    appAuth:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://idp.example/authorize
          tokenUrl: https://idp.example/oauth/token
          scopes:
            approvals.read: read approvals
    userAuth:
      type: apiKey
      in: header
      name: X-User-Token
`

const contractUserSpec = `openapi: 3.0.3
info: {title: Approvals, version: "1"}
servers:
  - url: http://127.0.0.1:9
paths:
  /approvals:
    get:
      operationId: approvals.list
      security:
        - appAuth: []
      responses:
        "200": {description: ok}
components:
  securitySchemes:
    appAuth:
      type: oauth2
      x-user-token-header: X-User-Token
      flows:
        authorizationCode:
          authorizationUrl: https://idp.example/authorize
          tokenUrl: https://idp.example/oauth/token
          scopes: {}
`
