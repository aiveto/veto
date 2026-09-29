package catalog_test

import (
	"context"
	"fmt"
	"testing"

	"os"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/semantics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchCapsAndPrefersExactID(t *testing.T) {
	ops := []catalog.Operation{{ID: "item.get", Name: "Get item", Description: "fetch one"}}
	for i := 0; i < 20; i++ {
		ops = append(ops, catalog.Operation{
			ID:          fmt.Sprintf("widget.%02d", i),
			Description: "mentions item.get once",
			Name:        "mention",
		})
	}
	cat := &catalog.Catalog{Operations: ops}
	cat.Finalize()
	matches := catalog.Search(cat, "item.get", nil)
	require.NotEmpty(t, matches)
	assert.LessOrEqual(t, len(matches), 8)
	assert.Equal(t, "item.get", matches[0].Operation.ID)
}

func TestTagsAndPathNounAreSearchable(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID: "widgets.ping", Group: "widgets", Tags: []string{"retire"}, Name: "Ping", Description: "Ping",
	}}}
	cat.Finalize()
	sem := semantics.NewDerived(cat)
	var sawRetire, sawNoun bool
	for _, s := range sem.AllSynonyms()["widgets.ping"] {
		if s == "retire" {
			sawRetire = true
		}
		if s == "widgets" {
			sawNoun = true
		}
	}
	assert.True(t, sawRetire)
	assert.True(t, sawNoun)
	matches := catalog.Search(cat, "retire", sem.AllSynonyms())
	require.Len(t, matches, 1)
	assert.Equal(t, "widgets.ping", matches[0].Operation.ID)
}

func TestSearchPageReturnsTheNextWindow(t *testing.T) {
	var ops []catalog.Operation
	for i := 0; i < 10; i++ {
		ops = append(ops, catalog.Operation{
			ID:          fmt.Sprintf("item.%02d", i),
			Description: "mentions item.get once",
			Name:        "mention",
		})
	}
	cat := &catalog.Catalog{Operations: ops}
	cat.Finalize()
	first := catalog.SearchPage(cat, "item.get", nil, 0, 3)
	next := catalog.SearchPage(cat, "item.get", nil, 3, 3)
	require.Len(t, first, 3)
	require.Len(t, next, 3)
	assert.NotEqual(t, first[0].Operation.ID, next[0].Operation.ID)
}

func ids(matches []catalog.Match) []string {
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.Operation.ID
	}
	return out
}

func TestSearchRetireFindsDelete(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	require.NoError(t, err)
	base := semantics.NewDerived(cat)
	overlay, err := os.ReadFile("../testdata/semantics.yaml")
	require.NoError(t, err)
	sem, err := semantics.ParseOverlay(overlay, base)
	require.NoError(t, err)
	matches := catalog.Search(cat, "retire", sem.AllSynonyms())
	require.NotEmpty(t, matches)
	assert.Equal(t, "assets.delete", matches[0].Operation.ID)
}

func TestSearchSchemaNameFindsOperations(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	require.NoError(t, err)
	matches := catalog.Search(cat, "Holding", nil)
	got := map[string]bool{}
	for _, m := range matches {
		got[m.Operation.ID] = true
	}
	assert.True(t, got["assets.list"])
	assert.True(t, got["assets.get"])
	assert.False(t, got["assets.delete"])
}

func TestSearchFollowsDeclaredRelation(t *testing.T) {
	assets, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	require.NoError(t, err)
	teams, err := openapi.Load(context.Background(), "../testdata/teams.yaml")
	require.NoError(t, err)
	cat, err := catalog.Merge(assets, teams)
	require.NoError(t, err)
	before := catalog.Search(cat, "teamsId", nil)
	assert.Empty(t, before)
	relData, err := os.ReadFile("../testdata/relations.yaml")
	require.NoError(t, err)
	rels, err := catalog.ParseRelations(relData)
	require.NoError(t, err)
	require.NoError(t, catalog.ApplyRelations(cat, rels))
	matches := catalog.Search(cat, "teamsId", nil)
	var found bool
	for _, m := range matches {
		if m.Operation.ID != "assets.get" {
			continue
		}
		found = true
		ok := false
		for _, id := range m.Related {
			if id == "teams.get" {
				ok = true
			}
		}
		assert.True(t, ok)
	}
	assert.True(t, found)
}
