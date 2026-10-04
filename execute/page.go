package execute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/internal/jsonfield"
)

func followPages(ctx context.Context, cfg Client, op *catalog.Operation, params map[string]string, body string, pageCap int) (string, bool, error) {
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
		resp, _, raw, err := invokeResponse(ctx, cfg, op, current)
		if err != nil {
			return "", false, err
		}
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", false, fmt.Errorf("follow page: http %d", resp.StatusCode)
		}
		body = string(raw)
		more, ok := pageItems(body)
		if !ok {
			return "", false, errors.New("follow page: response is not a page")
		}
		if pageBytes(items)+pageBytes(more) > int(limit) {
			truncated = true
			break
		}
		items = append(items, more...)
		// pageCap counts the first page too. A later cursor means this result is partial.
		if page+1 == pageCap {
			later, found := nextPage(op.Page, body)
			if found && !seen[fmt.Sprint(later)] {
				truncated = true
			}
		}
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
		val, ok := jsonfield.String(body, responseField(expr))
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

func cloneParams(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}
