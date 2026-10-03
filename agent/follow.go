package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/internal/jsonfield"
)

const maxFollowCalls = 8

// Follow runs operationID through Invoke, then each declared relation or link that names a value.
// A link with no parameter mapping is not called. A missing response field is an error.
func (l *Loop) Follow(ctx context.Context, operationID string, params map[string]string, approvalID string) ([]Call, error) {
	first, err := l.Invoke(ctx, operationID, params, approvalID)
	if err != nil || first.Status != "ok" {
		return []Call{first}, err
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
		if len(calls) >= maxFollowCalls {
			return calls, fmt.Errorf("follow stopped after %d calls", maxFollowCalls)
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
	val, ok := jsonfield.String(body, field)
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
	rest, pointer := strings.CutPrefix(field, "$response.body#")
	if !pointer {
		if field == "" || strings.Contains(field, "/") {
			return "", fmt.Errorf("response expression %q is not a field", expr)
		}
		val, ok := jsonfield.String(body, field)
		if !ok {
			return "", fmt.Errorf("response field %s is missing", field)
		}
		return val, nil
	}
	return evalPointer(expr, rest, body)
}

func evalPointer(expr, ptr, body string) (string, error) {
	ptr = strings.TrimPrefix(ptr, "/")
	if ptr == "" {
		return "", fmt.Errorf("response expression %q is not a field", expr)
	}
	cur := bytes.TrimSpace([]byte(body))
	indexes := 0
	for tok := range strings.SplitSeq(ptr, "/") {
		tok = unescapePointer(tok)
		if tok == "" {
			return "", fmt.Errorf("response expression %q is not a field", expr)
		}
		if len(cur) > 0 && cur[0] == '[' && pointerIndex(tok) {
			indexes++
			if indexes > 1 {
				return "", fmt.Errorf("response expression %q has more than one index", expr)
			}
		}
		next, err := pointerStep(cur, tok)
		if err != nil {
			return "", fmt.Errorf("response field %s is missing", ptr)
		}
		cur = next
	}
	val, ok := rawScalar(cur)
	if !ok {
		return "", fmt.Errorf("response field %s is missing", ptr)
	}
	return val, nil
}

func pointerStep(cur []byte, tok string) ([]byte, error) {
	cur = bytes.TrimSpace(cur)
	if len(cur) == 0 {
		return nil, errors.New("missing")
	}
	switch cur[0] {
	case '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(cur, &obj); err != nil {
			return nil, err
		}
		raw, ok := obj[tok]
		if !ok || jsonNull(raw) {
			return nil, errors.New("missing")
		}
		return raw, nil
	case '[':
		if !pointerIndex(tok) {
			return nil, errors.New("missing")
		}
		var arr []json.RawMessage
		if err := json.Unmarshal(cur, &arr); err != nil {
			return nil, err
		}
		i := 0
		for _, c := range tok {
			i = i*10 + int(c-'0')
		}
		if i < 0 || i >= len(arr) || jsonNull(arr[i]) {
			return nil, errors.New("missing")
		}
		return arr[i], nil
	default:
		return nil, errors.New("missing")
	}
}

func pointerIndex(tok string) bool {
	if tok == "" || (len(tok) > 1 && tok[0] == '0') {
		return false
	}
	for _, c := range tok {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func unescapePointer(tok string) string {
	var b strings.Builder
	for i := 0; i < len(tok); i++ {
		if tok[i] == '~' && i+1 < len(tok) {
			switch tok[i+1] {
			case '0':
				b.WriteByte('~')
				i++
				continue
			case '1':
				b.WriteByte('/')
				i++
				continue
			}
		}
		b.WriteByte(tok[i])
	}
	return b.String()
}

func jsonNull(raw []byte) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) == 0 || string(raw) == "null"
}

func rawScalar(raw []byte) (string, bool) {
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
