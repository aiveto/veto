package credentials_test

import (
	"context"
	"testing"

	"github.com/aiveto/veto/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderFuncSatisfiesProvider(t *testing.T) {
	var got credentials.Request
	p := credentials.ProviderFunc(func(ctx context.Context, in credentials.Request) (credentials.Credential, error) {
		got = in
		return credentials.Credential{Headers: map[string]string{"Authorization": "Bearer x"}}, nil
	})
	cred, err := p.Resolve(t.Context(), credentials.Request{Scheme: "internal", URL: "http://api.example"})
	require.NoError(t, err)
	assert.Equal(t, "internal", got.Scheme)
	assert.Equal(t, "Bearer x", cred.Headers["Authorization"])
}
