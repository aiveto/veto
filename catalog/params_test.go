package catalog_test

import (
	"testing"

	"github.com/aiveto/veto/catalog"
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
