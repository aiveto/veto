package execute

import (
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/aiveto/veto/catalog"
)

func writeQuery(q url.Values, p catalog.Param, raw string) {
	if raw == "" {
		return
	}
	if p.Style == "" || !structured(p.Schema) {
		q.Set(p.Name, raw)
		return
	}
	value, ok := decodeStructured(raw)
	if !ok {
		q.Set(p.Name, raw)
		return
	}
	switch p.Style {
	case "deepObject":
		obj, ok := value.(map[string]any)
		if !ok {
			q.Set(p.Name, raw)
			return
		}
		for _, key := range sortedKeys(obj) {
			q.Add(p.Name+"["+key+"]", scalar(obj[key]))
		}
	case "spaceDelimited":
		writeDelimited(q, p, value, " ", raw)
	case "pipeDelimited":
		writeDelimited(q, p, value, "|", raw)
	default:
		writeDelimited(q, p, value, ",", raw)
	}
}

func writeDelimited(q url.Values, p catalog.Param, value any, sep, raw string) {
	switch v := value.(type) {
	case []any:
		if p.Explode {
			for _, item := range v {
				q.Add(p.Name, scalar(item))
			}
			return
		}
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = scalar(item)
		}
		q.Set(p.Name, strings.Join(parts, sep))
	case map[string]any:
		keys := sortedKeys(v)
		if p.Explode {
			for _, key := range keys {
				q.Add(key, scalar(v[key]))
			}
			return
		}
		parts := make([]string, 0, len(keys)*2)
		for _, key := range keys {
			parts = append(parts, key, scalar(v[key]))
		}
		q.Set(p.Name, strings.Join(parts, sep))
	default:
		q.Set(p.Name, raw)
	}
}

func structured(schema string) bool {
	switch schemaType(schema) {
	case "array", "object":
		return true
	default:
		return false
	}
}

func schemaType(schema string) string {
	if schema == "" {
		return ""
	}
	var doc struct {
		Type any `json:"type"`
	}
	if err := json.Unmarshal([]byte(schema), &doc); err != nil {
		return ""
	}
	switch t := doc.Type.(type) {
	case string:
		return t
	case []any:
		if len(t) == 1 {
			s, _ := t[0].(string)
			return s
		}
	}
	return ""
}

func decodeStructured(raw string) (any, bool) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, false
	}
	switch value.(type) {
	case []any, map[string]any:
		return value, true
	default:
		return nil, false
	}
}

func sortedKeys(obj map[string]any) []string {
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func scalar(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return ""
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(b)
	}
}
