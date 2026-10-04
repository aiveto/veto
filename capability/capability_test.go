package capability

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllAreTheThreeTools(t *testing.T) {
	caps := All()
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
	spec, ok := ByCommand(SearchCommand)
	require.True(t, ok)
	assert.Equal(t, SearchName, spec.Name)
	_, ok = ByCommand("serve")
	assert.False(t, ok)
	help := HelpJSON()
	require.Len(t, help.Capabilities, 3)
	assert.Equal(t, SearchName, help.Capabilities[0].Name)
}
