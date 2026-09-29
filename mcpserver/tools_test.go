package mcpserver_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"os"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/mcpserver"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/semantics"
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
	if len(names) != len(want) {
		t.Fatalf("got %d tools %v, want %v", len(names), names, want)
	}
	for i, w := range want {
		if names[i] != w {
			t.Fatalf("tool[%d]=%q want %q full=%v", i, names[i], w, names)
		}
	}
	namesDefault := mcpserver.RegisterTools(nil, mcpserver.Options{})
	if len(namesDefault) != 3 {
		t.Fatalf("default should be 3 tools, got %v", namesDefault)
	}
}

func TestGroupedStaysOneToolPerResource(t *testing.T) {
	var ops []catalog.Operation
	for i := 0; i < 50; i++ {
		group := "assets"
		if i >= 25 {
			group = "teams"
		}
		ops = append(ops, catalog.Operation{ID: group + ".op" + strconv.Itoa(i), Group: group})
	}
	cat := &catalog.Catalog{Operations: ops}
	cat.Finalize()
	names := mcpserver.RegisterTools(cat, mcpserver.Options{Grouped: true})
	if len(names) != 5 {
		t.Fatalf("tools: %d %v", len(names), names)
	}
	for _, n := range names {
		if strings.Contains(n, ".op") {
			t.Fatalf("registered an operation tool: %v", names)
		}
	}
}

func TestGroupedAddsOneToolPerResource(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{ID: "assets.list", Group: "assets"},
		{ID: "assets.get", Group: "assets"},
		{ID: "assets.delete", Group: "assets", Exposure: catalog.ExposureDiscovery},
	}}
	cat.Finalize()
	names := mcpserver.RegisterTools(cat, mcpserver.Options{Grouped: true})
	if len(names) != 4 || names[3] != "assets" {
		t.Fatalf("grouped tools: %v", names)
	}
	for _, n := range names {
		if n == "assets.list" || n == "assets.get" || n == "assets.delete" {
			t.Fatalf("grouped registered an operation tool: %v", names)
		}
	}
	if err := mcpserver.ValidatePins(cat, []string{"assets.delete"}); err == nil || !strings.Contains(err.Error(), "discovery-only") {
		t.Fatalf("pin error: %v", err)
	}
}

func TestDescribeIncludesLinkAndSchema(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	srv := &mcpserver.Server{Catalog: cat, Semantics: semantics.NewDerived(cat)}
	b, err := srv.Describe(context.Background(), "assets.list")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "assets.get") || !strings.Contains(text, "Holding") {
		t.Fatalf("describe: %s", text)
	}
}

func TestDescribeMatchesThePack(t *testing.T) {
	assets, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	teams, err := openapi.Load(context.Background(), "../testdata/teams.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Merge(assets, teams)
	if err != nil {
		t.Fatal(err)
	}
	relData, err := os.ReadFile("../testdata/relations.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rels, err := catalog.ParseRelations(relData)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.ApplyRelations(cat, rels); err != nil {
		t.Fatal(err)
	}
	sem := semantics.NewDerived(cat)
	op := cat.ByID("assets.get")
	line := runctx.OperationLine(cat, *op, sem.Note(op.ID).Text())
	pack := runctx.NewBuilder(8192).Build(cat, nil, op, sem, nil)
	if !strings.Contains(pack.Index, line) {
		t.Fatalf("pack missing describe line:\n%s\n%s", pack.Index, line)
	}
	srv := &mcpserver.Server{Catalog: cat, Semantics: sem}
	b, err := srv.Describe(context.Background(), "assets.get")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "id path required") || !strings.Contains(text, "teamsId") || !strings.Contains(text, "Holding.teamsId identifies teams.get") {
		t.Fatalf("describe: %s", text)
	}
}
