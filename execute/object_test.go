package execute_test

import (
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/stretchr/testify/assert"
)

func TestCheckParamsRequiresAJSONObject(t *testing.T) {
	op := &catalog.Operation{
		ID: "orders.create",
		Params: []catalog.Param{{
			Name: "body", In: "body", Required: true, Schema: `{"type":"object"}`,
		}},
	}
	err := execute.CheckParams(op, map[string]string{"body": "not-json"})
	assert.ErrorContains(t, err, "JSON object")
	err = execute.CheckParams(op, map[string]string{"body": "[1]"})
	assert.ErrorContains(t, err, "JSON object")
	err = execute.CheckParams(op, map[string]string{"body": `{"a":1}{"b":2}`})
	assert.ErrorContains(t, err, "JSON object")
	assert.NoError(t, execute.CheckParams(op, map[string]string{"body": `{"a":1}`}))
}

func TestCheckParamsRejectsUnserializable(t *testing.T) {
	op := &catalog.Operation{
		ID: "labels.get",
		Params: []catalog.Param{{
			Name: "id", In: "path", Required: true, Style: "matrix", Schema: `{"type":"string"}`,
		}},
	}
	err := execute.CheckParams(op, map[string]string{"id": "abc"})
	assert.ErrorContains(t, err, "cannot be serialized")
}
