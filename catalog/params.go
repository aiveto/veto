package catalog

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	"github.com/aiveto/veto/result"
	"github.com/getkin/kin-openapi/openapi3"
)

// CheckParams reports a missing required value, a parameter the call cannot send, or a body that is not the declared JSON object.
func (op Operation) CheckParams(params map[string]string) error {
	for _, p := range op.Params {
		if why := p.Unserializable(); why != "" {
			return fmt.Errorf("operation %s: parameter %s cannot be serialized: %s", op.ID, p.Name, why)
		}
		required := p.Required || p.In == "path"
		v := strings.TrimSpace(params[p.Name])
		if v == "" && p.In == "header" {
			v = strings.TrimSpace(p.Default)
		}
		if required && v == "" {
			return result.ParamError{Operation: op.ID, Name: p.Name}
		}
		if p.In == "body" && v != "" {
			if err := p.checkBody(op.ID, v); err != nil {
				return err
			}
			if p.Type() == "object" && !jsonObject(v) {
				return result.BodyError{Operation: op.ID, Path: "/", Reason: "must be a JSON object"}
			}
		}
	}
	return nil
}

// Type is the single JSON Schema type of the parameter, or empty.
func (p Param) Type() string {
	if p.typ != "" {
		return p.typ
	}
	return schemaType(p.Schema)
}

func prepareParams(params []Param) {
	for i := range params {
		params[i].typ = schemaType(params[i].Schema)
		if params[i].In != "body" || params[i].Schema == "" || !jsonMedia(params[i].MediaType) {
			continue
		}
		var schema openapi3.Schema
		if err := jsonv2.Unmarshal([]byte(params[i].Schema), &schema); err != nil {
			continue
		}
		params[i].body = &schema
	}
}

func schemaType(raw string) string {
	if raw == "" {
		return ""
	}
	var doc struct {
		Type any `json:"type"`
	}
	if err := jsonv2.Unmarshal([]byte(raw), &doc); err != nil {
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

// Structured reports an array or object schema.
func (p Param) Structured() bool {
	switch p.Type() {
	case "array", "object":
		return true
	default:
		return false
	}
}

// Unserializable reports why a parameter cannot be placed on the request.
// An empty string means the call can send it.
func (p Param) Unserializable() string {
	switch p.In {
	case "path":
		return p.unserializablePath()
	case "query":
		return p.unserializableQuery()
	case "header":
		return p.unserializableHeader()
	case "body", "":
		return ""
	default:
		return p.In + " parameters are not sent"
	}
}

func (p Param) unserializablePath() string {
	style := p.Style
	if style == "" {
		style = "simple"
	}
	if style != "simple" {
		return "style " + style
	}
	if p.Structured() {
		return p.kind() + " path values are not sent"
	}
	return ""
}

func (p Param) unserializableQuery() string {
	if !p.Structured() {
		return ""
	}
	switch p.Style {
	case "form", "spaceDelimited", "pipeDelimited":
		return ""
	case "deepObject":
		if p.Type() != "object" {
			return "deepObject applies to an object"
		}
		return ""
	case "":
		return "style is missing"
	default:
		return "style " + p.Style
	}
}

func (p Param) unserializableHeader() string {
	if p.Structured() {
		return p.kind() + " header values are not sent"
	}
	if p.Style != "" && p.Style != "simple" {
		return "style " + p.Style
	}
	return ""
}

func (p Param) kind() string {
	if kind := p.Type(); kind != "" {
		return kind
	}
	return "structured"
}

func (p Param) checkBody(operationID, raw string) error {
	if !jsonMedia(p.MediaType) {
		return nil
	}
	value, err := decodeJSON(raw)
	if err != nil {
		return result.BodyError{Operation: operationID, Path: "/", Reason: "is not JSON"}
	}
	if p.Type() == "object" {
		if _, ok := value.(map[string]any); !ok {
			return result.BodyError{Operation: operationID, Path: "/", Reason: "must be a JSON object"}
		}
	}
	if p.Schema == "" {
		return nil
	}
	schema := p.body
	if schema == nil {
		parsed, ok := parseSchema(p.Schema)
		if !ok {
			return nil
		}
		schema = parsed
	}
	err = schema.VisitJSON(value, openapi3.VisitAsRequest())
	if err == nil {
		return nil
	}
	bad := result.BodyError{Operation: operationID, Path: "/", Reason: "does not match the schema"}
	if se, ok := errors.AsType[*openapi3.SchemaError](err); ok {
		bad.Path = "/" + strings.Join(se.JSONPointer(), "/")
		bad.Reason = se.Reason
	}
	return bad
}

func decodeJSON(raw string) (any, error) {
	var value any
	if err := jsonv2.Unmarshal([]byte(raw), &value); err != nil {
		return nil, err
	}
	return value, nil
}

func parseSchema(schemaText string) (*openapi3.Schema, bool) {
	var schema openapi3.Schema
	if jsonv2.Unmarshal([]byte(schemaText), &schema) != nil {
		return nil, false
	}
	return &schema, true
}

func jsonMedia(media string) bool {
	if i := strings.IndexByte(media, ';'); i >= 0 {
		media = media[:i]
	}
	media = strings.TrimSpace(media)
	return media == "" || media == "application/json" || strings.HasSuffix(media, "+json")
}

func jsonObject(raw string) bool {
	var value any
	if err := jsonv2.Unmarshal([]byte(raw), &value); err != nil {
		return false
	}
	_, ok := value.(map[string]any)
	return ok
}
