package auth

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/credentials"
	"github.com/aiveto/veto/result"
)

type (
	Options struct {
		Schemes        []Scheme
		Dir            string
		HTTP           *http.Client
		Now            func() time.Time
		Env            func(string) string
		CommandTimeout time.Duration
	}

	Material struct {
		Headers map[string]string
		Query   map[string]string
		Expires time.Time
		Secrets []string
		Sign    func(*http.Request) error
	}

	Resolver struct {
		schemes        map[string]Scheme
		extra          map[string]credentials.Provider
		mu             sync.Mutex
		dir            string
		http           *http.Client
		now            func() time.Time
		env            func(string) string
		commandTimeout time.Duration
		cache          *tokenCache
		flight         flight
	}
)

func New(opt Options) *Resolver {
	schemes := make(map[string]Scheme, len(opt.Schemes))
	for _, s := range opt.Schemes {
		if s.Source == "" {
			s.Source = "env"
		}
		schemes[s.Name] = s
	}
	dir := opt.Dir
	if dir == "" {
		dir = DefaultTokenDir()
	}
	now := opt.Now
	if now == nil {
		now = time.Now
	}
	env := opt.Env
	if env == nil {
		env = os.Getenv
	}
	client := opt.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Resolver{
		schemes:        schemes,
		extra:          map[string]credentials.Provider{},
		dir:            dir,
		http:           WithEnvProxy(client),
		now:            now,
		env:            env,
		commandTimeout: opt.CommandTimeout,
		cache:          &tokenCache{m: map[string]cacheEntry{}},
	}
}

// SetProvider registers a library Provider for one scheme name.
func (r *Resolver) SetProvider(name string, src credentials.Provider) {
	if r == nil || name == "" || src == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.extra == nil {
		r.extra = map[string]credentials.Provider{}
	}
	r.extra[name] = src
}

