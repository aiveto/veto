// Package jsonfield reads one field from a JSON object body.
package jsonfield

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
)

// String returns the named field. A JSON string is unquoted. Any other value is the raw JSON. An array uses the first element that has the field.
func String(body, field string) (string, bool) {
	trimmed := bytes.TrimSpace([]byte(body))
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var items []json.RawMessage
		if jsonv2.Unmarshal(trimmed, &items) != nil {
			return "", false
		}
		for _, item := range items {
			if value, ok := valueOf(item, field); ok {
				return value, true
			}
		}
		return "", false
	}
	var obj map[string]json.RawMessage
	if jsonv2.Unmarshal([]byte(body), &obj) != nil {
		return "", false
	}
	raw, ok := obj[field]
	if !ok {
		return "", false
	}
	return decodeValue(raw)
}

func valueOf(item json.RawMessage, field string) (string, bool) {
	var obj map[string]json.RawMessage
	if jsonv2.Unmarshal(item, &obj) != nil {
		return "", false
	}
	raw, ok := obj[field]
	if !ok {
		return "", false
	}
	return decodeValue(raw)
}

func decodeValue(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	if raw[0] == '"' {
		var s string
		if jsonv2.Unmarshal(raw, &s) != nil {
			return "", false
		}
		return s, true
	}
	return string(raw), true
}
