package mcpserver

import (
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/stretchr/testify/assert"
)

func TestGroupedDescriptionListsCallableIDs(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{ID: "orders.list", Group: "orders"},
		{ID: "orders.get", Group: "orders"},
		{ID: "orders.delete", Group: "orders", Exposure: catalog.ExposureDiscovery},
		{ID: "customers.get", Group: "customers"},
	}}
	cat.Finalize()
	got := groupedDescription(cat, "orders")
	assert.Contains(t, got, "orders.list")
	assert.Contains(t, got, "orders.get")
	assert.NotContains(t, got, "orders.delete")
	assert.NotContains(t, got, "customers.get")
	assert.Contains(t, got, "operation_id")
}
