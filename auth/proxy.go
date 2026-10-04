package auth

import "net/http"

// WithEnvProxy returns a client that still honors HTTPS_PROXY when Transport is *http.Transport.
// A nil Transport uses the default transport, which already reads the proxy environment.
// Any other RoundTripper is left alone. The caller owns its proxy.
func WithEnvProxy(c *http.Client) *http.Client {
	if c == nil {
		return &http.Client{Transport: http.DefaultTransport}
	}
	if c.Transport == nil {
		return c
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.Proxy != nil {
		return c
	}
	dup := *c
	clone := tr.Clone()
	clone.Proxy = http.ProxyFromEnvironment
	dup.Transport = clone
	return &dup
}
