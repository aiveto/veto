package catalog_test

import (
	"errors"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckParamsRequiresAJSONObject(t *testing.T) {
	op := &catalog.Operation{
		ID: "orders.create",
		Params: []catalog.Param{{
			Name: "body", In: "body", Required: true, Schema: `{"type":"object"}`,
		}},
	}
	err := op.CheckParams(map[string]string{"body": "not-json"})
	bad, ok := errors.AsType[result.BodyError](err)
	require.True(t, ok, err)
	assert.Equal(t, "is not JSON", bad.Reason)
	err = op.CheckParams(map[string]string{"body": "[1]"})
	bad, ok = errors.AsType[result.BodyError](err)
	require.True(t, ok, err)
	assert.Equal(t, "must be a JSON object", bad.Reason)
	err = op.CheckParams(map[string]string{"body": `{"a":1}{"b":2}`})
	_, ok = errors.AsType[result.BodyError](err)
	require.True(t, ok, err)
	assert.NoError(t, op.CheckParams(map[string]string{"body": `{"a":1}`}))
}

func TestCheckParamsChecksTheBodySchema(t *testing.T) {
	op := &catalog.Operation{
		ID: "customers.create",
		Params: []catalog.Param{{
			Name: "body", In: "body", Required: true, MediaType: "application/json",
			Schema: `{"type":"object","required":["name"],"properties":{"name":{"type":"string"},"age":{"type":"integer"}}}`,
		}},
	}
	const secret = "s3cret-value"
	err := op.CheckParams(map[string]string{"body": `{"name":"ada","age":"` + secret + `"}`})
	bad, ok := errors.AsType[result.BodyError](err)
	require.True(t, ok)
	assert.Equal(t, "/age", bad.Path)
	assert.NotContains(t, err.Error(), secret)

	_, ok = errors.AsType[result.BodyError](op.CheckParams(map[string]string{"body": `{"age":3}`}))
	assert.True(t, ok)
	assert.NoError(t, op.CheckParams(map[string]string{"body": `{"name":"ada","age":3,"extra":true}`}))
}

func TestCheckParamsRejectsAnIntegerEnumFloatWouldCollapse(t *testing.T) {
	op := &catalog.Operation{ID: "orders.get", Params: []catalog.Param{{
		Name: "id", In: "query", Schema: `{"type":"integer","enum":[9007199254740992]}`,
	}}}
	err := op.CheckParams(map[string]string{"id": "9007199254740993"})
	bad, ok := errors.AsType[result.ParamError](err)
	require.True(t, ok, err)
	assert.Equal(t, "value is not one of the allowed values", bad.Reason)
	assert.NoError(t, op.CheckParams(map[string]string{"id": "9007199254740992"}))

	small := &catalog.Operation{ID: "orders.get", Params: []catalog.Param{{
		Name: "id", In: "query", Schema: `{"type":"integer","enum":[2]}`,
	}}}
	assert.NoError(t, small.CheckParams(map[string]string{"id": "2"}))
	require.Error(t, small.CheckParams(map[string]string{"id": "3"}))
}

func TestCheckParamsRejectsABodyIntegerEnumFloatWouldCollapse(t *testing.T) {
	op := &catalog.Operation{ID: "orders.create", Params: []catalog.Param{{
		Name: "body", In: "body", Required: true, MediaType: "application/json",
		Schema: `{"type":"object","properties":{"id":{"type":"integer","enum":[9007199254740992]},"order":{"type":"object","properties":{"n":{"type":"integer","const":9007199254740992}}},"ns":{"type":"array","items":{"type":"integer","enum":[9007199254740992]}}}}`,
	}}}
	err := op.CheckParams(map[string]string{"body": `{"id":9007199254740993}`})
	bad, ok := errors.AsType[result.BodyError](err)
	require.True(t, ok, err)
	assert.Equal(t, "/id", bad.Path)
	assert.Equal(t, "value is not one of the allowed values", bad.Reason)
	require.NoError(t, op.CheckParams(map[string]string{"body": `{"id":9007199254740992}`}))

	err = op.CheckParams(map[string]string{"body": `{"order":{"n":9007199254740993}}`})
	bad, ok = errors.AsType[result.BodyError](err)
	require.True(t, ok, err)
	assert.Equal(t, "/order/n", bad.Path)
	assert.Equal(t, "value does not match const", bad.Reason)

	err = op.CheckParams(map[string]string{"body": `{"ns":[9007199254740993]}`})
	bad, ok = errors.AsType[result.BodyError](err)
	require.True(t, ok, err)
	assert.Equal(t, "/ns/0", bad.Path)
	require.NoError(t, op.CheckParams(map[string]string{"body": `{"ns":[9007199254740992]}`}))

	joined := &catalog.Operation{ID: "orders.create", Params: []catalog.Param{{
		Name: "body", In: "body", Required: true, MediaType: "application/json",
		Schema: `{"allOf":[{"type":"object","properties":{"id":{"type":"integer","enum":[9007199254740992]}}}]}`,
	}}}
	err = joined.CheckParams(map[string]string{"body": `{"id":9007199254740993}`})
	bad, ok = errors.AsType[result.BodyError](err)
	require.True(t, ok, err)
	assert.Equal(t, "/id", bad.Path)

	bound := &catalog.Operation{ID: "orders.create", Params: []catalog.Param{{
		Name: "body", In: "body", Required: true, MediaType: "application/json",
		Schema: `{"type":"object","properties":{"n":{"type":"integer","minimum":9007199254740992}}}`,
	}}}
	require.NoError(t, bound.CheckParams(map[string]string{"body": `{"n":9007199254740993}`}))
	inexact := &catalog.Operation{ID: "orders.create", Params: []catalog.Param{{
		Name: "body", In: "body", Required: true, MediaType: "application/json",
		Schema: `{"type":"object","properties":{"n":{"type":"integer","minimum":1e21}}}`,
	}}}
	err = inexact.CheckParams(map[string]string{"body": `{"n":9007199254740993}`})
	bad, ok = errors.AsType[result.BodyError](err)
	require.True(t, ok, err)
	assert.Equal(t, "integer cannot be checked exactly", bad.Reason)

	small := &catalog.Operation{ID: "orders.create", Params: []catalog.Param{{
		Name: "body", In: "body", Required: true, MediaType: "application/json",
		Schema: `{"type":"object","properties":{"age":{"type":"integer","enum":[2]}}}`,
	}}}
	require.NoError(t, small.CheckParams(map[string]string{"body": `{"age":2}`}))
	require.Error(t, small.CheckParams(map[string]string{"body": `{"age":3}`}))
}

func TestCheckParamsUsesPreparedSchema(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID: "customers.create",
		Params: []catalog.Param{{
			Name: "body", In: "body", Required: true, MediaType: "application/json",
			Schema: `{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`,
		}},
	}}}
	cat.Finalize()
	op := cat.ByID("customers.create")
	require.NotNil(t, op)
	_, ok := errors.AsType[result.BodyError](op.CheckParams(map[string]string{"body": `{}`}))
	assert.True(t, ok)
	assert.NoError(t, op.CheckParams(map[string]string{"body": `{"name":"ada"}`}))
}

