package execute_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMissingCredentialSkipsHTTP(t *testing.T) {
	cases := []struct {
		name   string
		scheme auth.Scheme
		spec   string
		op     string
	}{
		{
			name:   "client secret",
			scheme: auth.Scheme{Name: "workforce", Source: "client_credentials", TokenURL: "http://127.0.0.1:9/token", ClientID: "job", ClientSecretEnv: "WORKFORCE_SECRET"},
			spec:   bearerSpec,
			op:     "orders.get",
		},
		{
			name:   "env",
			scheme: auth.Scheme{Name: "bearerAuth", Source: "env", Env: "ORDER_TOKEN"},
			spec:   bearerSpec,
			op:     "orders.get",
		},
		{
			name:   "stored login",
			scheme: auth.Scheme{Name: "bearerAuth", Source: "login", ClientID: "veto", TokenURL: "http://127.0.0.1:9/token"},
			spec:   bearerSpec,
			op:     "orders.get",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("WORKFORCE_SECRET", "")
			t.Setenv("ORDER_TOKEN", "")
			var tokenHits, upstreamHits atomic.Int32
			tokenSrv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				tokenHits.Add(1)
			}))
			defer tokenSrv.Close()
			up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				upstreamHits.Add(1)
			}))
			defer up.Close()
			tc.scheme.TokenURL = tokenSrv.URL
			op := loadSpec(t, tc.spec).ByID(tc.op)
			_, err := (execute.Client{
				BaseURL: up.URL,
				Creds: auth.New(auth.Options{
					Schemes: []auth.Scheme{tc.scheme},
					Dir:     t.TempDir(),
					HTTP:    tokenSrv.Client(),
				}),
			}).InvokeHTTPResult(context.Background(), op, map[string]string{"id": "1"})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "WORKFORCE_SECRET")
			assert.Equal(t, int32(0), tokenHits.Load())
			assert.Equal(t, int32(0), upstreamHits.Load())
		})
	}
}

func TestTwoSchemesSetTwoHeaders(t *testing.T) {
	op := loadSpec(t, twoSchemeSpec).ByID("orders.get")
	var authz, apiKey string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authz = r.Header.Get("Authorization")
		apiKey = r.Header.Get("X-Api-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()
	t.Setenv("USER_TOKEN", "user-token")
	t.Setenv("API_TOKEN", "api-token")
	_, err := (execute.Client{
		BaseURL: up.URL,
		Creds: auth.New(auth.Options{Schemes: []auth.Scheme{
			{Name: "userAuth", Source: "env", Env: "USER_TOKEN"},
			{Name: "apiAuth", Source: "env", Env: "API_TOKEN"},
		}, Dir: t.TempDir()}),
	}).InvokeHTTPResult(context.Background(), op, map[string]string{"id": "1"})
	require.NoError(t, err)
	assert.Equal(t, "Bearer user-token", authz)
	assert.Equal(t, "api-token", apiKey)
}

func TestEmptySecuritySendsNothing(t *testing.T) {
	op := loadSpec(t, bearerSpec).ByID("orders.health")
	if op == nil {
		op = loadSpec(t, publicSpec).ByID("orders.health")
	}
	var authz string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authz = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()
	_, err := (execute.Client{
		BaseURL: up.URL,
		Auth:    map[string]string{"bearerAuth": "nope"},
		Creds:   auth.New(auth.Options{Schemes: []auth.Scheme{{Name: "bearerAuth", Source: "env", Env: "ORDER_TOKEN"}}, Dir: t.TempDir()}),
	}).InvokeHTTPResult(context.Background(), op, nil)
	require.NoError(t, err)
	assert.Empty(t, authz)
}

func TestQueryKeyIsScrubbedFromTheErrorAndTrace(t *testing.T) {
	op := loadSpec(t, querySpec).ByID("orders.search")
	const secret = "query-secret-value"
	rec, err := telemetry.Record()
	require.NoError(t, err)
	defer func() { require.NoError(t, rec.Stop(context.Background())) }()
	resp, err := execute.InvokeResponse(context.Background(), execute.Client{
		BaseURL: "http://127.0.0.1:9",
		HTTP:    &http.Client{Transport: errTrip{}},
		Auth:    map[string]string{"queryAuth": secret},
	}, op, nil)
	if resp != nil && resp.Body != nil {
		require.NoError(t, resp.Body.Close())
	}
	require.Error(t, err)
	assert.NotContains(t, err.Error(), secret)
	assert.NotContains(t, spanText(t, rec), secret)
}

func TestParallelClientCredentialsHitTheTokenURLOnce(t *testing.T) {
	var hits atomic.Int32
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(40 * time.Millisecond)
		_ = r.ParseForm()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "minted-" + r.Form.Get("audience"),
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer tokenSrv.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()
	t.Setenv("WORKFORCE_SECRET", "super-secret")
	op := loadSpec(t, bearerSpec).ByID("orders.get")
	client := execute.Client{
		BaseURL: up.URL,
		Creds: auth.New(auth.Options{
			Schemes: []auth.Scheme{{
				Name: "bearerAuth", Source: "client_credentials", TokenURL: tokenSrv.URL,
				ClientID: "job", ClientSecretEnv: "WORKFORCE_SECRET", Audience: "https://api.example",
				Scopes: []string{"orders.read"},
			}},
			Dir:  t.TempDir(),
			HTTP: tokenSrv.Client(),
		}),
	}
	const n = 8
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for range n {
		wg.Go(func() {
			_, err := client.InvokeHTTPResult(context.Background(), op, map[string]string{"id": "1"})
			if err != nil {
				errCh <- err
			}
		})
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), hits.Load())
}

