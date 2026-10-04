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
	require.ErrorContains(t, err, "JSON object")
	err = op.CheckParams(map[string]string{"body": "[1]"})
	require.ErrorContains(t, err, "JSON object")
	err = op.CheckParams(map[string]string{"body": `{"a":1}{"b":2}`})
	require.ErrorContains(t, err, "JSON object")
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
