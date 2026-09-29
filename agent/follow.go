package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aiveto/veto/catalog"
)

// Follow runs operationID through Invoke, then each declared relation or link that names a value.
// A link with no parameter mapping is not called. A missing response field is an error.
func (l *Loop) Follow(ctx context.Context, operationID string, params map[string]string, approvalID string) ([]Call, error) {
	first, err := l.Invoke(ctx, operationID, params, approvalID)
	if err != nil || first.Status != "ok" {
		if err != nil {
			return []Call{first}, err
		}
		return []Call{first}, nil
	}
	calls := []Call{first}
	if l.Catalog == nil {
		return calls, nil
	}
	for _, link := range l.Catalog.Links {
		if link.From != operationID {
			continue
		}
		nextParams, ok, err := paramsFromLink(link, first.Body, l.Catalog.ByID(link.To))
		if err != nil {
			return calls, err
		}
		if !ok {
			continue
		}
		next, err := l.Invoke(ctx, link.To, nextParams, "")
		calls = append(calls, next)
		if err != nil || next.Status != "ok" {
			return calls, err
		}
	}
	return calls, nil
}

func paramsFromLink(link catalog.OpLink, body string, target *catalog.Operation) (map[string]string, bool, error) {
	if len(link.Params) > 0 {
		out := make(map[string]string, len(link.Params))
		for name, expr := range link.Params {
			val, err := evalResponseExpr(expr, body)
			if err != nil {
				return nil, false, fmt.Errorf("link %s to %s: %w", link.From, link.To, err)
			}
			out[name] = val
		}
		return out, true, nil
	}
	if link.Note == "" || target == nil {
		return nil, false, nil
	}
	field := link.Note
	if i := strings.LastIndex(field, "."); i >= 0 {
		field = field[i+1:]
	}
	val, ok := jsonField(body, field)
	if !ok {
		return nil, false, fmt.Errorf("operation %s: response field %s is missing", link.From, field)
	}
	name := targetParam(target, field)
	if name == "" {
		return nil, false, fmt.Errorf("operation %s: no parameter for %s", link.To, field)
	}
	return map[string]string{name: val}, true, nil
}

func evalResponseExpr(expr, body string) (string, error) {
	field := strings.TrimSpace(expr)
	field = strings.TrimPrefix(field, "$response.body#")
	field = strings.TrimPrefix(field, "/")
	if field == "" || strings.Contains(field, "/") {
		return "", fmt.Errorf("response expression %q is not a field", expr)
	}
	val, ok := jsonField(body, field)
	if !ok {
		return "", fmt.Errorf("response field %s is missing", field)
	}
	return val, nil
}

func jsonField(body, field string) (string, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
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
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", false
		}
		return s, true
	}
	return string(raw), true
}

func targetParam(op *catalog.Operation, field string) string {
	if op == nil {
		return ""
	}
	var required []string
	for _, p := range op.Params {
		if p.In != "path" && p.In != "query" {
			continue
		}
		if p.Name == field {
			return p.Name
		}
		if p.Required || p.In == "path" {
			required = append(required, p.Name)
		}
	}
	if len(required) == 1 {
		return required[0]
	}
	return ""
}
