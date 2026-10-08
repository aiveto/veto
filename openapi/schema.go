package openapi

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
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
		b, err := jsonv2.Marshal(v)
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

func responseRequired(op *openapi3.Operation) []string {
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
		collectRequired(mt.Schema, &names, seen, stack)
	}
	sort.Strings(names)
	return names
}

func collectRequired(ref *openapi3.SchemaRef, names *[]string, seen map[string]bool, stack map[*openapi3.Schema]bool) {
	if ref == nil || ref.Value == nil || stack[ref.Value] {
		return
	}
	stack[ref.Value] = true
	defer delete(stack, ref.Value)
	for _, name := range ref.Value.Required {
		if ref.Value.Properties[name] == nil || seen[name] {
			continue
		}
		seen[name] = true
		*names = append(*names, name)
	}
	for _, sub := range ref.Value.AllOf {
		collectRequired(sub, names, seen, stack)
	}
	for _, sub := range ref.Value.OneOf {
		collectRequired(sub, names, seen, stack)
	}
	for _, sub := range ref.Value.AnyOf {
		collectRequired(sub, names, seen, stack)
	}
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
	s := cloneSchema(ref, map[*openapi3.Schema]bool{})
	if s == nil {
		return ""
	}
	b, err := jsonv2.Marshal(s)
	if err != nil {
		return ""
	}
	return string(b)
}

func cloneSchema(ref *openapi3.SchemaRef, seen map[*openapi3.Schema]bool) *openapi3.Schema {
	if ref == nil || ref.Value == nil {
		return nil
	}
	s := ref.Value
	if seen[s] {
		return &openapi3.Schema{}
	}
	seen[s] = true
	defer delete(seen, s)
	out := *s
	out.Items = cloneRef(s.Items, seen)
	out.Not = cloneRef(s.Not, seen)
	out.Contains = cloneRef(s.Contains, seen)
	out.PropertyNames = cloneRef(s.PropertyNames, seen)
	out.If = cloneRef(s.If, seen)
	out.Then = cloneRef(s.Then, seen)
	out.Else = cloneRef(s.Else, seen)
	out.AdditionalProperties.Schema = cloneRef(s.AdditionalProperties.Schema, seen)
	out.Properties = cloneSchemas(s.Properties, seen)
	out.PatternProperties = cloneSchemas(s.PatternProperties, seen)
	out.DependentSchemas = cloneSchemas(s.DependentSchemas, seen)
	out.AllOf = cloneRefs(s.AllOf, seen)
	out.OneOf = cloneRefs(s.OneOf, seen)
	out.AnyOf = cloneRefs(s.AnyOf, seen)
	out.PrefixItems = cloneRefs(s.PrefixItems, seen)
	return &out
}

func cloneRef(ref *openapi3.SchemaRef, seen map[*openapi3.Schema]bool) *openapi3.SchemaRef {
	s := cloneSchema(ref, seen)
	if s == nil {
		return nil
	}
	return &openapi3.SchemaRef{Value: s}
}

func cloneRefs(refs openapi3.SchemaRefs, seen map[*openapi3.Schema]bool) openapi3.SchemaRefs {
	if len(refs) == 0 {
		return nil
	}
	out := make(openapi3.SchemaRefs, 0, len(refs))
	for _, ref := range refs {
		if node := cloneRef(ref, seen); node != nil {
			out = append(out, node)
		}
	}
	return out
}

func cloneSchemas(in openapi3.Schemas, seen map[*openapi3.Schema]bool) openapi3.Schemas {
	if len(in) == 0 {
		return nil
	}
	out := make(openapi3.Schemas, len(in))
	for name, ref := range in {
		out[name] = cloneRef(ref, seen)
	}
	return out
}
