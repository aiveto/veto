package main

import (
	"strings"
	"testing"

	"github.com/aiveto/veto/catalog"
)

func TestConfigIsTheCatalog(t *testing.T) {
	_, contracts, relations, _, err := resolve("../../testdata/veto.yaml", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := loadCatalog(contracts, relations)
	if err != nil {
		t.Fatal(err)
	}
	if cat.ByID("assets.get") == nil || cat.ByID("teams.get") == nil {
		t.Fatalf("config did not register both APIs: %s", cat.IndexLine())
	}
	matches := catalog.Search(cat, "teamsId", nil)
	var joined bool
	for _, m := range matches {
		if m.Operation.ID != "assets.get" {
			continue
		}
		for _, id := range m.Related {
			if id == "teams.get" {
				joined = true
			}
		}
	}
	if !joined {
		t.Fatalf("search did not walk the relation: %v", matches)
	}
	ser := cat.IndexLine()
	if strings.Contains(ser, "openapi:") {
		t.Fatal("index contains the spec")
	}
}
