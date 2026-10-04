package mcpserver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCapabilitiesAreTheThreeTools(t *testing.T) {
	caps := Capabilities()
	require.Len(t, caps, 3)
	assert.Equal(t, SearchName, caps[0].Name)
	assert.Equal(t, SearchCommand, caps[0].Command)
	assert.Equal(t, DescribeName, caps[1].Name)
	assert.Equal(t, InvokeName, caps[2].Name)
	names := make([]string, 0, len(caps[0].Input))
	required := map[string]bool{}
	for _, f := range caps[0].Input {
		names = append(names, f.Name)
		required[f.Name] = f.Required
	}
	assert.Equal(t, []string{"query", "offset", "limit"}, names)
	assert.True(t, required["query"])
	assert.False(t, required["offset"])
	spec, ok := CapabilityByCommand(SearchCommand)
	require.True(t, ok)
	assert.Equal(t, SearchName, spec.Name)
	_, ok = CapabilityByCommand("serve")
	assert.False(t, ok)
}
