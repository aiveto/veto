package auth_test

import (
	"net/http"
	"os"
	"testing"

	"github.com/aiveto/veto/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	// ProxyFromEnvironment reads the environment once per process.
	if err := os.Setenv("HTTPS_PROXY", "http://127.0.0.1:9"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestCustomTransportHonorsHTTPSProxy(t *testing.T) {
	base := &http.Transport{}
	client := auth.WithEnvProxy(&http.Client{Transport: base})
	tr, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	assert.NotNil(t, tr.Proxy)
	assert.Nil(t, base.Proxy)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.example/orders", nil)
	require.NoError(t, err)
	got, err := tr.Proxy(req)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "http://127.0.0.1:9", got.String())
}

func TestRoundTripperWithoutProxyStillRuns(t *testing.T) {
	var called bool
	next := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		return nil, errString("stopped")
	})
	client := auth.WithEnvProxy(&http.Client{Transport: next})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://127.0.0.1/orders", nil)
	require.NoError(t, err)
	resp, err := client.Transport.RoundTrip(req)
	if resp != nil && resp.Body != nil {
		assert.NoError(t, resp.Body.Close())
	}
	assert.Error(t, err)
	assert.True(t, called)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type errString string

func (e errString) Error() string { return string(e) }
