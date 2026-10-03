// Package jsonfield reads one field from a JSON object body.
package jsonfield

import (
	"bytes"
	"encoding/json"
)

// String returns the named field. A JSON string is unquoted. Any other value is the raw JSON.
func String(body, field string) (string, bool) {
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(body), &obj) != nil {
		return "", false
	}
	raw, ok := obj[field]
	if !ok {
		return "", false
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return "", false
		}
		return s, true
	}
	return string(raw), true
}
