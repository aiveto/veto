package auth

import "net/http"

// WithEnvProxy returns a client that still honors HTTPS_PROXY when Transport is *http.Transport.
// A nil Transport uses the default transport, which already reads the proxy environment.
// Any other RoundTripper is left alone. The caller owns its proxy.
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
		return rt
	}
	if tr.Proxy != nil {
		return tr
	}
	clone := tr.Clone()
	clone.Proxy = http.ProxyFromEnvironment
	return clone
}
