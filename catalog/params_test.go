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
