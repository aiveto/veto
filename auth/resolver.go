package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aiveto/veto/catalog"
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
	}

	Resolver struct {
		schemes        map[string]Scheme
		extra          map[string]Source
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
		extra:          map[string]Source{},
		dir:            dir,
		http:           WithEnvProxy(client),
		now:            now,
		env:            env,
		commandTimeout: opt.CommandTimeout,
		cache:          &tokenCache{m: map[string]cacheEntry{}},
	}
}

// SetSource registers a library Source for one scheme name.
func (r *Resolver) SetSource(name string, src Source) {
	if r == nil || name == "" || src == nil {
		return
	}
	r.extra[name] = src
}

func (r *Resolver) Has(name string) bool {
	if r == nil {
		return false
	}
	if _, ok := r.extra[name]; ok {
		return true
	}
	_, ok := r.schemes[name]
	return ok
}

func (r *Resolver) Ready(ctx context.Context, a catalog.Auth) bool {
	if r == nil {
		return false
	}
	if _, ok := r.extra[a.Name]; ok {
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
	default:
		return false
	}
}

func (r *Resolver) UnsetError(a catalog.Auth) error {
	if r == nil {
		return fmt.Errorf("%s is unset", a.Name)
	}
	s, ok := r.schemes[a.Name]
	if !ok {
		return fmt.Errorf("%s is unset", a.Name)
	}
	switch s.Source {
	case "client_credentials":
		return fmt.Errorf("%s client secret is unset", a.Name)
	case "login":
		return fmt.Errorf("%s has no stored token", a.Name)
	default:
		return fmt.Errorf("%s is unset", a.Name)
	}
}

func (r *Resolver) Refreshable(name string) bool {
	if r == nil {
		return false
	}
	if _, ok := r.extra[name]; ok {
		return true
	}
	s, ok := r.schemes[name]
	if !ok {
		return false
	}
	switch s.Source {
	case "login", "client_credentials", "command":
		return true
	default:
		return false
	}
}

