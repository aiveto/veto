package auth_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandExpiresAtStaysOnThatURL(t *testing.T) {
	hits := filepath.Join(t.TempDir(), "hits")
	script := fmt.Sprintf(`import json,sys
doc=json.load(sys.stdin)
p=%q
n=0
try:
    n=int(open(p).read() or "0")
except FileNotFoundError:
    pass
open(p,"w").write(str(n+1))
json.dump({"headers":{"X-Sig":doc["url"]},"expires_at":"2099-01-01T00:00:00Z"}, sys.stdout)
`, hits)
	resolver := auth.New(auth.Options{
		Schemes: []auth.Scheme{{
			Name:    "sig",
			Source:  "command",
			Command: []string{"python3", "-c", script},
		}},
		Dir: t.TempDir(),
	})
	op := &catalog.Operation{ID: "orders.get", Method: http.MethodGet}
	scheme := catalog.Auth{Name: "sig", Kind: "apiKey", Header: "X-Sig"}
	const firstURL = "https://api.example/orders/1"
	const otherURL = "https://api.example/orders/2"
	ctx := context.Background()

	first, err := resolver.Material(ctx, op, scheme, http.MethodGet, firstURL, false)
	require.NoError(t, err)
	again, err := resolver.Material(ctx, op, scheme, http.MethodGet, firstURL, false)
	require.NoError(t, err)
	other, err := resolver.Material(ctx, op, scheme, http.MethodGet, otherURL, false)
	require.NoError(t, err)

	assert.Equal(t, firstURL, first.Headers["X-Sig"])
	assert.Equal(t, first.Headers, again.Headers)
	assert.Equal(t, otherURL, other.Headers["X-Sig"])
	raw, err := os.ReadFile(hits)
	require.NoError(t, err)
	assert.Equal(t, "2", string(raw))
}

func TestCommandExpiresAtFollowsMethodAndUserToken(t *testing.T) {
	hits := filepath.Join(t.TempDir(), "hits")
	script := fmt.Sprintf(`import json,sys
doc=json.load(sys.stdin)
p=%q
n=0
try:
    n=int(open(p).read() or "0")
except FileNotFoundError:
    pass
open(p,"w").write(str(n+1))
json.dump({"headers":{"X-Method":doc["method"],"X-User":doc.get("user_token","")},"expires_at":"2099-01-01T00:00:00Z"}, sys.stdout)
`, hits)
	resolver := auth.New(auth.Options{
		Schemes: []auth.Scheme{{
			Name:    "sig",
			Source:  "command",
			Command: []string{"python3", "-c", script},
		}},
		Dir: t.TempDir(),
	})
	op := &catalog.Operation{ID: "orders.get", Method: http.MethodGet}
	scheme := catalog.Auth{Name: "sig", Kind: "apiKey", Header: "X-Sig"}
	const endpoint = "https://api.example/orders/1"
	alice := auth.WithUserToken(context.Background(), "alice-token")
	bob := auth.WithUserToken(context.Background(), "bob-token")

	first, err := resolver.Material(alice, op, scheme, http.MethodGet, endpoint, false)
	require.NoError(t, err)
	again, err := resolver.Material(alice, op, scheme, http.MethodGet, endpoint, false)
	require.NoError(t, err)
	otherUser, err := resolver.Material(bob, op, scheme, http.MethodGet, endpoint, false)
	require.NoError(t, err)
	deleted, err := resolver.Material(bob, op, scheme, http.MethodDelete, endpoint, false)
	require.NoError(t, err)
	deletedAgain, err := resolver.Material(bob, op, scheme, http.MethodDelete, endpoint, false)
	require.NoError(t, err)

	assert.Equal(t, http.MethodGet, first.Headers["X-Method"])
	assert.Equal(t, "alice-token", first.Headers["X-User"])
	assert.Equal(t, first.Headers, again.Headers)
	assert.Equal(t, "bob-token", otherUser.Headers["X-User"])
	assert.Equal(t, http.MethodDelete, deleted.Headers["X-Method"])
	assert.Equal(t, "bob-token", deleted.Headers["X-User"])
	assert.Equal(t, deleted.Headers, deletedAgain.Headers)
	raw, err := os.ReadFile(hits)
	require.NoError(t, err)
	assert.Equal(t, "3", string(raw))
}

func TestCommandStdinOmitsTheBody(t *testing.T) {
	cases := []struct {
		name string
		user string
		want []string
	}{
		{name: "no user token", want: []string{"method", "operation_id", "scheme", "url"}},
		{name: "user token", user: "alice-token", want: []string{"method", "operation_id", "scheme", "url", "user_token"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keys := filepath.Join(t.TempDir(), "keys")
			script := fmt.Sprintf(`import json,sys
doc=json.load(sys.stdin)
open(%q,"w").write(json.dumps(sorted(doc.keys())))
json.dump({"headers":{"X-Sig":"ok"},"expires_at":"2099-01-01T00:00:00Z"}, sys.stdout)
`, keys)
			resolver := auth.New(auth.Options{
				Schemes: []auth.Scheme{{
					Name:    "sig",
					Source:  "command",
					Command: []string{"python3", "-c", script},
				}},
				Dir: t.TempDir(),
			})
			ctx := context.Background()
			if tc.user != "" {
				ctx = auth.WithUserToken(ctx, tc.user)
			}
			_, err := resolver.Material(ctx, &catalog.Operation{ID: "orders.create", Method: http.MethodPost}, catalog.Auth{Name: "sig", Kind: "apiKey", Header: "X-Sig"}, http.MethodPost, "https://api.example/orders", false)
			require.NoError(t, err)
			raw, err := os.ReadFile(keys)
			require.NoError(t, err)
			var got []string
			require.NoError(t, json.Unmarshal(raw, &got))
			assert.Equal(t, tc.want, got)
		})
	}
}