func TestDeniedCallSkipsTokenAndUpstream(t *testing.T) {
	var tokenHits, upstreamHits atomic.Int32
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer tokenSrv.Close()
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamHits.Add(1)
	}))
	defer up.Close()
	t.Setenv("WORKFORCE_SECRET", "super-secret")
	cat := loadSpec(t, bearerSpec)
	op := cat.ByID("orders.get")
	op.Permissions = []string{"orders.read"}
	loop, err := agent.New(cat, nil, execute.Client{
		BaseURL: up.URL,
		Creds: auth.New(auth.Options{Schemes: []auth.Scheme{{
			Name: "bearerAuth", Source: "client_credentials", TokenURL: tokenSrv.URL,
			ClientID: "job", ClientSecretEnv: "WORKFORCE_SECRET",
		}}, Dir: t.TempDir(), HTTP: tokenSrv.Client()}),
	})
	require.NoError(t, err)
	loop.Policy = policy.Builtin{Allow: map[string]bool{}}
	call, err := loop.Invoke(context.Background(), "orders.get", map[string]string{"id": "1"}, "")
	require.NoError(t, err)
	assert.Equal(t, "denied", call.Status)
	assert.Equal(t, int32(0), tokenHits.Load())
	assert.Equal(t, int32(0), upstreamHits.Load())
}

func TestFollowHopUsesTheTargetScheme(t *testing.T) {
	var tokenHits atomic.Int32
	var mu sync.Mutex
	got := map[string]string{}
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenHits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "work-token", "expires_in": 3600})
	}))
	defer tokenSrv.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got[r.URL.Path] = r.Header.Get("Authorization")
		mu.Unlock()
		if r.URL.Path == "/orders/123" {
			_, _ = w.Write([]byte(`{"customerId":"7"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"7"}`))
	}))
	defer up.Close()
	t.Setenv("USER_TOKEN", "user-token")
	t.Setenv("WORKFORCE_SECRET", "super-secret")
	cat := loadSpec(t, followAuthSpec)
	loop, err := agent.New(cat, nil, execute.Client{
		BaseURL: up.URL,
		Creds: auth.New(auth.Options{Schemes: []auth.Scheme{
			{Name: "userAuth", Source: "env", Env: "USER_TOKEN"},
			{Name: "workforce", Source: "client_credentials", TokenURL: tokenSrv.URL, ClientID: "job", ClientSecretEnv: "WORKFORCE_SECRET", Audience: "https://customers.example"},
		}, Dir: t.TempDir(), HTTP: tokenSrv.Client()}),
	})
	require.NoError(t, err)
	calls, err := loop.Follow(context.Background(), "orders.get", map[string]string{"id": "123"}, "")
	require.NoError(t, err)
	require.Len(t, calls, 2)
	assert.Equal(t, "Bearer user-token", got["/orders/123"])
	assert.Equal(t, "Bearer work-token", got["/customers/7"])
	assert.NotContains(t, got["/customers/7"], "user-token")
	assert.Equal(t, int32(1), tokenHits.Load())
}

