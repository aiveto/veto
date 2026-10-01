package execute

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type staticProvider struct{}

func (staticProvider) Resolve(context.Context, credentials.Request) (credentials.Credential, error) {
	return credentials.Credential{Headers: map[string]string{"Authorization": "Basic dGVzdA=="}}, nil
}

func TestCredentialedRedirectStaysOnOrigin(t *testing.T) {
	var leaked atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Key") != "" {
			leaked.Store(true)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(other.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/landed" {
			assert.Equal(t, "secret", r.Header.Get("X-Key"))
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Redirect(w, r, other.URL+"/stolen", http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	op := &catalog.Operation{
		ID: "orders.get", Method: http.MethodGet, PathTemplate: "/orders/{id}",
		Params: []catalog.Param{{Name: "id", In: "path", Required: true}},
		Auth:   []catalog.Auth{{Name: "key", Header: "X-Key", Kind: "apiKey"}},
	}
	cfg := Config{
		BaseURL: origin.URL,
		Client:  origin.Client(),
		Auth:    map[string]string{"key": "secret"},
	}
	resp, err := InvokeResponse(t.Context(), cfg, op, map[string]string{"id": "1"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusFound, resp.StatusCode)
	assert.False(t, leaked.Load())

	same := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/landed" {
			assert.Equal(t, "secret", r.Header.Get("X-Key"))
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Redirect(w, r, "/landed", http.StatusFound)
	}))
	t.Cleanup(same.Close)
	cfg.BaseURL = same.URL
	cfg.Client = same.Client()
	resp, err = InvokeResponse(t.Context(), cfg, op, map[string]string{"id": "1"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	cfg.FollowRedirects = true
	cfg.BaseURL = origin.URL
	cfg.Client = origin.Client()
	resp, err = InvokeResponse(t.Context(), cfg, op, map[string]string{"id": "1"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.True(t, leaked.Load())
}

func TestRegisteredProviderAdaptsAnUnsupportedScheme(t *testing.T) {
	basic := catalog.Auth{Name: "basic", Kind: "unsupported", Type: "http", Scheme: "basic"}
	err := requirementError(t.Context(), Config{}, []catalog.Auth{basic})
	require.ErrorContains(t, err, "not supported")
	r := auth.New(auth.Options{Dir: t.TempDir()})
	r.SetProvider("basic", staticProvider{})
	require.NoError(t, requirementError(t.Context(), Config{Creds: r}, []catalog.Auth{basic}))
}
