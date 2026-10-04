package execute

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func sanitize(req *http.Request) (method, rawURL string, headers map[string]string, body string) {
	if req == nil {
		return "", "", nil, ""
	}
	method = req.Method
	rawURL = redactedURL(req.URL)
	body = redactBody(readRequestBody(req))
	if len(req.Header) == 0 {
		return method, rawURL, nil, body
	}
	headers = make(map[string]string, len(req.Header))
	for name, values := range req.Header {
		joined := strings.Join(values, ", ")
		if sensitiveName(name) {
			joined = "REDACTED"
		}
		headers[name] = joined
	}
	return method, rawURL, headers, body
}

func readRequestBody(req *http.Request) string {
	if req == nil || req.Body == nil {
		return ""
	}
	b, err := io.ReadAll(req.Body)
	if err != nil {
		return ""
	}
	req.Body = io.NopCloser(bytes.NewReader(b))
	return string(b)
}

func redactedURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	cloned := *u
	q := cloned.Query()
	for key := range q {
		if sensitiveName(key) {
			q.Set(key, "REDACTED")
		}
	}
	cloned.RawQuery = q.Encode()
	return cloned.String()
}

func redactBody(body string) string {
	if body == "" {
		return ""
	}
	var value any
	if err := jsonv2.Unmarshal([]byte(body), &value); err != nil {
		return body
	}
	redactJSON(&value)
	raw, err := jsonv2.Marshal(value)
	if err != nil {
		return body
	}
	return string(raw)
}

func redactJSON(value *any) {
	switch t := (*value).(type) {
	case map[string]any:
		for key, item := range t {
			if sensitiveName(key) {
				t[key] = "REDACTED"
				continue
			}
			redactJSON(&item)
			t[key] = item
		}
	case []any:
		for i := range t {
			redactJSON(&t[i])
		}
	}
}

func sensitiveName(name string) bool {
	switch strings.ToLower(strings.ReplaceAll(name, "-", "_")) {
	case "access_token", "api_key", "apikey", "api_token", "authorization", "client_secret", "cookie", "id_token", "password", "refresh_token", "secret", "token", "veto_caller", "x_api_key", "x_api_token", "x_token":
		return true
	default:
		return false
	}
}
