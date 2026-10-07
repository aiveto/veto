package runtime

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/internal/jsonfield"
)

const maxNextCalls = 8

// NextCall is one later operation named by a relation and a value in this response.
type NextCall struct {
	OperationID string            `json:"OperationID"`
	Params      map[string]string `json:"Params,omitempty"`
}

// LinkParams reads the parameter values a link names in body.
// A link with no parameter mapping is not a call. A missing response field is an error.
func LinkParams(link catalog.OpLink, body string, target *catalog.Operation) (map[string]string, bool, error) {
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

// nextCalls lists later operations this response can fill.
// A missing field, a projected field, or a header is omitted. Follow still reports that miss.
func (rt *Runtime) nextCalls(operationID, body string) []NextCall {
	if rt == nil || rt.Catalog == nil || operationID == "" || strings.TrimSpace(body) == "" {
		return nil
	}
	var out []NextCall
	for _, link := range rt.Catalog.Links {
		if link.From != operationID || len(out) == maxNextCalls {
			continue
		}
		target := rt.Catalog.ByID(link.To)
		params, ok, err := LinkParams(link, body, target)
		if err != nil || !ok || secretParam(target, params) {
			continue
		}
		out = append(out, NextCall{OperationID: link.To, Params: params})
	}
	return out
}

func secretParam(op *catalog.Operation, params map[string]string) bool {
	if len(params) == 0 {
		return true
	}
	for name, val := range params {
		if strings.TrimSpace(val) == "" {
			return true
		}
		if op == nil {
			continue
		}
		for _, p := range op.Params {
			if p.Name == name && p.In == "header" {
				return true
			}
		}
	}
	return false
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
		if err := jsonv2.Unmarshal(cur, &obj); err != nil {
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
		if err := jsonv2.Unmarshal(cur, &arr); err != nil {
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
		if err := jsonv2.Unmarshal(raw, &s); err != nil {
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
