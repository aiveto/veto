package catalog

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/aiveto/veto/result"
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
		if p.In == "body" && v != "" && p.Type() == "object" && !jsonObject(v) {
			return fmt.Errorf("operation %s: %s must be a JSON object", op.ID, p.Name)
		}
	}
	return nil
}

// Type is the single JSON Schema type of the parameter, or empty.
func (p Param) Type() string {
	if p.Schema == "" {
		return ""
	}
	var doc struct {
		Type any `json:"type"`
	}
	if err := json.Unmarshal([]byte(p.Schema), &doc); err != nil {
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

func jsonObject(raw string) bool {
	dec := json.NewDecoder(strings.NewReader(raw))
	var value any
	if err := dec.Decode(&value); err != nil {
		return false
	}
	if _, ok := value.(map[string]any); !ok {
		return false
	}
	var extra any
	return dec.Decode(&extra) == io.EOF
}
