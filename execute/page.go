package execute

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/aiveto/veto/catalog"
)

func followPages(ctx context.Context, cfg Config, op *catalog.Operation, params map[string]string, body string, pageCap int) (string, bool, error) {
	items, ok := pageItems(body)
	if !ok {
		return body, false, nil
	}
	limit := bodyLimit(cfg.MaxBody)
	current := cloneParams(params)
	seen := map[string]bool{}
	truncated := false
	for page := 1; page < pageCap; page++ {
		next, ok := nextPage(op.Page, body)
		if !ok {
			break
		}
		sig := fmt.Sprint(next)
		if seen[sig] {
			break
		}
		seen[sig] = true
		maps.Copy(current, next)
		resp, err := InvokeResponse(ctx, cfg, op, current)
		if err != nil {
			return "", false, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_, _ = readBody(resp, cfg.MaxBody)
			return "", false, fmt.Errorf("follow page: http %d", resp.StatusCode)
		}
		body, err = readBody(resp, cfg.MaxBody)
		if err != nil {
			return "", false, err
		}
		more, ok := pageItems(body)
		if !ok {
			return "", false, errors.New("follow page: response is not a page")
		}
		if pageBytes(items)+pageBytes(more) > int(limit) {
			truncated = true
			break
		}
		items = append(items, more...)
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return "", false, fmt.Errorf("collect pages: %w", err)
	}
	if int64(len(raw)) > limit {
		return "", false, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return string(raw), truncated, nil
}

func pageBytes(items []json.RawMessage) int {
	n := 2
	for _, item := range items {
		n += len(item) + 1
	}
	return n
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
	maps.Copy(out, in)
	return out
}
