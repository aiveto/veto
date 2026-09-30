package auth

import "net/http"

// WithEnvProxy returns a client that still honors HTTPS_PROXY when Transport is set.
// A nil Transport uses the default transport, which already reads the proxy environment.
func WithEnvProxy(c *http.Client) *http.Client {
	if c == nil {
		return &http.Client{Transport: http.DefaultTransport}
	}
	dup := *c
	if dup.Transport == nil {
		dup.Transport = http.DefaultTransport
		return &dup
	}
	dup.Transport = ensureProxy(dup.Transport)
	return &dup
}

func ensureProxy(rt http.RoundTripper) http.RoundTripper {
	if rt == nil {
		return http.DefaultTransport
	}
	tr, ok := rt.(*http.Transport)
	if !ok {
		return proxyTrip{next: rt}
	}
	if tr.Proxy != nil {
		return tr
	}
	clone := tr.Clone()
	clone.Proxy = http.ProxyFromEnvironment
	return clone
}

type proxyTrip struct {
	next http.RoundTripper
}

func (p proxyTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := http.ProxyFromEnvironment(req)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return p.next.RoundTrip(req)
	}
	return (&http.Transport{Proxy: http.ProxyURL(u)}).RoundTrip(req)
}
