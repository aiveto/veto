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
		raw := params[p.Name]
		present := strings.TrimSpace(raw)
		if present == "" && p.In == "header" {
			present = strings.TrimSpace(p.Default)
		}
		if required && present == "" {
			return result.ParamError{Operation: op.ID, Name: p.Name}
		}
		sent := raw
		if p.In == "header" {
			sent = present
		}
		if p.In == "body" && present != "" {
			if err := p.checkBody(op.ID, raw); err != nil {
				return err
			}
			if p.Type() == "object" && !jsonObject(raw) {
				return result.BodyError{Operation: op.ID, Path: "/", Reason: "must be a JSON object"}
			}
			continue
		}
		if sent != "" && (p.In == "query" || p.In == "path" || p.In == "header") {
			if err := p.checkValue(op.ID, sent); err != nil {
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
	} else if p.Structured() {
		if err := exactBodyIntegers(operationID, raw, p.Schema); err != nil {
			return paramReason(operationID, p.Name, err)
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
	if json.Unmarshal([]byte(schemaText), &doc) == nil {
		if len(doc.Enum) > 0 && !integerInEnum(raw, doc.Enum) {
			return errors.New("value is not one of the allowed values")
		}
		if jsonNumber(doc.Const) && !sameIntegerLiteral(raw, doc.Const) {
			return errors.New("value does not match const")
		}
		if err := integerBounds(raw, doc.Minimum, doc.Maximum, doc.ExclusiveMinimum, doc.ExclusiveMaximum, doc.MultipleOf); err != nil {
			return err
		}
	}
	return nil
}

func integerBounds(raw string, minimum, maximum, exclusiveMinimum, exclusiveMaximum, multipleOf json.RawMessage) error {
	n := new(big.Int)
	if _, ok := n.SetString(strings.TrimPrefix(raw, "+"), 10); !ok {
		return nil
	}
	if err := cmpBound(n, raw, minimum, "minimum"); err != nil {
		return err
	}
	if err := cmpBound(n, raw, exclusiveMinimum, "exclusiveMinimum"); err != nil {
		return err
	}
	if err := cmpBound(n, raw, maximum, "maximum"); err != nil {
		return err
	}
	if err := cmpBound(n, raw, exclusiveMaximum, "exclusiveMaximum"); err != nil {
		return err
	}
	return cmpMultiple(n, raw, multipleOf)
}

func boundInt(raw string, bound json.RawMessage) (*big.Int, bool, error) {
	if !jsonNumber(bound) {
		return nil, false, nil
	}
	n := new(big.Int)
	if _, ok := n.SetString(strings.TrimSpace(string(bound)), 10); ok {
		return n, true, nil
	}
	if !floatKeepsInteger(raw) {
		return nil, false, errors.New("integer cannot be checked exactly")
	}
	return nil, false, nil
}

func cmpBound(n *big.Int, raw string, bound json.RawMessage, kind string) error {
	limit, ok, err := boundInt(raw, bound)
	if err != nil || !ok {
		return err
	}
	cmp := n.Cmp(limit)
	switch kind {
	case "minimum":
		if cmp < 0 {
			return errors.New("integer is below minimum")
		}
	case "exclusiveMinimum":
		if cmp <= 0 {
			return errors.New("integer is not above exclusiveMinimum")
		}
	case "maximum":
		if cmp > 0 {
			return errors.New("integer is above maximum")
		}
	case "exclusiveMaximum":
		if cmp >= 0 {
			return errors.New("integer is not below exclusiveMaximum")
		}
	}
	return nil
}

func cmpMultiple(n *big.Int, raw string, bound json.RawMessage) error {
	limit, ok, err := boundInt(raw, bound)
	if err != nil || !ok {
		return err
	}
	if limit.Sign() == 0 {
		return errors.New("integer cannot be checked exactly")
	}
	if new(big.Int).Mod(n, limit).Sign() != 0 {
		return errors.New("integer is not a multiple of multipleOf")
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
	if err := exactBodyIntegers(operationID, raw, p.Schema); err != nil {
		return err
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

// exactBodyIntegers checks integer enum, const, and bounds on the raw JSON text.
// properties, additionalProperties, items, allOf, and integer oneOf or anyOf alternatives are walked.
func exactBodyIntegers(operationID, raw, schemaText string) error {
	if strings.TrimSpace(schemaText) == "" {
		return nil
	}
	return walkExactInteger(operationID, "", []byte(raw), []byte(schemaText))
}

func walkExactInteger(operationID, path string, raw, schema []byte) error {
	raw = bytes.TrimSpace(raw)
	schema = bytes.TrimSpace(schema)
	if len(schema) == 0 || schema[0] != '{' {
		return nil
	}
	var doc struct {
		Type                 string                     `json:"type"`
		Properties           map[string]json.RawMessage `json:"properties"`
		AdditionalProperties json.RawMessage            `json:"additionalProperties"`
		Items                json.RawMessage            `json:"items"`
		AllOf                []json.RawMessage          `json:"allOf"`
		OneOf                []json.RawMessage          `json:"oneOf"`
		AnyOf                []json.RawMessage          `json:"anyOf"`
	}
	if json.Unmarshal(schema, &doc) == nil {
		if doc.Type == "integer" && jsonNumber(raw) {
			if err := exactInteger(string(raw), string(schema)); err != nil {
				pointer := path
				if pointer == "" {
					pointer = "/"
				}
				return result.BodyError{Operation: operationID, Path: pointer, Reason: err.Error()}
			}
		}
		for _, sub := range doc.AllOf {
			if err := walkExactInteger(operationID, path, raw, sub); err != nil {
				return err
			}
		}
		if err := walkExactAlternatives(operationID, path, raw, doc.OneOf); err != nil {
			return err
		}
		if err := walkExactAlternatives(operationID, path, raw, doc.AnyOf); err != nil {
			return err
		}
		if err := walkExactProperties(operationID, path, raw, doc.Properties, doc.AdditionalProperties); err != nil {
			return err
		}
		return walkExactItems(operationID, path, raw, doc.Items)
	}
	return nil
}

func walkExactProperties(operationID, path string, raw []byte, properties map[string]json.RawMessage, additional []byte) error {
	additional = bytes.TrimSpace(additional)
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	if len(properties) == 0 && (len(additional) == 0 || additional[0] != '{') {
		return nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		for name, child := range obj {
			sub, ok := properties[name]
			if !ok {
				sub = additional
			}
			sub = bytes.TrimSpace(sub)
			if len(sub) == 0 || sub[0] != '{' {
				continue
			}
			if err := walkExactInteger(operationID, path+"/"+pointerToken(name), child, sub); err != nil {
				return err
			}
		}
	}
	return nil
}

func walkExactAlternatives(operationID, path string, raw []byte, branches []json.RawMessage) error {
	raw = bytes.TrimSpace(raw)
	if len(branches) == 0 || !jsonNumber(raw) {
		return nil
	}
	var rejected error
	saw := false
	accepted := false
	for _, sub := range branches {
		if !appliesToNumber(sub) {
			continue
		}
		saw = true
		if err := walkExactInteger(operationID, path, raw, sub); err != nil {
			rejected = err
			continue
		}
		accepted = true
	}
	if saw && !accepted && rejected != nil {
		return rejected
	}
	return nil
}

func appliesToNumber(schema []byte) bool {
	schema = bytes.TrimSpace(schema)
	if len(schema) == 0 || schema[0] != '{' {
		return false
	}
	var doc struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(schema, &doc) != nil {
		return false
	}
	switch doc.Type {
	case "", "number", "integer":
		return true
	default:
		return false
	}
}

func paramReason(operationID, name string, err error) error {
	if bad, ok := errors.AsType[result.BodyError](err); ok {
		return result.ParamError{Operation: operationID, Name: name, Reason: bad.Reason}
	}
	return result.ParamError{Operation: operationID, Name: name, Reason: err.Error()}
}

func walkExactItems(operationID, path string, raw, items []byte) error {
	items = bytes.TrimSpace(items)
	if len(items) == 0 || items[0] != '{' || len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		for i, item := range arr {
			if err := walkExactInteger(operationID, path+"/"+strconv.Itoa(i), item, items); err != nil {
				return err
			}
		}
	}
	return nil
}

func pointerToken(name string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(name)
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
