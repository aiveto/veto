package execute

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/aiveto/veto/result"
	"github.com/aiveto/veto/runtime"
)

// View is set when a successful body was projected.
type View struct {
	Applied   bool
	Truncated bool
	Page      *result.Page
}

func (c Client) projection(ctx context.Context) runtime.Projection {
	if runtime.SkipProjection(ctx) {
		return runtime.Projection{}
	}
	fields := c.Fields
	limit := c.Limit
	offset := 0
	if p, ok := runtime.ProjectionFrom(ctx); ok {
		if len(p.Fields) > 0 {
			fields = p.Fields
		}
		if p.Limit > 0 {
			limit = p.Limit
		}
		offset = p.Offset
	}
	return runtime.Projection{Fields: fields, Offset: offset, Limit: limit}
}

// A named field list keeps the capped bytes. The call can return those fields
// and mark truncation. With no fields, the cap is still an error.
func consumeBody(resp *http.Response, cfg Client) ([]byte, bool, error) {
	if resp == nil || resp.Body == nil {
		return nil, false, nil
	}
	raw, cut, err := readCapped(resp.Body, cfg.MaxBody)
	if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, false, fmt.Errorf("read body: %w", err)
	}
	if cut && len(cfg.Project.Fields) == 0 {
		return nil, true, fmt.Errorf("response exceeds %d bytes", bodyLimit(cfg.MaxBody))
	}
	return raw, cut, nil
}

func shapeBody(status int, raw []byte, cut bool, cfg Client) ([]byte, View, error) {
	if len(cfg.Project.Fields) == 0 || status < 200 || status >= 300 {
		if cut {
			return nil, View{}, fmt.Errorf("response exceeds %d bytes", bodyLimit(cfg.MaxBody))
		}
		return raw, View{}, nil
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		if cut {
			return nil, View{}, fmt.Errorf("response exceeds %d bytes", bodyLimit(cfg.MaxBody))
		}
		return raw, View{Applied: true}, nil
	}
	body, page, truncated, err := projectBody(raw, cfg.Project, cut, bodyLimit(cfg.MaxBody))
	if err != nil {
		return nil, View{}, err
	}
	return body, View{Applied: true, Truncated: truncated, Page: page}, nil
}

var errNotJSON = errors.New("body is not json")

func projectBody(raw []byte, p runtime.Projection, cut bool, limit int64) ([]byte, *result.Page, bool, error) {
	val, partial, err := decodeContainer(raw, cut, p.Fields)
	if err != nil {
		return nil, nil, false, fmt.Errorf("project response: %w", err)
	}
	truncated := cut || partial
	switch typed := val.(type) {
	case []any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, pick(item, p.Fields))
		}
		offset, end, cut, err := pageWindow(len(items), p.Offset, p.Limit)
		if err != nil {
			return nil, nil, false, fmt.Errorf("project response: %w", err)
		}
		if cut {
			truncated = true
		}
		pageItems := items[offset:end]
		encoded, shrank, err := fitEncoded(pageItems, limit)
		if err != nil {
			return nil, nil, false, fmt.Errorf("project response: %w", err)
		}
		if shrank {
			truncated = true
			var again []any
			if err := jsonv2.Unmarshal(encoded, &again); err != nil {
				return nil, nil, false, fmt.Errorf("project response: %w", err)
			}
			pageItems = again
		}
		page := &result.Page{Offset: offset, Limit: p.Limit, Returned: len(pageItems)}
		return encoded, page, truncated, nil
	case map[string]any:
		encoded, err := encodeJSON(pickMap(typed, p.Fields))
		if err != nil {
			return nil, nil, false, fmt.Errorf("project response: %w", err)
		}
		if int64(len(encoded)) > limit {
			return nil, nil, false, fmt.Errorf("response exceeds %d bytes", limit)
		}
		return encoded, nil, truncated, nil
	default:
		return nil, nil, false, fmt.Errorf("project response: %w", errNotJSON)
	}
}

func pageWindow(n, offset, limit int) (int, int, bool, error) {
	if offset < 0 || limit < 0 {
		return 0, 0, false, errors.New("projection page is out of range")
	}
	if offset > n {
		offset = n
	}
	end := n
	if limit > 0 && limit < n-offset {
		end = offset + limit
		return offset, end, true, nil
	}
	return offset, end, false, nil
}

func fitEncoded(items []any, limit int64) ([]byte, bool, error) {
	encoded, err := encodeJSON(items)
	if err != nil {
		return nil, false, err
	}
	if int64(len(encoded)) <= limit {
		return encoded, false, nil
	}
	for len(items) > 0 {
		items = items[:len(items)-1]
		encoded, err = encodeJSON(items)
		if err != nil {
			return nil, false, err
		}
		if int64(len(encoded)) <= limit {
			return encoded, true, nil
		}
	}
	encoded, err = encodeJSON([]any{})
	if err != nil {
		return nil, false, err
	}
	return encoded, true, nil
}

func decodeContainer(raw []byte, cut bool, fields []string) (any, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, false, errNotJSON
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return nil, false, errNotJSON
	}
	dec := jsontext.NewDecoder(bytes.NewReader(trimmed))
	val, partial, err := decodeValue(dec, cut, fields)
	if err != nil {
		return nil, false, err
	}
	if cut {
		return val, partial, nil
	}
	if partial || dec.PeekKind() != 0 {
		return nil, false, errNotJSON
	}
	return val, false, nil
}

