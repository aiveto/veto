package catalog_test

import (
	"context"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/openapi"
)

func TestGraphGroupsAssetsAndLinksDelete(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var deleteLinked bool
	for _, e := range cat.Graph.Edges {
		if e.From == "assets" && e.To == "assets.delete" {
			deleteLinked = true
		}
	}
	if !deleteLinked {
		t.Fatalf("expected edge assets -> assets.delete, graph=%v", cat.Graph.Edges)
	}
	var hasResource, hasDelete bool
	for _, n := range cat.Graph.Nodes {
		if n.Kind == catalog.NodeResource && n.ID == "assets" {
			hasResource = true
		}
		if n.ID == "assets.delete" {
			hasDelete = true
		}
	}
	if !hasResource || !hasDelete {
		t.Fatalf("missing nodes: resource=%v delete=%v nodes=%v", hasResource, hasDelete, cat.Graph.Nodes)
	}
}

func TestGraphLinksOperationsAndSchemas(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	related := cat.Graph.Related("assets.list")
	if len(related) != 1 || related[0] != "assets.get" {
		t.Fatalf("list links: %v", related)
	}
	related = cat.Graph.Related("assets.get")
	if len(related) != 1 || related[0] != "assets.delete" {
		t.Fatalf("get links: %v", related)
	}
	schemas := cat.Graph.Schemas("assets.get")
	if len(schemas) != 1 || schemas[0] != "Holding" {
		t.Fatalf("schemas: %v", schemas)
	}
	var schemaNode bool
	for _, n := range cat.Graph.Nodes {
		if n.Kind == catalog.NodeSchema && n.ID == "Holding" {
			schemaNode = true
		}
	}
	if !schemaNode {
		t.Fatalf("missing schema node: %v", cat.Graph.Nodes)
	}
}
