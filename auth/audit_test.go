package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingProvider struct {
	n    int
	last credentials.Request
}

func (p *countingProvider) Resolve(_ context.Context, in credentials.Request) (credentials.Credential, error) {
	p.n++
	p.last = in
	token := in.UserToken + " " + in.URL + " " + in.Method + " " + in.OperationID + " " + in.Audience
	return credentials.Credential{
		Headers:   map[string]string{"Authorization": token},
		ExpiresAt: time.Now().Add(time.Hour),
		Sign: func(r *http.Request) error {
			r.Header.Set("X-Op", in.OperationID)
			return nil
		},
	}, nil
}

func TestExtraProviderDoesNotCacheAcrossRequests(t *testing.T) {
	r := New(Options{Dir: t.TempDir()})
	src := &countingProvider{}
	r.SetProvider("custom", src)
	p, ok := r.Provider(catalog.Auth{Name: "custom", Kind: "unsupported", Type: "http", Scheme: "basic"})
	require.True(t, ok)
	first, err := p.Resolve(t.Context(), credentials.Request{
		OperationID: "a", Method: http.MethodGet, URL: "http://a.example/1", UserToken: "person-a", Audience: "aud-a",
	})
	require.NoError(t, err)
	second, err := p.Resolve(t.Context(), credentials.Request{
		OperationID: "b", Method: http.MethodPost, URL: "http://b.example/2", UserToken: "person-b", Audience: "aud-b",
	})
	require.NoError(t, err)
	assert.Equal(t, 2, src.n)
	assert.NotEqual(t, first.Headers["Authorization"], second.Headers["Authorization"])
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://a.example", nil)
	require.NoError(t, err)
	require.NoError(t, first.Sign(req))
	assert.Equal(t, "a", req.Header.Get("X-Op"))
	req, err = http.NewRequestWithContext(t.Context(), http.MethodGet, "http://b.example", nil)
	require.NoError(t, err)
	require.NoError(t, second.Sign(req))
	assert.Equal(t, "b", req.Header.Get("X-Op"))
}

func TestExtraProviderKeepsCatalogScopesAheadOfSchemeDefaults(t *testing.T) {
	r := New(Options{
		Dir: t.TempDir(),
		Schemes: []Scheme{{
			Name: "api", Source: "env", Scopes: []string{"scheme.read"}, Audience: "scheme-aud",
		}},
	})
	src := &countingProvider{}
	r.SetProvider("api", src)
	p, ok := r.Provider(catalog.Auth{Name: "api", Scopes: []string{"orders.write"}})
	require.True(t, ok)

	_, err := p.Resolve(t.Context(), credentials.Request{})
	require.NoError(t, err)
	assert.Equal(t, []string{"orders.write"}, src.last.Scopes)
	assert.Equal(t, "scheme-aud", src.last.Audience)

	_, err = p.Resolve(t.Context(), credentials.Request{Scopes: []string{"caller.scope"}, Audience: "caller-aud"})
	require.NoError(t, err)
	assert.Equal(t, []string{"caller.scope"}, src.last.Scopes)
	assert.Equal(t, "caller-aud", src.last.Audience)

	empty, ok := r.Provider(catalog.Auth{Name: "api"})
	require.True(t, ok)
	_, err = empty.Resolve(t.Context(), credentials.Request{})
	require.NoError(t, err)
	assert.Equal(t, []string{"scheme.read"}, src.last.Scopes)
	assert.Equal(t, "scheme-aud", src.last.Audience)
}

func TestFlightDoReturnsWhenTheWaiterIsCanceled(t *testing.T) {
	f := &flight{}
	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_, _ = f.Do(context.Background(), "k", func() (Material, error) {
			close(started)
			<-release
			return Material{Headers: map[string]string{"Authorization": "leader"}}, nil
		})
	}()
	<-started
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := f.Do(ctx, "k", func() (Material, error) {
			return Material{}, nil
		})
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("follower blocked")
	}
	close(release)
}

func TestPostFormDoesNotFollowRedirects(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/token" {
			http.Redirect(w, r, "/stolen", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	_, err := postForm(t.Context(), srv.Client(), srv.URL+"/token", url.Values{"client_secret": {"sekret"}}, []string{"sekret"})
	require.Error(t, err)
	assert.Equal(t, []string{"/token"}, paths)
}

func TestSubjectTokenRefreshesAnExpiredLogin(t *testing.T) {
	var grants atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		grants.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "refreshed-access",
			"refresh_token": "refresh-secret",
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	body, err := json.Marshal(storedToken{
		AccessToken:  "old-access",
		RefreshToken: "refresh-secret",
		ExpiresAt:    time.Now().Add(-time.Minute),
		TokenURL:     srv.URL,
		ClientID:     "veto",
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "login.json"), append(body, '\n'), 0o600))
	r := New(Options{
		Dir: dir,
		Schemes: []Scheme{{
			Name: "api", Source: "token_exchange", Subject: "login",
			TokenURL: srv.URL, ClientID: "api", ClientSecretEnv: "API_SECRET",
		}},
		Env: func(k string) string {
			if k == "API_SECRET" {
				return "sekret"
			}
			return ""
		},
		HTTP: srv.Client(),
	})
	got := r.subjectToken(t.Context(), r.schemes["api"])
	assert.Equal(t, "refreshed-access", got)
	assert.Equal(t, int32(1), grants.Load())
}
