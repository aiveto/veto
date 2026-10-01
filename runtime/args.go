package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// FromStrings adapts a string map at a boundary that does not carry JSON types.
func FromStrings(in map[string]string) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// wire is the HTTP serialization of typed arguments.
// Strings stay as given. Objects and arrays become JSON text. Approval binds this same form.
func wire(args map[string]any) (map[string]string, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(args))
	for k, v := range args {
		text, err := wireValue(v)
		if err != nil {
			return nil, fmt.Errorf("param %s: %w", k, err)
		}
		out[k] = text
	}
	return out, nil
}

func wireValue(v any) (string, error) {
	switch val := v.(type) {
	case nil:
		return "", nil
	case string:
		return val, nil
	case json.RawMessage:
		return string(val), nil
	default:
		return jsonText(val)
	}
}

func jsonText(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}
