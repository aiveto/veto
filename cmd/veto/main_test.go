package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/memory"
	"github.com/aiveto/veto/model"
	"github.com/aiveto/veto/policy"
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

func TestBuildLoopConstructsDefaultsAndOpenAIHost(t *testing.T) {
	contract, err := filepath.Abs("../../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	scripted := filepath.Join(dir, "scripted.yaml")
	body := "memory: local\npolicy: builtin\nmodel: scripted\ncontracts:\n  - " + contract + "\n"
	if err := os.WriteFile(scripted, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	loop, _, err := buildLoop(nil, scripted, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loop.Model.(*model.Scripted); !ok {
		t.Fatalf("model: %T", loop.Model)
	}
	if _, ok := loop.Policy.(policy.Builtin); !ok {
		t.Fatalf("policy: %T", loop.Policy)
	}
	if _, ok := loop.Memory.(*memory.LocalMap); !ok {
		t.Fatalf("memory: %T", loop.Memory)
	}

	t.Setenv("OPENAI_API_KEY", "test-key")
	for _, tc := range []struct {
		name string
		yaml string
		want string
	}{
		{name: "empty", yaml: "model: openai\n", want: "https://api.openai.com/v1"},
		{name: "set", yaml: "model: openai\nmodel_base_url: http://127.0.0.1:9/v1\nmodel_name: gpt-test\n", want: "http://127.0.0.1:9/v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".yaml")
			text := tc.yaml + "contracts:\n  - " + contract + "\n"
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			loop, _, err := buildLoop(nil, path, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			live, ok := loop.Model.(*model.OpenAI)
			if !ok {
				t.Fatalf("model: %T", loop.Model)
			}
			if live.BaseURL != tc.want {
				t.Fatalf("base: %s", live.BaseURL)
			}
		})
	}
}
