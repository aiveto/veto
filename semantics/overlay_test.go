package semantics

import (
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverlayMissingKeyKeepsDerivedSentence(t *testing.T) {
	cat := &catalog.Catalog{
		Operations: []catalog.Operation{
			{ID: "orders.get", Name: "getOrder", Description: "Fetch one order"},
			{ID: "orders.delete", Name: "deleteOrder", Description: "Remove an order"},
			{ID: "customers.get", Name: "getCustomer", Description: "Fetch one customer"},
		},
		Uses: []catalog.SchemaUse{{OperationID: "orders.get", Name: "Order"}},
	}
	require.NoError(t, catalog.ApplyRelations(cat, []catalog.Relation{{
		Schema: "Order", Field: "customerId", To: "customers.get",
	}}))
	body := "- operation: orders.delete\n  synonyms:\n    - retire\n"
	over, err := ParseOverlay([]byte(body), NewDerived(cat))
	require.NoError(t, err)
	patched := over.Note("orders.delete")
	assert.Equal(t, "Remove an order", patched.Sentence)
	missing := over.Note("orders.get")
	assert.Equal(t, "Fetch one order", missing.Sentence)
	assert.Equal(t, "Order.customerId identifies customers.get", missing.Relation)
}
