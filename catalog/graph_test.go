package catalog_test

import (
	"context"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJoinsPrintTheRelation(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{ID: "assets.get", Name: "get"},
		{ID: "teams.get", Name: "team"},
	}, Links: []catalog.OpLink{{From: "assets.get", To: "teams.get", Note: "Holding.teamsId"}}}
	cat.Finalize()
	assert.Equal(t, []string{"assets.get --[Holding.teamsId]--> teams.get"}, cat.Joins())
}

func TestGraphLinksOperationsAndSchemas(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	require.NoError(t, err)
	assert.Equal(t, []string{"assets.get"}, cat.Graph.Related("assets.list"))
	assert.Equal(t, []string{"assets.delete"}, cat.Graph.Related("assets.get"))
	assert.Equal(t, []string{"Holding"}, cat.Graph.Schemas("assets.get"))
	assert.Contains(t, cat.Graph.OperationsForResource("assets"), "assets.delete")
}