func TestTokenStaysOffTheTrace(t *testing.T) {
	const access = "access-token-value"
	const secret = "client-secret-value"
	const query = "query-secret-value"
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": access, "expires_in": 3600, "refresh_token": "refresh-token-value"})
	}))
	defer tokenSrv.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()
	t.Setenv("WORKFORCE_SECRET", secret)
	t.Setenv("QUERY_TOKEN", query)
	rec, err := telemetry.Record()
	require.NoError(t, err)
	defer func() { require.NoError(t, rec.Stop(context.Background())) }()
	creds := auth.New(auth.Options{Schemes: []auth.Scheme{
		{Name: "bearerAuth", Source: "client_credentials", TokenURL: tokenSrv.URL, ClientID: "job", ClientSecretEnv: "WORKFORCE_SECRET"},
		{Name: "queryAuth", Source: "env", Env: "QUERY_TOKEN"},
	}, Dir: t.TempDir(), HTTP: tokenSrv.Client()})
	_, err = (execute.Client{BaseURL: up.URL, Creds: creds}).InvokeHTTPResult(context.Background(), loadSpec(t, bearerSpec).ByID("orders.get"), map[string]string{"id": "1"})
	require.NoError(t, err)
	_, err = (execute.Client{BaseURL: up.URL, Creds: creds}).InvokeHTTPResult(context.Background(), loadSpec(t, querySpec).ByID("orders.search"), nil)
	require.NoError(t, err)
	text := spanText(t, rec)
	assert.NotContains(t, text, access)
	assert.NotContains(t, text, secret)
	assert.NotContains(t, text, query)
	assert.NotContains(t, text, "refresh-token-value")
}

