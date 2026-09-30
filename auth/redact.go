package auth

import (
	"net/url"
	"strings"
)

// Redact removes secret values and named query credentials from s.
func Redact(s string, secrets, queryKeys []string) string {
	if s == "" {
		return s
	}
	for _, key := range queryKeys {
		s = redactQuery(s, key)
	}
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		s = strings.ReplaceAll(s, secret, "REDACTED")
	}
	return s
}

func redactQuery(s, key string) string {
	if key == "" || !strings.Contains(s, key+"=") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	rest := s
	for {
		i := strings.Index(rest, key+"=")
		if i < 0 {
			b.WriteString(rest)
			return b.String()
		}
		b.WriteString(rest[:i])
		b.WriteString(key)
		b.WriteString("=REDACTED")
		rest = rest[i+len(key)+1:]
		end := strings.IndexAny(rest, "&#\" ")
		if end < 0 {
			return b.String()
		}
		rest = rest[end:]
	}
}

// RedactURL clears query parameters named in keys.
func RedactURL(raw string, keys []string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return Redact(raw, nil, keys)
	}
	q := u.Query()
	for _, key := range keys {
		if q.Has(key) {
			q.Set(key, "REDACTED")
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
