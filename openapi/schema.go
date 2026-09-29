package openapi

import (
	"encoding/json"

	"github.com/aiveto/veto/catalog"
	"github.com/getkin/kin-openapi/openapi3"
)

const jsonMedia = "application/json"

func bodyParam(op *openapi3.Operation) (catalog.Param, bool) {
	if op == nil || op.RequestBody == nil || op.RequestBody.Value == nil {
		return catalog.Param{}, false
	}
	rb := op.RequestBody.Value
	mt := rb.Content.Get(jsonMedia)
	if mt == nil || mt.Schema == nil {
		return catalog.Param{}, false
	}
	return catalog.Param{
		Name:        "body",
		In:          "body",
		Required:    rb.Required,
		Description: rb.Description,
		Schema:      schemaJSON(mt.Schema),
	}, true
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
