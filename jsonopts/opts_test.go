package jsonopts

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestV1EncodesANilSliceAsNull(t *testing.T) {
	var items []string
	v2, err := (Set{}).Marshal(map[string]any{"tags": items})
	require.NoError(t, err)
	assert.Contains(t, string(v2), `"tags":[]`)

	v1, err := (Set{V1: true}).Marshal(map[string]any{"tags": items})
	require.NoError(t, err)
	assert.Contains(t, string(v1), `"tags":null`)
}

func TestV1UnmarshalsAPriorFile(t *testing.T) {
	var got struct {
		Tags []string `json:"tags"`
	}
	require.NoError(t, (Set{V1: true}).Unmarshal([]byte(`{"tags":null}`), &got))
	assert.Nil(t, got.Tags)
	require.NoError(t, (Set{}).Unmarshal([]byte(`{"tags":null}`), &got))
	assert.Nil(t, got.Tags)
}
