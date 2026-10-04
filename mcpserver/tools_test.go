package mcpserver_test

import (
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/mcpserver"
	"github.com/stretchr/testify/assert"
)

func TestMCPToolListIsCapabilitiesPlusPins(t *testing.T) {
	names := mcpserver.RegisterTools(nil, mcpserver.Options{
		Pins:       []string{"orders.get"},
		DirectPins: true,
	})
	want := []string{
		"capabilities_search",
		"capabilities_describe",
		"capabilities_invoke",
		"orders.get",
	}
	assert.Equal(t, want, names)
	namesDefault := mcpserver.RegisterTools(nil, mcpserver.Options{})
	assert.Len(t, namesDefault, 3)
}

func TestGroupedAddsOneToolPerResource(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{ID: "orders.list", Group: "orders"},
		{ID: "orders.get", Group: "orders"},
		{ID: "orders.delete", Group: "orders", Exposure: catalog.ExposureDiscovery},
	}}
	cat.Finalize()
	names := mcpserver.RegisterTools(cat, mcpserver.Options{Grouped: true})
	assert.Equal(t, []string{"capabilities_search", "capabilities_describe", "capabilities_invoke", "orders"}, names)
	err := mcpserver.ValidatePins(cat, []string{"orders.delete"})
	assert.ErrorContains(t, err, "discovery-only")
}