// Material returns headers and query values for one scheme.
// force skips a cached token and refreshes once. It does not open a browser.
func (r *Resolver) Material(ctx context.Context, op *catalog.Operation, a catalog.Auth, method, endpoint string, force bool) (Material, error) {
	if r == nil {
		return Material{}, fmt.Errorf("%s is unset", a.Name)
	}
	if src, ok := r.extra[a.Name]; ok {
		return r.fromSource(ctx, src, op, a, method, endpoint, force)
	}
	s, ok := r.schemes[a.Name]
	if !ok {
		return Material{}, fmt.Errorf("%s is unset", a.Name)
	}
	need := a.Scopes
	if len(need) == 0 {
		need = s.Scopes
	}
	key := cacheKey(s, need)
	if !force {
		if mat, ok := r.cache.fresh(key, r.now()); ok {
			return mat, nil
		}
	} else {
		r.cache.delete(key)
	}
	flightKey := key
	if s.Source == "command" {
		flightKey += "\x00" + method + "\x00" + endpoint
	}
	if force {
		flightKey += "\x00force"
	}
	return r.flight.Do(flightKey, func() (Material, error) {
		if !force {
			if mat, ok := r.cache.fresh(key, r.now()); ok {
				return mat, nil
			}
		}
		mat, err := r.fetch(ctx, s, a, op, method, endpoint, need, force)
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

func (r *Resolver) fromSource(ctx context.Context, src Source, op *catalog.Operation, a catalog.Auth, method, endpoint string, force bool) (Material, error) {
	s := Scheme{Name: a.Name, Source: "source", Audience: "", Scopes: a.Scopes}
	if cfg, ok := r.schemes[a.Name]; ok {
		s.Audience = cfg.Audience
		if len(s.Scopes) == 0 {
			s.Scopes = cfg.Scopes
		}
	}
	need := a.Scopes
	if len(need) == 0 {
		need = s.Scopes
	}
	key := cacheKey(s, need)
	if !force {
		if mat, ok := r.cache.fresh(key, r.now()); ok {
			return mat, nil
		}
	}
	user := UserToken(ctx)
	out, err := src.Token(ctx, Input{
		OperationID: opID(op),
		Method:      method,
		URL:         endpoint,
		Scheme:      a.Name,
		UserToken:   user,
		Scopes:      need,
		Audience:    s.Audience,
	})
	if err != nil {
		return Material{}, err
	}
	mat := Material{Headers: cloneMap(out.Headers), Expires: out.ExpiresAt, Secrets: mapValues(out.Headers)}
	if !mat.Expires.IsZero() {
		r.cache.put(key, mat)
	}
	return mat, nil
}

func (r *Resolver) fetch(ctx context.Context, s Scheme, a catalog.Auth, op *catalog.Operation, method, endpoint string, need []string, force bool) (Material, error) {
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
		return r.fetchClient(ctx, s, a, need)
	case "command":
		return r.fetchCommand(ctx, s, op, method, endpoint)
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

func (r *Resolver) fetchLogin(ctx context.Context, s Scheme, a catalog.Auth, need []string, force bool) (Material, error) {
	stored, err := readToken(r.dir, s.Name)
	if err != nil || (stored.RefreshToken == "" && stored.AccessToken == "") {
		return Material{}, fmt.Errorf("%s has no stored token", a.Name)
	}
	now := r.now()
	if !force && stored.AccessToken != "" && fresh(stored.ExpiresAt, now) && covers(stored.Scopes, need) {
		return placeToken(a, s.Header, stored.AccessToken, stored.ExpiresAt), nil
	}
	if stored.RefreshToken == "" {
		if !force && stored.AccessToken != "" && usable(stored.ExpiresAt, now) {
			return placeToken(a, s.Header, stored.AccessToken, stored.ExpiresAt), nil
		}
		return Material{}, fmt.Errorf("%s has no stored token", a.Name)
	}
	if err := fillEndpoints(ctx, r.http, &s); err != nil {
		if stored.AccessToken != "" && usable(stored.ExpiresAt, now) && !force {
			return placeToken(a, s.Header, stored.AccessToken, stored.ExpiresAt), nil
		}
		return Material{}, err
	}
	tokenURL := s.TokenURL
	if tokenURL == "" {
		tokenURL = stored.TokenURL
	}
	clientID := s.ClientID
	if clientID == "" {
		clientID = stored.ClientID
	}
	secret := r.env(s.ClientSecretEnv)
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", stored.RefreshToken)
	form.Set("client_id", clientID)
	if secret != "" {
		form.Set("client_secret", secret)
	}
	if scopes := scopeParam(need); scopes != "" {
		form.Set("scope", scopes)
	}
	tok, err := postForm(ctx, r.http, tokenURL, form, []string{secret, stored.RefreshToken, stored.AccessToken})
	if err != nil {
		if !force && stored.AccessToken != "" && usable(stored.ExpiresAt, now) {
			return placeToken(a, s.Header, stored.AccessToken, stored.ExpiresAt), nil
		}
		return Material{}, err
	}
	scopes, err := grantedScopes(tok.Scope, need)
	if err != nil {
		return Material{}, err
	}
	refresh := tok.RefreshToken
	if refresh == "" {
		refresh = stored.RefreshToken
	}
	exp := expiryFrom(r.now(), tok.ExpiresIn)
	next := storedToken{
		RefreshToken: refresh,
		AccessToken:  tok.AccessToken,
		ExpiresAt:    exp,
		Scopes:       scopes,
		Audience:     s.Audience,
		TokenURL:     tokenURL,
		ClientID:     clientID,
	}
	if err := writeToken(r.dir, s.Name, next); err != nil {
		return Material{}, err
	}
	return placeToken(a, s.Header, tok.AccessToken, exp), nil
}

func (r *Resolver) fetchClient(ctx context.Context, s Scheme, a catalog.Auth, need []string) (Material, error) {
	secret := r.env(s.ClientSecretEnv)
	if secret == "" || s.TokenURL == "" || s.ClientID == "" {
		return Material{}, fmt.Errorf("%s client secret is unset", a.Name)
	}
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", s.ClientID)
	form.Set("client_secret", secret)
	if scopes := scopeParam(need); scopes != "" {
		form.Set("scope", scopes)
	}
	if s.Audience != "" {
		form.Set("audience", s.Audience)
	}
	tok, err := postForm(ctx, r.http, s.TokenURL, form, []string{secret})
	if err != nil {
		return Material{}, err
	}
	scopes, err := grantedScopes(tok.Scope, need)
	if err != nil {
		return Material{}, err
	}
	_ = scopes
	exp := expiryFrom(r.now(), tok.ExpiresIn)
	return placeToken(a, s.Header, tok.AccessToken, exp), nil
}

func (r *Resolver) fetchCommand(ctx context.Context, s Scheme, op *catalog.Operation, method, endpoint string) (Material, error) {
	in := commandIn{
		OperationID: opID(op),
		Method:      method,
		URL:         endpoint,
		Scheme:      s.Name,
	}
	if user := UserToken(ctx); user != "" {
		in.UserToken = user
	}
	out, err := runCommand(ctx, s, in, r.commandTimeout)
	if err != nil {
		return Material{}, err
	}
	return Material{Headers: cloneMap(out.Headers), Expires: out.ExpiresAt, Secrets: mapValues(out.Headers)}, nil
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

func cacheKey(s Scheme, scopes []string) string {
	cp := append([]string(nil), scopes...)
	sort.Strings(cp)
	id := strings.Join([]string{
		s.Source,
		s.Name,
		s.ClientID,
		s.TokenURL,
		strings.Join(s.Command, " "),
	}, "\x00")
	return strings.Join([]string{id, s.Audience, strings.Join(cp, " ")}, "\x00")
}

func opID(op *catalog.Operation) string {
	if op == nil {
		return ""
	}
	return op.ID
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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

type (
	cacheEntry struct {
		mat Material
	}

	tokenCache struct {
		mu sync.Mutex
		m  map[string]cacheEntry
	}
)

func (c *tokenCache) fresh(key string, now time.Time) (Material, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || !fresh(e.mat.Expires, now) {
		return Material{}, false
	}
	return cloneMaterial(e.mat), true
}

func (c *tokenCache) usable(key string, now time.Time) (Material, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || !usable(e.mat.Expires, now) {
		return Material{}, false
	}
	return cloneMaterial(e.mat), true
}

func (c *tokenCache) put(key string, mat Material) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = cacheEntry{mat: cloneMaterial(mat)}
}

func (c *tokenCache) delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key)
}

func cloneMaterial(m Material) Material {
	return Material{
		Headers: cloneMap(m.Headers),
		Query:   cloneMap(m.Query),
		Expires: m.Expires,
		Secrets: append([]string(nil), m.Secrets...),
	}
}

type (
	flight struct {
		mu sync.Mutex
		m  map[string]*flightCall
	}

	flightCall struct {
		done chan struct{}
		mat  Material
		err  error
	}
)

func (f *flight) Do(key string, fn func() (Material, error)) (Material, error) {
	f.mu.Lock()
	if f.m == nil {
		f.m = map[string]*flightCall{}
	}
	if c, ok := f.m[key]; ok {
		f.mu.Unlock()
		<-c.done
		return cloneMaterial(c.mat), c.err
	}
	c := &flightCall{done: make(chan struct{})}
	f.m[key] = c
	f.mu.Unlock()
	c.mat, c.err = fn()
	close(c.done)
	f.mu.Lock()
	delete(f.m, key)
	f.mu.Unlock()
	return cloneMaterial(c.mat), c.err
}
