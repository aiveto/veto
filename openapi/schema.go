package openapi

import (
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/aiveto/veto/catalog"
	"github.com/getkin/kin-openapi/openapi3"
)

const jsonMedia = "application/json"

func bodyParam(op *openapi3.Operation) (catalog.Param, bool) {
	if op == nil || op.RequestBody == nil || op.RequestBody.Value == nil {
		return catalog.Param{}, false
	}
	rb := op.RequestBody.Value
	media, mt := requestMedia(rb.Content)
	if mt == nil || mt.Schema == nil {
		return catalog.Param{}, false
	}
	return catalog.Param{
		Name:        "body",
		In:          "body",
		Required:    rb.Required,
		Description: rb.Description,
		Schema:      schemaJSON(mt.Schema),
		MediaType:   media,
	}, true
}

func requestMedia(content openapi3.Content) (string, *openapi3.MediaType) {
	type pair struct {
		key string
		mt  *openapi3.MediaType
	}
	var pairs []pair
	for key, mt := range content {
		if mt == nil || mt.Schema == nil {
			continue
		}
		pairs = append(pairs, pair{key: key, mt: mt})
	}
	if len(pairs) == 0 {
		return "", nil
	}
	slices.SortFunc(pairs, func(a, b pair) int { return strings.Compare(a.key, b.key) })
	for _, p := range pairs {
		if p.key == jsonMedia {
			return p.key, p.mt
		}
	}
	for _, p := range pairs {
		if mediaBase(p.key) == jsonMedia {
			return p.key, p.mt
		}
	}
	return pairs[0].key, pairs[0].mt
}

func mediaBase(media string) string {
	media = strings.TrimSpace(media)
	if i := strings.IndexByte(media, ';'); i >= 0 {
		media = strings.TrimSpace(media[:i])
	}
	return media
}

func schemaDefault(ref *openapi3.SchemaRef) string {
	if ref == nil || ref.Value == nil || ref.Value.Default == nil {
		return ""
	}
	switch v := ref.Value.Default.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case json.Number:
		return v.String()
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func responseFields(op *openapi3.Operation) []string {
	if op == nil || op.Responses == nil {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	stack := map[*openapi3.Schema]bool{}
	for code, ref := range op.Responses.Map() {
		if !strings.HasPrefix(code, "2") || ref == nil || ref.Value == nil {
			continue
		}
		mt := ref.Value.Content.Get(jsonMedia)
		if mt == nil {
			continue
		}
		collectFields(mt.Schema, &names, seen, stack)
	}
	sort.Strings(names)
	return names
}

func collectFields(ref *openapi3.SchemaRef, names *[]string, seen map[string]bool, stack map[*openapi3.Schema]bool) {
	if ref == nil || ref.Value == nil || stack[ref.Value] {
		return
	}
	stack[ref.Value] = true
	defer delete(stack, ref.Value)
	for name := range ref.Value.Properties {
		if seen[name] {
			continue
		}
		seen[name] = true
		*names = append(*names, name)
	}
	collectFields(ref.Value.Items, names, seen, stack)
	for _, sub := range ref.Value.AllOf {
		collectFields(sub, names, seen, stack)
	}
	for _, sub := range ref.Value.OneOf {
		collectFields(sub, names, seen, stack)
	}
	for _, sub := range ref.Value.AnyOf {
		collectFields(sub, names, seen, stack)
	}
}

func schemaJSON(ref *openapi3.SchemaRef) string {
	node := inlineSchema(ref, map[*openapi3.Schema]bool{})
	if node == nil {
		return ""
	}
	b, err := json.Marshal(node)
	if err != nil {
		return ""
	}
	return string(b)
}

func inlineSchema(ref *openapi3.SchemaRef, seen map[*openapi3.Schema]bool) any {
	if ref == nil || ref.Value == nil {
		return nil
	}
	s := ref.Value
	if seen[s] {
		return map[string]any{}
	}
	seen[s] = true
	defer delete(seen, s)

	out := map[string]any{}
	if s.Type != nil && len(*s.Type) > 0 {
		types := []string(*s.Type)
		if len(types) == 1 {
			out["type"] = types[0]
		} else {
			out["type"] = types
		}
	}
	if s.Format != "" {
		out["format"] = s.Format
	}
	if s.Description != "" {
		out["description"] = s.Description
	}
	if len(s.Enum) > 0 {
		out["enum"] = s.Enum
	}
	if len(s.Required) > 0 {
		out["required"] = s.Required
	}
	if s.Items != nil {
		if item := inlineSchema(s.Items, seen); item != nil {
			out["items"] = item
		}
	}
	if len(s.Properties) > 0 {
		props := map[string]any{}
		for name, p := range s.Properties {
			if node := inlineSchema(p, seen); node != nil {
				props[name] = node
			}
		}
		out["properties"] = props
	}
	if len(s.AllOf) > 0 {
		out["allOf"] = inlineList(s.AllOf, seen)
	}
	if len(s.OneOf) > 0 {
		out["oneOf"] = inlineList(s.OneOf, seen)
	}
	if len(s.AnyOf) > 0 {
		out["anyOf"] = inlineList(s.AnyOf, seen)
	}
	if len(out) == 0 {
		return map[string]any{}
	}
	return out
}

func inlineList(refs openapi3.SchemaRefs, seen map[*openapi3.Schema]bool) []any {
	out := make([]any, 0, len(refs))
	for _, ref := range refs {
		if node := inlineSchema(ref, seen); node != nil {
			out = append(out, node)
		}
	}
	return out
}
