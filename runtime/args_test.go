package runtime

import (
	"encoding/json"
	"testing"

	"github.com/aiveto/veto/jsonopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONTextUsesV2Defaults(t *testing.T) {
	got, err := jsonText(map[string]any{"note": "a<b>", "tags": []any{}}, jsonopts.Set{})
	require.NoError(t, err)
	assert.Contains(t, got, `"note":"a<b>"`)
	assert.Contains(t, got, `"tags":[]`)
	assert.NotContains(t, got, `\u003c`)
}

func TestJSONTextEncodesANilSliceAsAnEmptyArray(t *testing.T) {
	var items []any
	got, err := jsonText(items, jsonopts.Set{})
	require.NoError(t, err)
	assert.Equal(t, "[]", got)
}

func TestWireIsStableAcrossKeyOrderAndJSONOpts(t *testing.T) {
	left := map[string]any{"body": map[string]any{"z": "1", "a": "2", "m": map[string]any{"k": "v", "b": "w"}}}
	right := map[string]any{"body": map[string]any{"a": "2", "m": map[string]any{"b": "w", "k": "v"}, "z": "1"}}
	a, err := wire(left, jsonopts.Set{})
	require.NoError(t, err)
	b, err := wire(right, jsonopts.Set{V1: true})
	require.NoError(t, err)
	assert.Equal(t, a, b)
	assert.Contains(t, a["body"], `"a":"2"`)
	assert.Contains(t, a["body"], `"z":"1"`)
}

func TestWireKeepsJSONNumberDigits(t *testing.T) {
	got, err := wire(map[string]any{"id": json.Number("9007199254740993")}, jsonopts.Set{})
	require.NoError(t, err)
	assert.Equal(t, "9007199254740993", got["id"])
}
