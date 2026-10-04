package runtime

import (
	"encoding/json"
	"fmt"

	jsonv2 "encoding/json/v2"

	"github.com/aiveto/veto/jsonopts"
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
func wire(args map[string]any, opts jsonopts.Set) (map[string]string, error) {
	if len(args) == 0 {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(args))
	for k, v := range args {
		text, err := wireValue(v, opts)
		if err != nil {
			return nil, fmt.Errorf("param %s: %w", k, err)
		}
		out[k] = text
	}
	return out, nil
}

func wireValue(v any, opts jsonopts.Set) (string, error) {
	switch val := v.(type) {
	case nil:
		return "", nil
	case string:
		return val, nil
	case json.Number:
		return val.String(), nil
	case json.RawMessage:
		return string(val), nil
	default:
		return jsonText(val, opts)
	}
}

func jsonText(v any, _ jsonopts.Set) (string, error) {
	b, err := jsonv2.Marshal(v, jsonv2.Deterministic(true))
	if err != nil {
		return "", err
	}
	return string(b), nil
}
