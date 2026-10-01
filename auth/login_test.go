package auth_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aiveto/veto/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssuerDiscoveryAndDeviceLogin(t *testing.T) {
	cases := []struct {
		name   string
		device bool
	}{
		{name: "issuer", device: false},
		{name: "device", device: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/.well-known/openid-configuration":
					_ = json.NewEncoder(w).Encode(map[string]string{
						"authorization_endpoint":        srv.URL + "/authorize",
						"token_endpoint":                srv.URL + "/token",
						"device_authorization_endpoint": srv.URL + "/device",
					})
				case "/device":
					_ = json.NewEncoder(w).Encode(map[string]string{
						"device_code":      "device-secret",
						"user_code":        "ABCD",
						"verification_uri": srv.URL + "/device/verify",
					})
				case "/token":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"access_token":  "access",
						"refresh_token": "refresh-secret",
						"expires_in":    3600,
					})
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			dir := t.TempDir()
			var opened []string
			err := auth.Login(context.Background(), auth.LoginOptions{
				Scheme: auth.Scheme{
					Name:     "user",
					ClientID: "veto",
					Issuer:   srv.URL,
					Scopes:   []string{"orders.read"},
				},
				Dir:         dir,
				Device:      tc.device,
				RedirectURL: "http://127.0.0.1:0/callback",
				HTTP:        srv.Client(),
				Out:         io.Discard,
				Open: func(ctx context.Context, raw string) error {
					opened = append(opened, raw)
					return driveCallback(ctx, raw)
				},
			})
			require.NoError(t, err)
			if tc.device {
				assert.Empty(t, opened)
			} else {
				require.Len(t, opened, 1)
				assert.Contains(t, opened[0], srv.URL+"/authorize")
			}
			st, err := os.Stat(filepath.Join(dir, "user.json"))
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())
			raw, err := os.ReadFile(filepath.Join(dir, "user.json"))
			require.NoError(t, err)
			assert.Contains(t, string(raw), "refresh-secret")
		})
	}
}

func driveCallback(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	cb, err := url.Parse(u.Query().Get("redirect_uri"))
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("code", "abc")
	q.Set("state", u.Query().Get("state"))
	cb.RawQuery = q.Encode()
	var resp *http.Response
	for range 20 {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, cb.String(), nil)
		if reqErr != nil {
			return reqErr
		}
		resp, err = http.DefaultClient.Do(req)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		return err
	}
	_, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if readErr != nil {
		return readErr
	}
	return closeErr
}
