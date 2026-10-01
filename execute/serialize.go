package execute

import "github.com/aiveto/veto/catalog"

// Unserializable reports why a parameter cannot be placed on the request.
// An empty string means the call can send it.
func Unserializable(p catalog.Param) string {
	switch p.In {
	case "path":
		return unserializablePath(p)
	case "query":
		return unserializableQuery(p)
	case "header":
		return unserializableHeader(p)
	case "body", "":
		return ""
	default:
		return p.In + " parameters are not sent"
	}
}

func unserializablePath(p catalog.Param) string {
	style := p.Style
	if style == "" {
		style = "simple"
	}
	if style != "simple" {
		return "style " + style
	}
	if structured(p.Schema) {
		return schemaKind(p.Schema) + " path values are not sent"
	}
	return ""
}

func unserializableQuery(p catalog.Param) string {
	if !structured(p.Schema) {
		return ""
	}
	switch p.Style {
	case "form", "spaceDelimited", "pipeDelimited":
		return ""
	case "deepObject":
		if schemaType(p.Schema) != "object" {
			return "deepObject applies to an object"
		}
		return ""
	case "":
		return "style is missing"
	default:
		return "style " + p.Style
	}
}

func unserializableHeader(p catalog.Param) string {
	if structured(p.Schema) {
		return schemaKind(p.Schema) + " header values are not sent"
	}
	if p.Style != "" && p.Style != "simple" {
		return "style " + p.Style
	}
	return ""
}

func schemaKind(schema string) string {
	kind := schemaType(schema)
	if kind == "" {
		return "structured"
	}
	return kind
}
