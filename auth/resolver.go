package auth

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/credentials"
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
	r.extra[name] = src
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
	case "token_exchange":
		if s.ClientSecretEnv == "" || r.env(s.ClientSecretEnv) == "" {
			return fmt.Errorf("%s client secret is unset", a.Name)
		}
		return fmt.Errorf("%s subject token is unset", a.Name)
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
	if src, ok := r.extra[a.Name]; ok {
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

func (r *Resolver) fetchLogin(ctx context.Context, s Scheme, a catalog.Auth, need []string, force bool) (Material, error) {
	stored, err := readToken(r.dir, s.Name)
	if err != nil || (stored.RefreshToken == "" && tokenField(stored, authFieldName(s)) == "") {
		return Material{}, fmt.Errorf("%s has no stored token", a.Name)
	}
	now := r.now()
	if !force && loginSendable(s, a, stored, need, now) {
		mat, perr := r.placeLogin(s, a, stored, stored.ExpiresAt)
		if perr == nil {
			return mat, nil
		}
		if stored.RefreshToken == "" {
			return Material{}, perr
		}
	}
	if stored.RefreshToken == "" {
		if tokenField(stored, authFieldName(s)) == "" || force || !usable(stored.ExpiresAt, now) {
			return Material{}, fmt.Errorf("%s has no stored token", a.Name)
		}
		return r.placeLogin(s, a, stored, stored.ExpiresAt)
	}
	if err := fillEndpoints(ctx, r.http, &s); err != nil {
		if !force && usable(stored.ExpiresAt, now) {
			if mat, perr := r.placeLogin(s, a, stored, stored.ExpiresAt); perr == nil {
				return mat, nil
			}
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
	secrets := append([]string{secret, stored.RefreshToken, stored.AccessToken}, fieldSecrets(stored.Fields)...)
	tok, err := refreshToken(ctx, r.http, clientID, secret, tokenURL, stored.RefreshToken, secrets)
	if err != nil {
		if !force && usable(stored.ExpiresAt, now) {
			if mat, perr := r.placeLogin(s, a, stored, stored.ExpiresAt); perr == nil {
				return mat, nil
			}
		}
		return Material{}, err
	}
	scopes, err := grantedScopes(tok.Scope, need)
	if err != nil {
		return Material{}, err
	}
	exp := expiryFrom(r.now(), tok.ExpiresIn)
	next := absorb(stored, tok, s, scopes, exp, tokenURL, clientID)
	if err := writeToken(r.dir, s.Name, next); err != nil {
		return Material{}, err
	}
	return r.placeLogin(s, a, next, next.ExpiresAt)
}

func loginSendable(s Scheme, a catalog.Auth, stored storedToken, need []string, now time.Time) bool {
	if tokenField(stored, authFieldName(s)) == "" || !fresh(stored.ExpiresAt, now) || !covers(stored.Scopes, need) {
		return false
	}
	if userHeaderName(s, a) != "" && tokenField(stored, userFieldName(s)) == "" {
		return false
	}
	return true
}

func userHeaderName(s Scheme, a catalog.Auth) string {
	if s.UserHeader != "" {
		return s.UserHeader
	}
	return a.UserHeader
}

func (r *Resolver) placeLogin(s Scheme, a catalog.Auth, stored storedToken, exp time.Time) (Material, error) {
	authTok := tokenField(stored, authFieldName(s))
	if authTok == "" {
		return Material{}, fmt.Errorf("%s has no stored token", a.Name)
	}
	mat := placeToken(a, s.Header, authTok, exp)
	header := userHeaderName(s, a)
	if header == "" {
		return mat, nil
	}
	for k := range mat.Headers {
		if strings.EqualFold(k, header) {
			return Material{}, fmt.Errorf("%s user token header matches the auth header", a.Name)
		}
	}
	userTok := tokenField(stored, userFieldName(s))
	if userTok == "" {
		return Material{}, fmt.Errorf("%s user token is unset", a.Name)
	}
	if mat.Headers == nil {
		mat.Headers = map[string]string{}
	}
	mat.Headers[header] = userTok
	mat.Secrets = append(mat.Secrets, userTok)
	return mat, nil
}

func (r *Resolver) fetchExchange(ctx context.Context, s Scheme, a catalog.Auth, need []string) (Material, error) {
	secret := r.env(s.ClientSecretEnv)
	if secret == "" || s.TokenURL == "" || s.ClientID == "" {
		return Material{}, fmt.Errorf("%s client secret is unset", a.Name)
	}
	subject := r.subjectToken(ctx, s)
	if subject == "" {
		return Material{}, fmt.Errorf("%s subject token is unset", a.Name)
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	form.Set("subject_token", subject)
	tokenType := s.SubjectTokenType
	if tokenType == "" {
		tokenType = "urn:ietf:params:oauth:token-type:access_token"
	}
	form.Set("subject_token_type", tokenType)
	form.Set("client_id", s.ClientID)
	form.Set("client_secret", secret)
	if s.Audience != "" {
		form.Set("audience", s.Audience)
	}
	if scopes := scopeParam(need); scopes != "" {
		form.Set("scope", scopes)
	}
	tok, err := postForm(ctx, r.http, s.TokenURL, form, []string{secret, subject})
	if err != nil {
		return Material{}, err
	}
	if _, err := grantedScopes(tok.Scope, need); err != nil {
		return Material{}, err
	}
	return placeToken(a, s.Header, tok.AccessToken, expiryFrom(r.now(), tok.ExpiresIn)), nil
}

func (r *Resolver) subjectToken(ctx context.Context, s Scheme) string {
	if s.Subject == "invoke" {
		return UserToken(ctx)
	}
	if s.Subject == "" {
		return ""
	}
	stored, err := readToken(r.dir, s.Subject)
	if err != nil {
		return ""
	}
	if subjectExpired(stored, r.now()) {
		if stored.RefreshToken == "" {
			return ""
		}
		login := Scheme{Name: s.Subject}
		if other, ok := r.schemes[s.Subject]; ok {
			login = other
		}
		login.Name = s.Subject
		if _, err := r.fetchLogin(ctx, login, catalog.Auth{Name: s.Subject}, nil, true); err != nil {
			return ""
		}
		stored, err = readToken(r.dir, s.Subject)
		if err != nil {
			return ""
		}
	}
	if other, ok := r.schemes[s.Subject]; ok {
		if v := tokenField(stored, authFieldName(other)); v != "" {
			return v
		}
	}
	return tokenField(stored, "access_token")
}

func subjectExpired(stored storedToken, now time.Time) bool {
	return !stored.ExpiresAt.IsZero() && !now.Before(stored.ExpiresAt)
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

func (r *Resolver) fetchClient(ctx context.Context, s Scheme, a catalog.Auth, need []string) (Material, error) {
	secret := r.env(s.ClientSecretEnv)
	if secret == "" || s.TokenURL == "" || s.ClientID == "" {
		return Material{}, fmt.Errorf("%s client secret is unset", a.Name)
	}
	tok, err := clientCredentialsToken(ctx, r.http, s.ClientID, secret, s.TokenURL, s.Audience, need)
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

func (r *Resolver) fetchCommand(ctx context.Context, s Scheme, operationID, method, endpoint string) (Material, error) {
	in := commandIn{
		OperationID: operationID,
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

type (
	schemeProvider struct {
		r *Resolver
		a catalog.Auth
	}
	extraProvider struct {
		r   *Resolver
		a   catalog.Auth
		src credentials.Provider
	}
	fixedProvider struct {
		a     catalog.Auth
		token string
	}
)

func (p schemeProvider) Resolve(ctx context.Context, in credentials.Request) (credentials.Credential, error) {
	mat, err := p.r.materialScheme(ctx, p.a, in, forced(ctx))
	if err != nil {
		return credentials.Credential{}, err
	}
	return credentialOf(mat), nil
}

func (p extraProvider) Resolve(ctx context.Context, in credentials.Request) (credentials.Credential, error) {
	if in.UserToken != "" && UserToken(ctx) == "" {
		ctx = WithUserToken(ctx, in.UserToken)
	}
	if in.Scheme == "" {
		in.Scheme = p.a.Name
	}
	if in.UserToken == "" {
		in.UserToken = UserToken(ctx)
	}
	if len(in.Scopes) == 0 {
		in.Scopes = p.a.Scopes
		if len(in.Scopes) == 0 {
			if cfg, ok := p.r.schemes[p.a.Name]; ok {
				in.Scopes = cfg.Scopes
			}
		}
	}
	if in.Audience == "" {
		if cfg, ok := p.r.schemes[p.a.Name]; ok {
			in.Audience = cfg.Audience
		}
	}
	return p.src.Resolve(ctx, in)
}

func (p fixedProvider) Resolve(context.Context, credentials.Request) (credentials.Credential, error) {
	if p.token == "" {
		name := p.a.Name
		if name == "" {
			name = "credential"
		}
		return credentials.Credential{}, fmt.Errorf("%s is unset", name)
	}
	return credentialOf(placeToken(p.a, "", p.token, time.Time{})), nil
}

func credentialOf(mat Material) credentials.Credential {
	return credentials.Credential{
		Headers:   cloneMap(mat.Headers),
		Query:     cloneMap(mat.Query),
		ExpiresAt: mat.Expires,
		Sign:      mat.Sign,
	}
}

func materialOf(c credentials.Credential) Material {
	return Material{
		Headers: cloneMap(c.Headers),
		Query:   cloneMap(c.Query),
		Expires: c.ExpiresAt,
		Secrets: CredentialSecrets(c),
		Sign:    c.Sign,
	}
}

// CredentialSecrets lists header values, query values, and a bearer token without its prefix.
func CredentialSecrets(c credentials.Credential) []string {
	out := make([]string, 0, len(c.Headers)+len(c.Query))
	for _, v := range c.Headers {
		out = append(out, secretParts(v)...)
	}
	for _, v := range c.Query {
		out = append(out, secretParts(v)...)
	}
	return out
}

func secretParts(v string) []string {
	if v == "" {
		return nil
	}
	out := []string{v}
	const prefix = "bearer "
	if len(v) > len(prefix) && strings.EqualFold(v[:len(prefix)], prefix) {
		raw := strings.TrimSpace(v[len(prefix):])
		if raw != "" && raw != v {
			out = append(out, raw)
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
		Sign:    m.Sign,
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

func (f *flight) Do(ctx context.Context, key string, fn func() (Material, error)) (Material, error) {
	f.mu.Lock()
	if f.m == nil {
		f.m = map[string]*flightCall{}
	}
	if c, ok := f.m[key]; ok {
		f.mu.Unlock()
		select {
		case <-c.done:
			return cloneMaterial(c.mat), c.err
		case <-ctx.Done():
			return Material{}, ctx.Err()
		}
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
