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
			{ID: "assets.get", Name: "getAsset", Description: "Fetch one asset"},
			{ID: "assets.delete", Name: "deleteAsset", Description: "Remove an asset"},
			{ID: "teams.get", Name: "getTeam", Description: "Fetch one team"},
		},
		Uses: []catalog.SchemaUse{{OperationID: "assets.get", Name: "Holding"}},
	}
	require.NoError(t, catalog.ApplyRelations(cat, []catalog.Relation{{
		Schema: "Holding", Field: "teamsId", To: "teams.get",
	}}))
	body := "- operation: assets.delete\n  synonyms:\n    - retire\n"
	over, err := ParseOverlay([]byte(body), NewDerived(cat))
	require.NoError(t, err)
	patched := over.Note("assets.delete")
	assert.Equal(t, "Remove an asset", patched.Sentence)
	missing := over.Note("assets.get")
	assert.Equal(t, "Fetch one asset", missing.Sentence)
	assert.Equal(t, "Holding.teamsId identifies teams.get", missing.Relation)
}
