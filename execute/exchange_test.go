package execute_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/credentials"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenExchangeSendsTheNewAccessToken(t *testing.T) {
	var hits atomic.Int32
	var gotGrant, gotSubject, gotType, gotAud, gotScope string
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		gotGrant = form.Get("grant_type")
		gotSubject = form.Get("subject_token")
		gotType = form.Get("subject_token_type")
		gotAud = form.Get("audience")
		gotScope = form.Get("scope")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "exchanged-token", "expires_in": 3600})
	}))
	defer tokenSrv.Close()
	var gotAuth string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()
	t.Setenv("VETO_SECRET", "super-secret")
	op := loadSpec(t, bearerSpec).ByID("orders.get")
	client := execute.Client{
		BaseURL: up.URL,
		Creds: auth.New(auth.Options{Schemes: []auth.Scheme{{
			Name: "bearerAuth", Source: "token_exchange", TokenURL: tokenSrv.URL,
			ClientID: "veto", ClientSecretEnv: "VETO_SECRET", Audience: "https://api.example",
			Scopes: []string{"orders.read"}, Subject: "invoke",
		}}, Dir: t.TempDir(), HTTP: tokenSrv.Client()}),
	}
	ctx := auth.WithUserToken(context.Background(), "caller-token")
	_, err := client.InvokeHTTPResult(ctx, op, map[string]string{"id": "1"})
	require.NoError(t, err)
	_, err = client.InvokeHTTPResult(ctx, op, map[string]string{"id": "1"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), hits.Load())
	assert.Equal(t, "urn:ietf:params:oauth:grant-type:token-exchange", gotGrant)
	assert.Equal(t, "caller-token", gotSubject)
	assert.Equal(t, "urn:ietf:params:oauth:token-type:access_token", gotType)
	assert.Equal(t, "https://api.example", gotAud)
	assert.Equal(t, "orders.read", gotScope)
	assert.Equal(t, "Bearer exchanged-token", gotAuth)
	assert.NotContains(t, gotAuth, "caller-token")
}

func TestTokenExchangeSubjectCanBeAStoredLogin(t *testing.T) {
	var gotSubject string
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		gotSubject = form.Get("subject_token")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "exchanged-token", "expires_in": 3600})
	}))
	defer tokenSrv.Close()
	var gotAuth string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()
	t.Setenv("VETO_SECRET", "super-secret")
	dir := t.TempDir()
	require.NoError(t, auth.SetToken(dir, "user", "stored-user-token"))
	_, err := (execute.Client{
		BaseURL: up.URL,
		Creds: auth.New(auth.Options{Schemes: []auth.Scheme{{
			Name: "bearerAuth", Source: "token_exchange", TokenURL: tokenSrv.URL,
			ClientID: "veto", ClientSecretEnv: "VETO_SECRET", Audience: "https://api.example",
			Subject: "user",
		}}, Dir: dir, HTTP: tokenSrv.Client()}),
	}).InvokeHTTPResult(context.Background(), loadSpec(t, bearerSpec).ByID("orders.get"), map[string]string{"id": "1"})
	require.NoError(t, err)
	assert.Equal(t, "stored-user-token", gotSubject)
	assert.Equal(t, "Bearer exchanged-token", gotAuth)
	assert.NotContains(t, gotAuth, "stored-user-token")
}

func TestMissingExchangeSubjectSkipsHTTP(t *testing.T) {
	var tokenHits, upstreamHits atomic.Int32
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		tokenHits.Add(1)
	}))
	defer tokenSrv.Close()
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamHits.Add(1)
	}))
	defer up.Close()
	t.Setenv("VETO_SECRET", "super-secret")
	_, err := (execute.Client{
		BaseURL: up.URL,
		Creds: auth.New(auth.Options{Schemes: []auth.Scheme{{
			Name: "bearerAuth", Source: "token_exchange", TokenURL: tokenSrv.URL,
			ClientID: "veto", ClientSecretEnv: "VETO_SECRET", Audience: "https://api.example",
			Subject: "invoke",
		}}, Dir: t.TempDir(), HTTP: tokenSrv.Client()}),
	}).InvokeHTTPResult(context.Background(), loadSpec(t, bearerSpec).ByID("orders.get"), map[string]string{"id": "1"})
	require.Error(t, err)
	require.ErrorContains(t, err, "subject token is unset")
	assert.NotContains(t, err.Error(), "super-secret")
	assert.Equal(t, int32(0), tokenHits.Load())
	assert.Equal(t, int32(0), upstreamHits.Load())
}

func TestDeniedExchangeSkipsTheTokenURL(t *testing.T) {
	var tokenHits, upstreamHits atomic.Int32
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		tokenHits.Add(1)
	}))
	defer tokenSrv.Close()
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamHits.Add(1)
	}))
	defer up.Close()
	t.Setenv("VETO_SECRET", "super-secret")
	cat := loadSpec(t, bearerSpec)
	op := cat.ByID("orders.get")
	op.Permissions = []string{"orders.read"}
	loop, err := agent.New(cat, nil, execute.Client{
		BaseURL: up.URL,
		Creds: auth.New(auth.Options{Schemes: []auth.Scheme{{
			Name: "bearerAuth", Source: "token_exchange", TokenURL: tokenSrv.URL,
			ClientID: "veto", ClientSecretEnv: "VETO_SECRET", Audience: "https://api.example",
			Subject: "invoke",
		}}, Dir: t.TempDir(), HTTP: tokenSrv.Client()}),
	})
	require.NoError(t, err)
	loop.Policy = policy.Builtin{Allow: map[string]bool{}}
	call, err := loop.Invoke(auth.WithUserToken(context.Background(), "caller-token"), "orders.get", map[string]string{"id": "1"}, "")
	require.NoError(t, err)
	assert.Equal(t, "denied", call.Status)
	assert.Equal(t, int32(0), tokenHits.Load())
	assert.Equal(t, int32(0), upstreamHits.Load())
}

type signProvider struct {
	err error
}

func (p signProvider) Resolve(context.Context, credentials.Request) (credentials.Credential, error) {
	return credentials.Credential{
		Query: map[string]string{"api_key": "q-secret"},
		Sign: func(r *http.Request) error {
			if p.err != nil {
				return p.err
			}
			if r.URL.Query().Get("api_key") == "" {
				return stringError("query was not on the request")
			}
			r.Header.Set("X-Signed", "yes")
			return nil
		},
	}, nil
}

func TestProviderSignRunsAfterTheRequestIsBuilt(t *testing.T) {
	var gotKey, gotSigned string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.URL.Query().Get("api_key")
		gotSigned = r.Header.Get("X-Signed")
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()
	creds := auth.New(auth.Options{Dir: t.TempDir()})
	creds.SetProvider("queryAuth", signProvider{})
	_, err := (execute.Client{BaseURL: up.URL, Creds: creds}).InvokeHTTPResult(context.Background(), loadSpec(t, querySpec).ByID("orders.search"), nil)
	require.NoError(t, err)
	assert.Equal(t, "q-secret", gotKey)
	assert.Equal(t, "yes", gotSigned)
}

func TestProviderSignErrorSkipsUpstream(t *testing.T) {
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	defer up.Close()
	creds := auth.New(auth.Options{Dir: t.TempDir()})
	creds.SetProvider("queryAuth", signProvider{err: stringError("sign failed")})
	_, err := (execute.Client{BaseURL: up.URL, Creds: creds}).InvokeHTTPResult(context.Background(), loadSpec(t, querySpec).ByID("orders.search"), nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "sign failed")
	assert.Equal(t, int32(0), hits.Load())
}
