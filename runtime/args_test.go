package runtime

import (
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

func TestJSONTextV1EncodesANilSliceAsNull(t *testing.T) {
	var items []any
	got, err := jsonText(items, jsonopts.Set{V1: true})
	require.NoError(t, err)
	assert.Equal(t, "null", got)
}
