package mcpserver_test

import (
	"testing"

	"github.com/aiveto/veto/capability"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/mcpserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestGroupedNameCannotReplaceACapability(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{ID: "search.list", Group: "capabilities_search"},
	}}
	cat.Finalize()
	err := mcpserver.ValidateRegistration(cat, mcpserver.Options{Grouped: true})
	require.ErrorContains(t, err, "collides")
	require.ErrorContains(t, err, "capabilities_search")
}

func TestHandlerRejectsAGroupedCapabilityName(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{ID: "search.list", Group: "capabilities_search"},
	}}
	cat.Finalize()
	_, err := mcpserver.Handler(&capability.Server{Catalog: cat}, mcpserver.Options{Grouped: true}, []mcpserver.Identity{
		{ID: "ada", Token: "secret"},
	})
	require.ErrorContains(t, err, "collides")
}

func TestPinNameCannotReplaceACapability(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{ID: "capabilities_invoke", PathTemplate: "/invoke"},
	}}
	cat.Finalize()
	err := mcpserver.ValidateRegistration(cat, mcpserver.Options{Pins: []string{"capabilities_invoke"}, DirectPins: true})
	require.ErrorContains(t, err, "collides")
	require.ErrorContains(t, err, "capabilities_invoke")
}
