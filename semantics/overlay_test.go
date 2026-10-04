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
	over, err := ParseOverlay([]byte(body), New(cat))
	require.NoError(t, err)
	patched := over.Note("orders.delete")
	assert.Equal(t, "Remove an order", patched.Sentence)
	missing := over.Note("orders.get")
	assert.Equal(t, "Fetch one order", missing.Sentence)
	assert.Equal(t, "Order.customerId identifies customers.get", missing.Relation)
}

func TestNoteSynonymsAreCallerOwned(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID: "widgets.ping", Name: "Ping", Description: "Ping", Tags: []string{"retire"},
	}}}
	cat.Finalize()
	sem := New(cat)
	got := sem.Note("widgets.ping")
	got.Synonyms = append(got.Synonyms, "injected")
	again := sem.Note("widgets.ping")
	assert.NotContains(t, again.Synonyms, "injected")
}

func TestAllSynonymsIsCallerOwned(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID: "widgets.ping", Name: "Ping", Description: "Ping", Tags: []string{"retire"},
	}}}
	cat.Finalize()
	sem := New(cat)
	got := sem.AllSynonyms()
	got["widgets.ping"] = append(got["widgets.ping"], "injected")
	got["other"] = []string{"nope"}
	again := sem.AllSynonyms()
	assert.NotContains(t, again["widgets.ping"], "injected")
	_, ok := again["other"]
	assert.False(t, ok)
}

func TestOverlayRejectsABadFile(t *testing.T) {
	base := New(&catalog.Catalog{})
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "unknown field", body: "- operation: orders.delete\n  synonims:\n    - scrap\n", want: "synonims"},
		{name: "duplicate field", body: "- operation: orders.delete\n  operation: orders.get\n", want: `duplicate field "operation"`},
		{name: "missing operation", body: "- sentence: orphan\n", want: "operation required"},
		{name: "duplicate operation", body: "- operation: orders.delete\n  sentence: one\n- operation: orders.delete\n  sentence: two\n", want: `duplicate operation "orders.delete"`},
		{name: "alias cycle", body: "- &a\n  operation: orders.delete\n  sentence: *a\n", want: "alias cycle"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseOverlay([]byte(tc.body), base)
			require.ErrorContains(t, err, tc.want)
		})
	}
}
