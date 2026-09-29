package mcpserver_test

import (
	"context"
	"os"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/mcpserver"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/semantics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPToolListIsCapabilitiesPlusPins(t *testing.T) {
	names := mcpserver.RegisterTools(nil, mcpserver.Options{
		Pins:       []string{"assets.get"},
		DirectPins: true,
	})
	want := []string{
		"capabilities_search",
		"capabilities_describe",
		"capabilities_invoke",
		"assets.get",
	}
	assert.Equal(t, want, names)
	namesDefault := mcpserver.RegisterTools(nil, mcpserver.Options{})
	assert.Len(t, namesDefault, 3)
}

func TestGroupedAddsOneToolPerResource(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{ID: "assets.list", Group: "assets"},
		{ID: "assets.get", Group: "assets"},
		{ID: "assets.delete", Group: "assets", Exposure: catalog.ExposureDiscovery},
	}}
	cat.Finalize()
	names := mcpserver.RegisterTools(cat, mcpserver.Options{Grouped: true})
	assert.Equal(t, []string{"capabilities_search", "capabilities_describe", "capabilities_invoke", "assets"}, names)
	err := mcpserver.ValidatePins(cat, []string{"assets.delete"})
	assert.ErrorContains(t, err, "discovery-only")
}

func TestDescribeIncludesLinkAndSchema(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	require.NoError(t, err)
	srv := &mcpserver.Server{Catalog: cat, Semantics: semantics.NewDerived(cat)}
	b, err := srv.Describe("assets.list")
	require.NoError(t, err)
	text := string(b)
	assert.Contains(t, text, "assets.get")
	assert.Contains(t, text, "Holding")
}

func TestDescribeMatchesThePack(t *testing.T) {
	assets, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	require.NoError(t, err)
	teams, err := openapi.Load(context.Background(), "../testdata/teams.yaml")
	require.NoError(t, err)
	cat, err := catalog.Merge(assets, teams)
	require.NoError(t, err)
	relData, err := os.ReadFile("../testdata/relations.yaml")
	require.NoError(t, err)
	rels, err := catalog.ParseRelations(relData)
	require.NoError(t, err)
	require.NoError(t, catalog.ApplyRelations(cat, rels))
	sem := semantics.NewDerived(cat)
	op := cat.ByID("assets.get")
	line := runctx.OperationLine(cat, *op, sem.Note(op.ID).Text())
	pack := runctx.NewBuilder(8192).Build(cat, nil, op, sem, nil)
	assert.Contains(t, pack.Index, line)
	srv := &mcpserver.Server{Catalog: cat, Semantics: sem}
	b, err := srv.Describe("assets.get")
	require.NoError(t, err)
	text := string(b)
	assert.Contains(t, text, "id path required")
	assert.Contains(t, text, "teamsId")
	assert.Contains(t, text, "Holding.teamsId identifies teams.get")
}
