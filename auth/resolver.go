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
	"github.com/aiveto/veto/jsonopts"
	"github.com/aiveto/veto/result"
)

// cacheSep cannot appear in a field, so "ab"+"c" and "a"+"bc" stay distinct.
const cacheSep = "\x00"

type (
	Options struct {
		Schemes        []Scheme
		Dir            string
		HTTP           *http.Client
		Now            func() time.Time
		Env            func(string) string
		CommandTimeout time.Duration
		JSON           jsonopts.Set
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
		json           jsonopts.Set
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
		json:           opt.JSON,
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
	s, src, ok := r.lookupSource(a.Name)
	if !ok {
		return false
	}
	return src.ready(ctx, r, s)
}

func (r *Resolver) UnsetError(a catalog.Auth) error {
	if r == nil {
		return result.AuthError{Name: a.Name}
	}
	s, src, ok := r.lookupSource(a.Name)
	if !ok {
		return result.AuthError{Name: a.Name}
	}
	return src.unset(r, s)
}

func (r *Resolver) Refreshable(name string) bool {
	if r == nil {
		return false
	}
	if _, ok := r.lookupExtra(name); ok {
		return true
	}
	_, src, ok := r.lookupSource(name)
	if !ok {
		return false
	}
	return src.refreshable()
}

// SuppliesUserHeader reports whether a login scheme can place the user token on header.
func (r *Resolver) SuppliesUserHeader(name, header string) bool {
	s, src, ok := r.lookupSource(name)
	if !ok {
		return false
	}
	pair, ok := src.(userHeaderSource)
	if !ok {
		return false
	}
	return pair.suppliesUserHeader(s, header)
}

// Blockers lists process-level reasons this scheme cannot run.
// An invoke token that arrives on the call is not a blocker.
func (r *Resolver) Blockers(a catalog.Auth) []string {
	if r == nil {
		return []string{fmt.Sprintf("auth scheme %s has no env var", a.Name)}
	}
	if _, ok := r.lookupExtra(a.Name); ok {
		return nil
	}
	s, src, ok := r.lookupSource(a.Name)
	if !ok {
		if _, configured := r.schemes[a.Name]; configured {
			return nil
		}
		return []string{fmt.Sprintf("auth scheme %s has no env var", a.Name)}
	}
	return src.blockers(r, s)
}

func (r *Resolver) lookupSource(name string) (Scheme, source, bool) {
	if r == nil || name == "" {
		return Scheme{}, nil, false
	}
	s, ok := r.schemes[name]
	if !ok {
		return Scheme{}, nil, false
	}
	src := sourceOf(s.Source)
	if src == nil {
		return s, nil, false
	}
	return s, src, true
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
	if extra, ok := sourceOf(s.Source).(cacheKeyed); ok {
		key = strings.Join([]string{key, extra.cacheSuffix(ctx, r, s, method, endpoint)}, cacheSep)
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
		flightKey = strings.Join([]string{key, "force"}, cacheSep)
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
	src := sourceOf(s.Source)
	if src == nil {
		return Material{}, fmt.Errorf("security scheme %s is not supported", a.Name)
	}
	return src.fetch(ctx, r, s, a, fetchIn{
		operationID: operationID,
		method:      method,
		endpoint:    endpoint,
		need:        need,
		force:       force,
	})
}

func (r *Resolver) fetchEnv(s Scheme, a catalog.Auth) (Material, error) {
	tok := ""
	if s.Env != "" {
		tok = r.env(s.Env)
	}
	if tok == "" {
		stored, err := readToken(r.dir, s.Name, r.json)
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
		return strings.Join([]string{key, id}, cacheSep)
	}
	return key
}

func cacheKey(s Scheme, scopes []string) string {
	cp := append([]string(nil), scopes...)
	sort.Strings(cp)
	return strings.Join([]string{
		s.Source,
		s.Name,
		s.ClientID,
		s.TokenURL,
		strings.Join(s.Command, " "),
		s.UserHeader,
		s.AuthToken,
		s.UserToken,
		s.Subject,
		s.Audience,
		strings.Join(cp, " "),
	}, cacheSep)
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
