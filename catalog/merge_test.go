package catalog_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/openapi"
)

type relationSuite struct {
	suite.Suite
	assets *catalog.Catalog
	teams  *catalog.Catalog
	cat    *catalog.Catalog
}

func (s *relationSuite) SetupTest() {
	var err error
	s.assets, err = openapi.Load(context.Background(), "../testdata/openapi.yaml")
	s.Require().NoError(err)
	s.teams, err = openapi.Load(context.Background(), "../testdata/teams.yaml")
	s.Require().NoError(err)
	s.cat, err = catalog.Merge(s.assets, s.teams)
	s.Require().NoError(err)
}

func (s *relationSuite) TestJoinAppearsOnlyAfterDeclaration() {
	s.NotContains(s.cat.Graph.Related("assets.get"), "teams.get")
	relData, err := os.ReadFile("../testdata/relations.yaml")
	s.Require().NoError(err)
	rels, err := catalog.ParseRelations(relData)
	s.Require().NoError(err)
	s.Require().NoError(catalog.ApplyRelations(s.cat, rels))
	s.Contains(s.cat.Graph.Related("assets.get"), "teams.get")
	s.Contains(s.cat.Joins(), "assets.get --[Holding.teamsId]--> teams.get")
	s.NotEqual(s.assets.ByID("assets.get").BaseURL, s.teams.ByID("teams.get").BaseURL)
}

func (s *relationSuite) TestRejectsUnusedSchemaAndMissingTarget() {
	err := catalog.ApplyRelations(s.cat, []catalog.Relation{{Schema: "Missing", Field: "id", To: "teams.get"}})
	s.ErrorContains(err, "not used")
	err = catalog.ApplyRelations(s.cat, []catalog.Relation{{Schema: "Holding", Field: "teamsId", To: "missing.get"}})
	s.ErrorContains(err, "unknown operation")
}

func TestRelations(t *testing.T) {
	suite.Run(t, new(relationSuite))
}

func TestDuplicateOperationRefused(t *testing.T) {
	assets, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	require.NoError(t, err)
	_, err = catalog.Merge(assets, assets)
	assert.ErrorContains(t, err, "duplicate operation")
}
