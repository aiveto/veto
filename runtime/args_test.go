package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONTextUsesV2Defaults(t *testing.T) {
	got, err := jsonText(map[string]any{"note": "a<b>", "tags": []any{}})
	require.NoError(t, err)
	assert.Contains(t, got, `"note":"a<b>"`)
	assert.Contains(t, got, `"tags":[]`)
	assert.NotContains(t, got, `\u003c`)
}

func TestJSONTextEncodesANilSliceAsAnEmptyArray(t *testing.T) {
	var items []any
	got, err := jsonText(items)
	require.NoError(t, err)
	assert.Equal(t, "[]", got)
}
