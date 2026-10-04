package auth

import (
	"context"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceReadyAndBlockers(t *testing.T) {
	a := catalog.Auth{Name: "bearerAuth"}
	cases := []struct {
		name      string
		scheme    Scheme
		env       map[string]string
		ready     bool
		refresh   bool
		unset     string
		blockers  []string
		withToken bool
	}{
		{
			name:     "env unset",
			scheme:   Scheme{Name: "bearerAuth", Source: "env", Env: "ORDER_TOKEN"},
			unset:    "bearerAuth is unset",
			blockers: []string{"ORDER_TOKEN is unset"},
		},
		{
			name:    "env set",
			scheme:  Scheme{Name: "bearerAuth", Source: "env", Env: "ORDER_TOKEN"},
			env:     map[string]string{"ORDER_TOKEN": "s3cret"},
			ready:   true,
			unset:   "bearerAuth is unset",
			refresh: false,
		},
		{
			name:     "login missing",
			scheme:   Scheme{Name: "bearerAuth", Source: "login"},
			unset:    "bearerAuth has no stored token",
			refresh:  true,
			blockers: []string{"auth scheme bearerAuth has no stored token"},
		},
		{
			name:     "client secret unset",
			scheme:   Scheme{Name: "workforce", Source: "client_credentials", TokenURL: "http://example.test/token", ClientID: "job", ClientSecretEnv: "WORKFORCE_SECRET"},
			unset:    "workforce client secret is unset",
			refresh:  true,
			blockers: []string{"WORKFORCE_SECRET is unset"},
		},
		{
			name:    "client secret set",
			scheme:  Scheme{Name: "workforce", Source: "client_credentials", TokenURL: "http://example.test/token", ClientID: "job", ClientSecretEnv: "WORKFORCE_SECRET"},
			env:     map[string]string{"WORKFORCE_SECRET": "s3cret"},
			ready:   true,
			unset:   "workforce client secret is unset",
			refresh: true,
		},
		{
			name:    "invoke without token",
			scheme:  Scheme{Name: "bearerAuth", Source: "invoke"},
			unset:   "bearerAuth is unset",
			refresh: false,
		},
		{
			name:      "invoke with token",
			scheme:    Scheme{Name: "bearerAuth", Source: "invoke"},
			ready:     true,
			unset:     "bearerAuth is unset",
			withToken: true,
		},
		{
			name:     "command missing",
			scheme:   Scheme{Name: "bearerAuth", Source: "command"},
			unset:    "bearerAuth is unset",
			refresh:  true,
			blockers: []string{"auth scheme bearerAuth has no command"},
		},
		{
			name:    "command set",
			scheme:  Scheme{Name: "bearerAuth", Source: "command", Command: []string{"true"}},
			ready:   true,
			unset:   "bearerAuth is unset",
			refresh: true,
		},
		{
			name:     "exchange secret unset",
			scheme:   Scheme{Name: "ex", Source: "token_exchange", TokenURL: "http://example.test/token", ClientID: "job", ClientSecretEnv: "EX_SECRET", Subject: "login"},
			unset:    "ex client secret is unset",
			refresh:  true,
			blockers: []string{"EX_SECRET is unset"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := New(Options{
				Schemes: []Scheme{tc.scheme},
				Dir:     t.TempDir(),
				Env: func(key string) string {
					return tc.env[key]
				},
			})
			ctx := context.Background()
			if tc.withToken {
				ctx = WithUserToken(ctx, "person")
			}
			auth := a
			auth.Name = tc.scheme.Name
			assert.Equal(t, tc.ready, r.Ready(ctx, auth))
			assert.Equal(t, tc.refresh, r.Refreshable(tc.scheme.Name))
			require.EqualError(t, r.UnsetError(auth), tc.unset)
			assert.Equal(t, tc.blockers, r.Blockers(auth))
		})
	}
}

func TestLoginSuppliesUserHeader(t *testing.T) {
	r := New(Options{
		Schemes: []Scheme{
			{Name: "login", Source: "login", UserHeader: "X-User"},
			{Name: "open", Source: "login"},
			{Name: "env", Source: "env", Env: "TOKEN"},
		},
		Dir: t.TempDir(),
	})
	assert.True(t, r.SuppliesUserHeader("login", "X-User"))
	assert.True(t, r.SuppliesUserHeader("open", "Authorization"))
	assert.False(t, r.SuppliesUserHeader("login", "Authorization"))
	assert.False(t, r.SuppliesUserHeader("env", "X-User"))
}

func TestInvokeBlockersStayEmptyWithoutToken(t *testing.T) {
	r := New(Options{
		Schemes: []Scheme{{Name: "bearerAuth", Source: "invoke"}},
		Dir:     t.TempDir(),
	})
	a := catalog.Auth{Name: "bearerAuth"}
	assert.False(t, r.Ready(context.Background(), a))
	assert.Empty(t, r.Blockers(a))
}
