package capability

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	authHeader   = regexp.MustCompile(`(?i)\b(authorization|veto-caller)\s*:\s*(?:bearer|basic)?\s*\S+`)
	authScheme   = regexp.MustCompile(`(?i)\b(bearer|basic)\s+\S+`)
	httpURL      = regexp.MustCompile(`https?://[^\s"'<>]+`)
	userinfo     = regexp.MustCompile(`(?i)(https?://)[^/\s@"']+@`)
	secretAssign = regexp.MustCompile(`(?i)\b(access_token|refresh_token|client_secret|api[_-]?key|password|authorization|secret|token)=([^\s&"',;]+)`)
)

// Sanitize removes credentials from an invoke error shown on the wire.
func Sanitize(msg string) string {
	if msg == "" {
		return ""
	}
	msg = authHeader.ReplaceAllString(msg, "$1: REDACTED")
	msg = authScheme.ReplaceAllString(msg, "$1 REDACTED")
	msg = httpURL.ReplaceAllStringFunc(msg, redactURL)
	msg = userinfo.ReplaceAllString(msg, "${1}REDACTED@")
	return secretAssign.ReplaceAllString(msg, "$1=REDACTED")
}

func redactURL(raw string) string {
	end := len(raw)
	for end > 0 && strings.ContainsRune(".,);", rune(raw[end-1])) {
		end--
	}
	core, tail := raw[:end], raw[end:]
	u, err := url.Parse(core)
	if err != nil || u.Host == "" {
		return raw
	}
	changed := false
	if u.User != nil {
		u.User = url.User("REDACTED")
		changed = true
	}
	q := u.Query()
	for k := range q {
		if sensitiveQuery(k) {
			q.Set(k, "REDACTED")
			changed = true
		}
	}
	if !changed {
		return raw
	}
	u.RawQuery = q.Encode()
	return u.String() + tail
}

func sensitiveQuery(name string) bool {
	switch strings.ToLower(strings.ReplaceAll(name, "-", "_")) {
	case "access_token", "api_key", "apikey", "authorization", "client_secret", "password", "refresh_token", "secret", "token":
		return true
	default:
		return false
	}
}
