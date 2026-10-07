package catalog

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/aiveto/veto/result"
	"github.com/getkin/kin-openapi/openapi3"
)

// CheckParams reports a missing required value, a parameter the call cannot send, a body that is not the declared JSON object, or a query, path, or header value that is not the declared schema.
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
			continue
		}
		if v != "" && (p.In == "query" || p.In == "path" || p.In == "header") {
			if err := p.checkValue(op.ID, v); err != nil {
				return err
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

func (p Param) checkValue(operationID, raw string) error {
	if p.Schema == "" {
		return nil
	}
	schema, ok := parseSchema(p.Schema)
	if !ok {
		return nil
	}
	value, err := valueForSchema(raw, p)
	if err != nil {
		return result.ParamError{Operation: operationID, Name: p.Name, Reason: err.Error()}
	}
	if p.Type() == "integer" {
		if err := exactInteger(raw, p.Schema); err != nil {
			return result.ParamError{Operation: operationID, Name: p.Name, Reason: err.Error()}
		}
	}
	if err := schema.VisitJSON(value, openapi3.VisitAsRequest()); err != nil {
		reason := "does not match the schema"
		if se, ok := errors.AsType[*openapi3.SchemaError](err); ok && se.Reason != "" {
			reason = se.Reason
		}
		return result.ParamError{Operation: operationID, Name: p.Name, Reason: reason}
	}
	return nil
}

func valueForSchema(raw string, p Param) (any, error) {
	if p.Structured() {
		value, err := decodeJSON(raw)
		if err != nil {
			return nil, errors.New("is not JSON")
		}
		return value, nil
	}
	return scalarValue(raw, p.Type())
}

func scalarValue(raw, typ string) (any, error) {
	switch typ {
	case "integer", "number":
		var n json.Number
		if err := json.Unmarshal([]byte(raw), &n); err != nil {
			return nil, scalarTypeError(typ)
		}
		if typ == "integer" && !integerNumber(n) {
			return nil, scalarTypeError(typ)
		}
		return n, nil
	case "boolean":
		switch raw {
		case "true":
			return true, nil
		case "false":
			return false, nil
		default:
			return nil, errors.New("must be a boolean")
		}
	default:
		return raw, nil
	}
}

func scalarTypeError(typ string) error {
	if typ == "integer" {
		return errors.New("must be an integer")
	}
	return errors.New("must be a number")
}

func exactInteger(raw, schemaText string) error {
	if schemaText == "" || !integerNumber(json.Number(raw)) {
		return nil
	}
	var doc struct {
		Enum             []json.RawMessage `json:"enum"`
		Const            json.RawMessage   `json:"const"`
		Minimum          json.RawMessage   `json:"minimum"`
		Maximum          json.RawMessage   `json:"maximum"`
		ExclusiveMinimum json.RawMessage   `json:"exclusiveMinimum"`
		ExclusiveMaximum json.RawMessage   `json:"exclusiveMaximum"`
		MultipleOf       json.RawMessage   `json:"multipleOf"`
	}
	if json.Unmarshal([]byte(schemaText), &doc) != nil {
		return nil
	}
	if len(doc.Enum) > 0 && !integerInEnum(raw, doc.Enum) {
		return errors.New("value is not one of the allowed values")
	}
	if jsonNumber(doc.Const) && !sameIntegerLiteral(raw, doc.Const) {
		return errors.New("value does not match const")
	}
	if !floatKeepsInteger(raw) && hasNumericBound(doc.Minimum, doc.Maximum, doc.ExclusiveMinimum, doc.ExclusiveMaximum, doc.MultipleOf) {
		return errors.New("integer cannot be checked exactly")
	}
	return nil
}

func integerInEnum(raw string, enum []json.RawMessage) bool {
	for _, item := range enum {
		if jsonNumber(item) && sameIntegerLiteral(raw, item) {
			return true
		}
	}
	return false
}

func sameIntegerLiteral(raw string, literal json.RawMessage) bool {
	left := new(big.Int)
	right := new(big.Int)
	if _, ok := left.SetString(strings.TrimPrefix(raw, "+"), 10); !ok {
		return false
	}
	text := strings.TrimSpace(string(literal))
	if _, ok := right.SetString(text, 10); ok {
		return left.Cmp(right) == 0
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(f, 0) || math.Trunc(f) != f {
		return false
	}
	back, acc := new(big.Float).SetFloat64(f).Int(nil)
	return acc == big.Exact && back.Cmp(left) == 0
}

func floatKeepsInteger(raw string) bool {
	raw = strings.TrimPrefix(raw, "+")
	n := new(big.Int)
	if _, ok := n.SetString(raw, 10); !ok {
		return false
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsInf(f, 0) || math.Trunc(f) != f {
		return false
	}
	back, acc := new(big.Float).SetFloat64(f).Int(nil)
	return acc == big.Exact && back.Cmp(n) == 0
}

func jsonNumber(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return false
	}
	switch raw[0] {
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	default:
		return false
	}
}

func hasNumericBound(parts ...json.RawMessage) bool {
	for _, part := range parts {
		if jsonNumber(part) {
			return true
		}
	}
	return false
}

func integerNumber(n json.Number) bool {
	s := string(n)
	if s == "" || strings.ContainsAny(s, ".eE") {
		return false
	}
	if s[0] == '-' || s[0] == '+' {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
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