func TestCheckParamsRejectsMalformedJSON(t *testing.T) {
	cases := []struct {
		name   string
		schema string
	}{
		{name: "object", schema: `{"type":"object"}`},
		{name: "array", schema: `{"type":"array","items":{"type":"string"}}`},
		{name: "integer", schema: `{"type":"integer"}`},
		{name: "allOf object", schema: `{"allOf":[{"type":"object","properties":{"id":{"type":"string"}}}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := &catalog.Operation{
				ID: "widgets.create",
				Params: []catalog.Param{{
					Name: "body", In: "body", Required: true, MediaType: "application/json", Schema: tc.schema,
				}},
			}
			err := op.CheckParams(map[string]string{"body": "not-json"})
			bad, ok := errors.AsType[result.BodyError](err)
			require.True(t, ok, err)
			assert.Equal(t, "is not JSON", bad.Reason)
		})
	}
}

func TestCheckParamsRejectsUnserializable(t *testing.T) {
	op := &catalog.Operation{
		ID: "labels.get",
		Params: []catalog.Param{{
			Name: "id", In: "path", Required: true, Style: "matrix", Schema: `{"type":"string"}`,
		}},
	}
	err := op.CheckParams(map[string]string{"id": "abc"})
	assert.ErrorContains(t, err, "cannot be serialized")
}

func TestCheckParamsChecksQueryPathAndHeaderSchema(t *testing.T) {
	op := &catalog.Operation{
		ID: "orders.list",
		Params: []catalog.Param{
			{Name: "status", In: "query", Schema: `{"type":"string","enum":["open","closed"]}`},
			{Name: "limit", In: "query", Schema: `{"type":"integer"}`},
			{Name: "id", In: "path", Required: true, Style: "simple", Schema: `{"type":"string"}`},
			{Name: "X-Trace", In: "header", Schema: `{"type":"boolean"}`},
		},
	}
	assert.NoError(t, op.CheckParams(map[string]string{"status": "open", "limit": "5", "id": "1", "X-Trace": "true"}))
	assert.NoError(t, op.CheckParams(map[string]string{"limit": "9007199254740993", "id": "1"}))

	const secret = "s3cret-value"
	err := op.CheckParams(map[string]string{"status": secret, "id": "1"})
	bad, ok := errors.AsType[result.ParamError](err)
	require.True(t, ok, err)
	assert.Equal(t, "status", bad.Name)
	assert.NotEmpty(t, bad.Reason)
	assert.NotContains(t, err.Error(), secret)

	err = op.CheckParams(map[string]string{"limit": "nope", "id": "1"})
	bad, ok = errors.AsType[result.ParamError](err)
	require.True(t, ok, err)
	assert.Equal(t, "limit", bad.Name)
	assert.Equal(t, "must be an integer", bad.Reason)

	err = op.CheckParams(map[string]string{"id": "1", "X-Trace": "maybe"})
	bad, ok = errors.AsType[result.ParamError](err)
	require.True(t, ok, err)
	assert.Equal(t, "X-Trace", bad.Name)
	assert.Equal(t, "must be a boolean", bad.Reason)
}

func TestCheckParamsRejectsAnIntegerBoundFloatWouldCollapse(t *testing.T) {
	const below = "9007199254740992"
	const bound = "9007199254740993"
	floor := `{"type":"integer","minimum":` + bound + `}`
	cases := []struct {
		name   string
		op     catalog.Operation
		params map[string]string
		fail   bool
	}{
		{
			name:   "query below an inexact minimum",
			op:     catalog.Operation{ID: "orders.list", Params: []catalog.Param{{Name: "n", In: "query", Schema: floor}}},
			params: map[string]string{"n": below},
			fail:   true,
		},
		{
			name:   "query equal to the minimum",
			op:     catalog.Operation{ID: "orders.list", Params: []catalog.Param{{Name: "n", In: "query", Schema: floor}}},
			params: map[string]string{"n": bound},
		},
		{
			name: "structured query",
			op: catalog.Operation{ID: "orders.list", Params: []catalog.Param{{
				Name: "filter", In: "query", Style: "deepObject",
				Schema: `{"type":"object","properties":{"n":` + floor + `}}`,
			}}},
			params: map[string]string{"filter": `{"n":` + below + `}`},
			fail:   true,
		},
		{
			name: "body additionalProperties",
			op: catalog.Operation{ID: "orders.create", Params: []catalog.Param{{
				Name: "body", In: "body", Required: true,
				Schema: `{"type":"object","properties":{"name":{"type":"string"}},"additionalProperties":` + floor + `}`,
			}}},
			params: map[string]string{"body": `{"name":"a","extra":` + below + `}`},
			fail:   true,
		},
		{
			name: "body additionalProperties at the minimum",
			op: catalog.Operation{ID: "orders.create", Params: []catalog.Param{{
				Name: "body", In: "body", Required: true,
				Schema: `{"type":"object","additionalProperties":` + floor + `}`,
			}}},
			params: map[string]string{"body": `{"extra":` + bound + `}`},
		},
		{
			name: "body oneOf",
			op: catalog.Operation{ID: "orders.create", Params: []catalog.Param{{
				Name: "body", In: "body", Required: true,
				Schema: `{"oneOf":[` + floor + `]}`,
			}}},
			params: map[string]string{"body": below},
			fail:   true,
		},
		{
			name: "body oneOf at the minimum",
			op: catalog.Operation{ID: "orders.create", Params: []catalog.Param{{
				Name: "body", In: "body", Required: true,
				Schema: `{"oneOf":[` + floor + `]}`,
			}}},
			params: map[string]string{"body": bound},
		},
		{
			name: "boolean exclusiveMinimum still rejects the bound",
			op: catalog.Operation{ID: "orders.list", Params: []catalog.Param{{
				Name: "n", In: "query", Schema: `{"type":"integer","minimum":0,"exclusiveMinimum":true}`,
			}}},
			params: map[string]string{"n": "0"},
			fail:   true,
		},
		{
			name: "boolean exclusiveMinimum allows the next integer",
			op: catalog.Operation{ID: "orders.list", Params: []catalog.Param{{
				Name: "n", In: "query", Schema: `{"type":"integer","minimum":0,"exclusiveMinimum":true}`,
			}}},
			params: map[string]string{"n": "1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.op.CheckParams(tc.params)
			if !tc.fail {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.NotContains(t, err.Error(), below)
		})
	}
}

func TestCheckParamsValidatesTheOutgoingValue(t *testing.T) {
	enum := `{"type":"string","enum":["safe"]}`
	op := &catalog.Operation{ID: "orders.list", Params: []catalog.Param{
		{Name: "q", In: "query", Schema: enum},
		{Name: "id", In: "path", Required: true, Schema: enum},
		{Name: "X-Trace", In: "header", Schema: enum},
		{Name: "need", In: "query", Required: true, Schema: `{"type":"string"}`},
	}}
	err := op.CheckParams(map[string]string{"q": " safe ", "id": "safe"})
	bad, ok := errors.AsType[result.ParamError](err)
	require.True(t, ok, err)
	assert.Equal(t, "q", bad.Name)
	assert.NotEmpty(t, bad.Reason)

	err = op.CheckParams(map[string]string{"q": "safe", "id": " safe "})
	bad, ok = errors.AsType[result.ParamError](err)
	require.True(t, ok, err)
	assert.Equal(t, "id", bad.Name)

	require.NoError(t, op.CheckParams(map[string]string{"q": "safe", "id": "safe", "X-Trace": " safe ", "need": "x"}))

	err = op.CheckParams(map[string]string{"q": "safe", "id": "safe", "need": "   "})
	bad, ok = errors.AsType[result.ParamError](err)
	require.True(t, ok, err)
	assert.Equal(t, "need", bad.Name)
	assert.Empty(t, bad.Reason)
}