func (r *Resolver) lookupExtra(name string) (credentials.Provider, bool) {
	if r == nil || name == "" {
		return nil, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	src, ok := r.extra[name]
	return src, ok
}

// Configured returns the scheme named in config.
func (r *Resolver) Configured(name string) (Scheme, bool) {
	if r == nil || name == "" {
		return Scheme{}, false
	}
	s, ok := r.schemes[name]
	return s, ok
}

func (r *Resolver) Has(name string) bool {
	if r == nil {
		return false
	}
	if _, ok := r.lookupExtra(name); ok {
		return true
	}
	_, ok := r.schemes[name]
	return ok
}

func (r *Resolver) Ready(ctx context.Context, a catalog.Auth) bool {
	if r == nil {
		return false
	}
	if _, ok := r.lookupExtra(a.Name); ok {
		return true
	}
	s, ok := r.schemes[a.Name]
	if !ok {
		return false
	}
	switch s.Source {
	case "env":
		if s.Env != "" && r.env(s.Env) != "" {
			return true
		}
		tok, err := readToken(r.dir, s.Name)
		return err == nil && tok.AccessToken != ""
	case "login":
		tok, err := readToken(r.dir, s.Name)
		if err != nil {
			return false
		}
		if tok.RefreshToken != "" {
			return true
		}
		return tok.AccessToken != "" && usable(tok.ExpiresAt, r.now())
	case "client_credentials":
		return s.TokenURL != "" && s.ClientID != "" && s.ClientSecretEnv != "" && r.env(s.ClientSecretEnv) != ""
	case "invoke":
		return UserToken(ctx) != ""
	case "command":
		return len(s.Command) > 0
	case "token_exchange":
		if s.TokenURL == "" || s.ClientID == "" || s.ClientSecretEnv == "" || r.env(s.ClientSecretEnv) == "" {
			return false
		}
		return r.subjectToken(ctx, s) != ""
	default:
		return false
	}
}

func (r *Resolver) UnsetError(a catalog.Auth) error {
	if r == nil {
		return result.AuthError{Name: a.Name}
	}
	s, ok := r.schemes[a.Name]
	if !ok {
		return result.AuthError{Name: a.Name}
	}
	switch s.Source {
	case "client_credentials":
		return result.AuthError{Name: a.Name, Detail: "client secret is unset"}
	case "login":
		return result.AuthError{Name: a.Name, Detail: "has no stored token"}
	case "token_exchange":
		if s.ClientSecretEnv == "" || r.env(s.ClientSecretEnv) == "" {
			return result.AuthError{Name: a.Name, Detail: "client secret is unset"}
		}
		return result.AuthError{Name: a.Name, Detail: "subject token is unset"}
	default:
		return result.AuthError{Name: a.Name}
	}
}

func (r *Resolver) Refreshable(name string) bool {
	if r == nil {
		return false
	}
	if _, ok := r.lookupExtra(name); ok {
		return true
	}
	s, ok := r.schemes[name]
	if !ok {
		return false
	}
	switch s.Source {
	case "login", "client_credentials", "command", "token_exchange":
		return true
	default:
		return false
	}
}

// Provider returns the provider that obtains material for one scheme.
func (r *Resolver) Provider(a catalog.Auth) (credentials.Provider, bool) {
	if r == nil || a.Name == "" {
		return nil, false
	}
	if src, ok := r.lookupExtra(a.Name); ok {
		return extraProvider{r: r, a: a, src: src}, true
	}
	if _, ok := r.schemes[a.Name]; ok {
		return schemeProvider{r: r, a: a}, true
	}
	return nil, false
}

// Fixed places one already-known secret. It is the provider for a static auth map.
func Fixed(a catalog.Auth, token string) credentials.Provider {
	return fixedProvider{a: a, token: token}
}

// Material returns headers and query values for one scheme.
// force skips a cached token and refreshes once. It does not open a browser.
func (r *Resolver) Material(ctx context.Context, op *catalog.Operation, a catalog.Auth, method, endpoint string, force bool) (Material, error) {
	if r == nil {
		return Material{}, fmt.Errorf("%s is unset", a.Name)
	}
	p, ok := r.Provider(a)
	if !ok {
		return Material{}, fmt.Errorf("%s is unset", a.Name)
	}
	if force {
		ctx = WithForce(ctx)
	}
	id := ""
	if op != nil {
		id = op.ID
	}
	cred, err := p.Resolve(ctx, credentials.Request{
		OperationID: id,
		Method:      method,
		URL:         endpoint,
		Scheme:      a.Name,
		UserToken:   UserToken(ctx),
		Scopes:      a.Scopes,
	})
	if err != nil {
		return Material{}, err
	}
	return materialOf(cred), nil
}

func (r *Resolver) materialScheme(ctx context.Context, a catalog.Auth, in credentials.Request, force bool) (Material, error) {
	if in.UserToken != "" && UserToken(ctx) == "" {
		ctx = WithUserToken(ctx, in.UserToken)
	}
	s, ok := r.schemes[a.Name]
	if !ok {
		return Material{}, fmt.Errorf("%s is unset", a.Name)
	}
	need := in.Scopes
	if len(need) == 0 {
		need = a.Scopes
	}
	if len(need) == 0 {
		need = s.Scopes
	}
	endpoint := in.URL
	method := in.Method
	key := cacheKey(s, need)
	if s.Source == "command" {
		key += "\x00" + method + "\x00" + endpoint + "\x00" + UserToken(ctx)
	}
	if s.Source == "token_exchange" {
		subject := r.subjectToken(ctx, s)
		if subject == "" {
			return Material{}, fmt.Errorf("%s subject token is unset", a.Name)
		}
		key += "\x00" + subject
	}
	key = scopedKey(ctx, key)
	if !force {
		if mat, ok := r.cache.fresh(key, r.now()); ok {
			return mat, nil
		}
	} else {
		r.cache.delete(key)
	}
	flightKey := key
	if force {
		flightKey += "\x00force"
	}
	return r.flight.Do(ctx, flightKey, func() (Material, error) {
		if !force {
			if mat, ok := r.cache.fresh(key, r.now()); ok {
				return mat, nil
			}
		}
		mat, err := r.fetch(ctx, s, a, in.OperationID, method, endpoint, need, force)
		if err != nil {
			if !force {
				if stale, ok := r.cache.usable(key, r.now()); ok {
					return stale, nil
				}
			}
			return Material{}, err
		}
		if !mat.Expires.IsZero() {
			r.cache.put(key, mat)
		}
		return mat, nil
	})
}

func (r *Resolver) fetch(ctx context.Context, s Scheme, a catalog.Auth, operationID, method, endpoint string, need []string, force bool) (Material, error) {
	switch s.Source {
	case "env":
		return r.fetchEnv(s, a)
	case "invoke":
		tok := UserToken(ctx)
		if tok == "" {
			return Material{}, fmt.Errorf("%s is unset", a.Name)
		}
		return placeToken(a, s.Header, tok, time.Time{}), nil
	case "login":
		return r.fetchLogin(ctx, s, a, need, force)
	case "client_credentials":
		if userHeaderName(s, a) != "" {
			return Material{}, fmt.Errorf("%s user token is unset", a.Name)
		}
		return r.fetchClient(ctx, s, a, need)
	case "token_exchange":
		if userHeaderName(s, a) != "" {
			return Material{}, fmt.Errorf("%s user token is unset", a.Name)
		}
		return r.fetchExchange(ctx, s, a, need)
	case "command":
		return r.fetchCommand(ctx, s, operationID, method, endpoint)
	default:
		return Material{}, fmt.Errorf("security scheme %s is not supported", a.Name)
	}
}

func (r *Resolver) fetchEnv(s Scheme, a catalog.Auth) (Material, error) {
	tok := ""
	if s.Env != "" {
		tok = r.env(s.Env)
	}
	if tok == "" {
		stored, err := readToken(r.dir, s.Name)
		if err == nil {
			tok = stored.AccessToken
		}
	}
	if tok == "" {
		return Material{}, fmt.Errorf("%s is unset", a.Name)
	}
	return placeToken(a, s.Header, tok, time.Time{}), nil
}

func userHeaderName(s Scheme, a catalog.Auth) string {
	if s.UserHeader != "" {
		return s.UserHeader
	}
	return a.UserHeader
}

func fieldSecrets(fields map[string]string) []string {
	if len(fields) == 0 {
		return nil
	}
	out := make([]string, 0, len(fields))
	for _, v := range fields {
		if len(v) >= 8 {
			out = append(out, v)
		}
	}
	return out
}

func placeToken(a catalog.Auth, headerOverride, token string, exp time.Time) Material {
	header := a.Header
	if headerOverride != "" {
		header = headerOverride
	}
	switch a.Kind {
	case "apiKey":
		if headerOverride != "" {
			return Material{Headers: map[string]string{headerOverride: token}, Expires: exp, Secrets: []string{token}}
		}
		if a.Query != "" {
			return Material{Query: map[string]string{a.Query: token}, Expires: exp, Secrets: []string{token}}
		}
		if header == "" {
			return Material{}
		}
		return Material{Headers: map[string]string{header: token}, Expires: exp, Secrets: []string{token}}
	default:
		if header == "" {
			header = "Authorization"
		}
		val := token
		if !strings.HasPrefix(strings.ToLower(token), "bearer ") {
			val = "Bearer " + token
		}
		return Material{Headers: map[string]string{header: val}, Expires: exp, Secrets: []string{token, val}}
	}
}

func scopedKey(ctx context.Context, key string) string {
	if id := Caller(ctx); id != "" {
		return key + "\x00" + id
	}
	return key
}

func cacheKey(s Scheme, scopes []string) string {
	cp := append([]string(nil), scopes...)
	sort.Strings(cp)
	id := strings.Join([]string{
		s.Source,
		s.Name,
		s.ClientID,
		s.TokenURL,
		strings.Join(s.Command, " "),
		s.UserHeader,
		s.AuthToken,
		s.UserToken,
		s.Subject,
	}, "\x00")
	return strings.Join([]string{id, s.Audience, strings.Join(cp, " ")}, "\x00")
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	return maps.Clone(in)
}

func mapValues(in map[string]string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
