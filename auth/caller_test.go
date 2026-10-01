package auth_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type seqProvider struct {
	n atomic.Int32
}

func (p *seqProvider) Resolve(context.Context, credentials.Request) (credentials.Credential, error) {
	token := "token-ada"
	if p.n.Add(1) > 1 {
		token = "token-grace"
	}
	return credentials.Credential{
		Headers:   map[string]string{"Authorization": "Bearer " + token},
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func TestProviderOutputIsNotReused(t *testing.T) {
	src := &seqProvider{}
	r := auth.New(auth.Options{})
	r.SetProvider("userAuth", src)
	p, ok := r.Provider(catalog.Auth{Name: "userAuth"})
	require.True(t, ok)
	ada := auth.WithCaller(t.Context(), "ada")
	grace := auth.WithCaller(t.Context(), "grace")
	first, err := p.Resolve(ada, credentials.Request{Scheme: "userAuth", URL: "http://a.example"})
	require.NoError(t, err)
	second, err := p.Resolve(ada, credentials.Request{Scheme: "userAuth", URL: "http://b.example"})
	require.NoError(t, err)
	other, err := p.Resolve(grace, credentials.Request{Scheme: "userAuth", URL: "http://c.example"})
	require.NoError(t, err)
	assert.Equal(t, "Bearer token-ada", first.Headers["Authorization"])
	assert.Equal(t, "Bearer token-grace", second.Headers["Authorization"])
	assert.NotEqual(t, first.Headers["Authorization"], other.Headers["Authorization"])
	assert.Equal(t, int32(3), src.n.Load())
}
