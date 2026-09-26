package mcpserver_test

import (
	"context"
	"strings"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/mcpserver"
	"github.com/aiveto/veto/openapi"
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
