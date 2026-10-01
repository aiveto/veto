package execute

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/aiveto/veto/result"
)

type (
	// Projection names the response fields and the page to return.
	// An empty Fields list leaves the body unchanged.
	Projection struct {
		Fields []string
		Offset int
		Limit  int

		explicit bool
	}

	// View is set when a successful body was projected.
	View struct {
		Applied   bool
		Truncated bool
		Page      *result.Page
	}
)

type projectionKey struct{}

// WithProjection carries invoke-request fields. They override the client list when set.
func WithProjection(ctx context.Context, p Projection) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	p.explicit = true
	return context.WithValue(ctx, projectionKey{}, p)
}

// ProjectionFrom reports fields set on the invoke request.
func ProjectionFrom(ctx context.Context) (Projection, bool) {
	if ctx == nil {
		return Projection{}, false
	}
	p, ok := ctx.Value(projectionKey{}).(Projection)
	if !ok || !p.explicit {
		return Projection{}, false
	}
	return p, true
}

func (c Client) projection(ctx context.Context) Projection {
	fields := c.Fields
	limit := c.Limit
	offset := 0
	if p, ok := ProjectionFrom(ctx); ok {
		if len(p.Fields) > 0 {
			fields = p.Fields
		}
		if p.Limit > 0 {
			limit = p.Limit
		}
		offset = p.Offset
	}
	return Projection{Fields: fields, Offset: offset, Limit: limit}
}

// A named field list keeps the capped bytes. The call can return those fields
// and mark truncation. With no fields, the cap is still an error.
func consumeBody(resp *http.Response, cfg Config) ([]byte, bool, error) {
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

func shapeBody(status int, raw []byte, cut bool, cfg Config) ([]byte, View, error) {
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

func projectBody(raw []byte, p Projection, cut bool, max int64) ([]byte, *result.Page, bool, error) {
	val, partial, err := decodeContainer(raw)
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
		encoded, shrank, err := fitEncoded(pageItems, max)
		if err != nil {
			return nil, nil, false, fmt.Errorf("project response: %w", err)
		}
		if shrank {
			truncated = true
			var again []any
			if err := json.Unmarshal(encoded, &again); err != nil {
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
		if int64(len(encoded)) > max {
			return nil, nil, false, fmt.Errorf("response exceeds %d bytes", max)
		}
		return encoded, nil, truncated, nil
	default:
		return nil, nil, false, fmt.Errorf("project response: %w", errNotJSON)
	}
}

func pageWindow(n, offset, limit int) (int, int, bool, error) {
	if offset < 0 || limit < 0 {
		return 0, 0, false, fmt.Errorf("projection page is out of range")
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

func fitEncoded(items []any, max int64) ([]byte, bool, error) {
	encoded, err := encodeJSON(items)
	if err != nil {
		return nil, false, err
	}
	if int64(len(encoded)) <= max {
		return encoded, false, nil
	}
	for len(items) > 0 {
		items = items[:len(items)-1]
		encoded, err = encodeJSON(items)
		if err != nil {
			return nil, false, err
		}
		if int64(len(encoded)) <= max {
			return encoded, true, nil
		}
	}
	encoded, err = encodeJSON([]any{})
	if err != nil {
		return nil, false, err
	}
	return encoded, true, nil
}

func decodeContainer(raw []byte) (any, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, false, errNotJSON
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	switch trimmed[0] {
	case '{':
		obj, partial, err := decodeObject(dec)
		return obj, partial, err
	case '[':
		items, partial, err := decodeArray(dec)
		return items, partial, err
	default:
		return nil, false, errNotJSON
	}
}

func decodeObject(dec *json.Decoder) (map[string]any, bool, error) {
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, false, errNotJSON
	}
	obj := map[string]any{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return obj, true, nil
		}
		key, ok := keyTok.(string)
		if !ok {
			return obj, true, nil
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return obj, true, nil
		}
		val, err := decodeRaw(raw)
		if err != nil {
			return obj, true, nil
		}
		obj[key] = val
	}
	tok, err = dec.Token()
	if err != nil || tok != json.Delim('}') {
		return obj, true, nil
	}
	return obj, false, nil
}

func decodeArray(dec *json.Decoder) ([]any, bool, error) {
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('[') {
		return nil, false, errNotJSON
	}
	var items []any
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return items, true, nil
		}
		val, err := decodeRaw(raw)
		if err != nil {
			return items, true, nil
		}
		items = append(items, val)
	}
	tok, err = dec.Token()
	if err != nil || tok != json.Delim(']') {
		return items, true, nil
	}
	return items, false, nil
}

func decodeRaw(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
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
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