func TestLoginStoresRefreshAndInvokeRefreshesWithoutABrowser(t *testing.T) {
	var grants []string
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		grants = append(grants, form.Get("grant_type"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-" + form.Get("grant_type"),
			"refresh_token": "refresh-token-value",
			"expires_in":    3600,
		})
	}))
	defer tokenSrv.Close()
	var opens atomic.Int32
	dir := t.TempDir()
	err := auth.Login(context.Background(), auth.LoginOptions{
		Scheme: auth.Scheme{
			Name:             "userAuth",
			Source:           "login",
			ClientID:         "veto",
			AuthorizationURL: "http://idp.example/authorize",
			TokenURL:         tokenSrv.URL,
			Scopes:           []string{"orders.read"},
		},
		Dir:         dir,
		RedirectURL: "http://127.0.0.1:0/callback",
		HTTP:        tokenSrv.Client(),
		Out:         io.Discard,
		Open: func(ctx context.Context, raw string) error {
			opens.Add(1)
			return finishCodeLogin(ctx, raw)
		},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), opens.Load())
	path := filepath.Join(dir, "userAuth.json")
	st, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var stored struct {
		Refresh   string    `json:"refresh_token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	require.NoError(t, json.Unmarshal(raw, &stored))
	assert.Equal(t, "refresh-token-value", stored.Refresh)
	stored.ExpiresAt = time.Now().Add(-time.Hour)
	rewritten, err := json.Marshal(stored)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, rewritten, 0o600))

	var got string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()
	_, err = (execute.Client{
		BaseURL: up.URL,
		Creds: auth.New(auth.Options{Schemes: []auth.Scheme{{
			Name: "userAuth", Source: "login", ClientID: "veto", TokenURL: tokenSrv.URL,
		}}, Dir: dir, HTTP: tokenSrv.Client()}),
	}).InvokeHTTPResult(context.Background(), loadSpec(t, userSpec).ByID("orders.get"), map[string]string{"id": "1"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), opens.Load())
	assert.Equal(t, []string{"authorization_code", "refresh_token"}, grants)
	assert.Equal(t, "Bearer access-refresh_token", got)
	assert.NotContains(t, got, "refresh-token-value")
}

func TestCommandHookFailuresSkipUpstream(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want string
	}{
		{name: "exit", cmd: "printf 'secret-stdout'; exit 2", want: "exited 2"},
		{name: "bad json", cmd: "printf 'not-json'", want: "invalid JSON"},
		{name: "timeout", cmd: "sleep 5", want: "timed out"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				hits.Add(1)
			}))
			defer up.Close()
			op := loadSpec(t, bearerSpec).ByID("orders.get")
			_, err := (execute.Client{
				BaseURL: up.URL,
				Creds: auth.New(auth.Options{Schemes: []auth.Scheme{{
					Name: "bearerAuth", Source: "command", Command: []string{"sh", "-c", tc.cmd}, Timeout: 200 * time.Millisecond,
				}}, Dir: t.TempDir()}),
			}).InvokeHTTPResult(context.Background(), op, map[string]string{"id": "1"})
			require.Error(t, err)
			require.ErrorContains(t, err, tc.want)
			assert.NotContains(t, err.Error(), "secret-stdout")
			require.Equal(t, int32(0), hits.Load())
		})
	}
}

func finishCodeLogin(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		return stringError("missing pkce")
	}
	cb, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		return err
	}
	cb.RawQuery = url.Values{"code": {"abc"}, "state": {q.Get("state")}}.Encode()
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

type stringError string

func (e stringError) Error() string { return string(e) }

type errTrip struct{}

func (errTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, stringError("Get \"" + req.URL.String() + "\": refused")
}

const twoSchemeSpec = `openapi: 3.0.3
info: {title: Orders, version: "1"}
servers:
  - url: http://127.0.0.1:9
paths:
  /orders/{id}:
    get:
      operationId: orders.get
      security:
        - userAuth: []
          apiAuth: []
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      responses:
        "200": {description: ok}
components:
  securitySchemes:
    userAuth: {type: http, scheme: bearer}
    apiAuth: {type: apiKey, in: header, name: X-Api-Key}
`

const publicSpec = `openapi: 3.0.3
info: {title: Orders, version: "1"}
servers:
  - url: http://127.0.0.1:9
paths:
  /health:
    get:
      operationId: orders.health
      security: []
      responses:
        "200": {description: ok}
`

const querySpec = `openapi: 3.0.3
info: {title: Orders, version: "1"}
servers:
  - url: http://127.0.0.1:9
paths:
  /orders/search:
    get:
      operationId: orders.search
      security:
        - queryAuth: []
      responses:
        "200": {description: ok}
components:
  securitySchemes:
    queryAuth: {type: apiKey, in: query, name: api_key}
`

const userSpec = `openapi: 3.0.3
info: {title: Orders, version: "1"}
servers:
  - url: http://127.0.0.1:9
paths:
  /orders/{id}:
    get:
      operationId: orders.get
      security:
        - userAuth: [orders.read]
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      responses:
        "200": {description: ok}
components:
  securitySchemes:
    userAuth:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://idp.example/authorize
          tokenUrl: https://idp.example/oauth/token
          scopes:
            orders.read: read
`

const followAuthSpec = `openapi: 3.0.3
info: {title: Orders, version: "1"}
servers:
  - url: http://127.0.0.1:9
paths:
  /orders/{id}:
    get:
      operationId: orders.get
      security:
        - userAuth: []
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      responses:
        "200":
          description: ok
          links:
            customer:
              operationId: customers.get
              parameters:
                id: $response.body#/customerId
  /customers/{id}:
    get:
      operationId: customers.get
      security:
        - workforce: []
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      responses:
        "200": {description: ok}
components:
  securitySchemes:
    userAuth: {type: http, scheme: bearer}
    workforce: {type: http, scheme: bearer}
`
