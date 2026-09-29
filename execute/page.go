package execute

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aiveto/veto/catalog"
)

func followPages(ctx context.Context, cfg Config, op *catalog.Operation, params map[string]string, body string, cap int) (string, error) {
	items, ok := pageItems(body)
	if !ok {
		return body, nil
	}
	current := cloneParams(params)
	seen := map[string]bool{}
	for page := 1; page < cap; page++ {
		next, ok := nextPage(op.Page, body)
		if !ok {
			break
		}
		sig := fmt.Sprint(next)
		if seen[sig] {
			break
		}
		seen[sig] = true
		for k, v := range next {
			current[k] = v
		}
		resp, err := Invoke(ctx, cfg, op, current)
		if err != nil {
			return "", err
		}
		body, err = ReadBody(resp)
		if err != nil {
			return "", err
		}
		more, ok := pageItems(body)
		if !ok {
			break
		}
		items = append(items, more...)
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return "", fmt.Errorf("collect pages: %w", err)
	}
	return string(raw), nil
}

func nextPage(mapping map[string]string, body string) (map[string]string, bool) {
	if len(mapping) == 0 {
		return nil, false
	}
	out := make(map[string]string, len(mapping))
	for name, expr := range mapping {
		val, ok := jsonField(body, responseField(expr))
		if !ok || val == "" {
			return nil, false
		}
		out[name] = val
	}
	return out, true
}

func responseField(expr string) string {
	field := strings.TrimSpace(expr)
	field = strings.TrimPrefix(field, "$response.body#")
	return strings.TrimPrefix(field, "/")
}

func pageItems(body string) ([]json.RawMessage, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, false
	}
	if body[0] == '[' {
		var items []json.RawMessage
		if json.Unmarshal([]byte(body), &items) != nil {
			return nil, false
		}
		return items, true
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(body), &obj) != nil {
		return nil, false
	}
	raw, ok := obj["items"]
	if !ok {
		return nil, false
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil, false
	}
	return items, true
}

func jsonField(body, field string) (string, bool) {
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

func cloneParams(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