func decodeValue(dec *jsontext.Decoder, cut bool, fields []string) (any, bool, error) {
	switch dec.PeekKind() {
	case jsontext.KindBeginObject:
		return decodeObject(dec, cut, fields)
	case jsontext.KindBeginArray:
		return decodeArray(dec, cut, fields)
	case jsontext.KindNull:
		if _, err := dec.ReadValue(); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	case jsontext.KindFalse, jsontext.KindTrue, jsontext.KindString, jsontext.KindNumber:
		raw, err := dec.ReadValue()
		if err != nil {
			return nil, false, err
		}
		val, err := decodeRaw(raw)
		if err != nil {
			return nil, false, err
		}
		return val, false, nil
	case jsontext.KindInvalid, jsontext.KindEndObject, jsontext.KindEndArray:
		return nil, false, errNotJSON
	}
	return nil, false, errNotJSON
}

func decodeObject(dec *jsontext.Decoder, cut bool, fields []string) (map[string]any, bool, error) {
	tok, err := dec.ReadToken()
	if err != nil || tok.Kind() != jsontext.KindBeginObject {
		return nil, false, errNotJSON
	}
	keep := fieldRoots(fields)
	obj := map[string]any{}
	for dec.PeekKind() != jsontext.KindEndObject && dec.PeekKind() != 0 {
		keyTok, err := dec.ReadToken()
		if err != nil || keyTok.Kind() != jsontext.KindString {
			return partialOrError(cut, obj)
		}
		key := keyTok.String()
		skip, child := fieldChild(fields, keep, key)
		if skip {
			if err := dec.SkipValue(); err != nil {
				return partialOrError(cut, obj)
			}
			continue
		}
		val, partial, err := decodeValue(dec, cut, child)
		if err != nil {
			return partialOrError(cut, obj)
		}
		obj[key] = val
		if partial {
			return obj, true, nil
		}
	}
	tok, err = dec.ReadToken()
	if err != nil || tok.Kind() != jsontext.KindEndObject {
		return partialOrError(cut, obj)
	}
	return obj, false, nil
}

func decodeArray(dec *jsontext.Decoder, cut bool, fields []string) ([]any, bool, error) {
	tok, err := dec.ReadToken()
	if err != nil || tok.Kind() != jsontext.KindBeginArray {
		return nil, false, errNotJSON
	}
	items := []any{}
	for dec.PeekKind() != jsontext.KindEndArray && dec.PeekKind() != 0 {
		val, partial, err := decodeValue(dec, cut, fields)
		if err != nil {
			return partialOrError(cut, items)
		}
		items = append(items, val)
		if partial {
			return items, true, nil
		}
	}
	tok, err = dec.ReadToken()
	if err != nil || tok.Kind() != jsontext.KindEndArray {
		return partialOrError(cut, items)
	}
	return items, false, nil
}

func fieldRoots(fields []string) map[string]bool {
	if fields == nil {
		return nil
	}
	out := map[string]bool{}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if i := strings.IndexByte(field, '.'); i >= 0 {
			field = field[:i]
		}
		out[field] = true
	}
	return out
}

func fieldChild(fields []string, keep map[string]bool, key string) (bool, []string) {
	if keep == nil {
		return false, nil
	}
	if !keep[key] {
		return true, nil
	}
	prefix := key + "."
	var child []string
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == key {
			return false, nil
		}
		if strings.HasPrefix(field, prefix) {
			child = append(child, field[len(prefix):])
		}
	}
	return false, child
}

func partialOrError[T any](cut bool, v T) (T, bool, error) {
	if cut {
		return v, true, nil
	}
	var zero T
	return zero, false, errNotJSON
}

func decodeRaw(raw jsontext.Value) (any, error) {
	raw = jsontext.Value(bytes.TrimSpace(raw))
	switch raw.Kind() {
	case jsontext.KindFalse:
		return false, nil
	case jsontext.KindTrue:
		return true, nil
	case jsontext.KindString:
		var s string
		if err := jsonv2.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return s, nil
	case jsontext.KindNumber:
		return json.Number(string(raw)), nil
	case jsontext.KindNull, jsontext.KindInvalid, jsontext.KindBeginObject, jsontext.KindEndObject, jsontext.KindBeginArray, jsontext.KindEndArray:
		return nil, errNotJSON
	default:
		return nil, errNotJSON
	}
}

func pick(v any, fields []string) any {
	obj, ok := v.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return pickMap(obj, fields)
}

func pickMap(obj map[string]any, fields []string) map[string]any {
	out := map[string]any{}
	seen := map[string]bool{}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" || seen[field] {
			continue
		}
		seen[field] = true
		parts := strings.Split(field, ".")
		val, ok := walk(obj, parts)
		if !ok {
			continue
		}
		putPath(out, parts, val)
	}
	return out
}

func walk(v any, parts []string) (any, bool) {
	cur := v
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[part]
			if !ok {
				return nil, false
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			cur = node[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

func putPath(dst map[string]any, parts []string, val any) {
	if len(parts) == 0 {
		return
	}
	if len(parts) == 1 {
		dst[parts[0]] = val
		return
	}
	next, _ := dst[parts[0]].(map[string]any)
	if next == nil {
		next = map[string]any{}
		dst[parts[0]] = next
	}
	putPath(next, parts[1:], val)
}

func encodeJSON(v any) ([]byte, error) {
	return jsonv2.Marshal(v)
}
