package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/agent/openai"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/memory"
	"github.com/aiveto/veto/policy"
)

func TestHelpJSONStaysOffTheHumanHelpPath(t *testing.T) {
	root := newRoot()
	var buf bytes.Buffer
	if jsonHelp(&buf, root, []string{"serve", "--help"}) {
		t.Fatal("human help was treated as JSON")
	}
	if !jsonHelp(&buf, root, []string{"serve", "--help-json"}) {
		t.Fatal("expected JSON help")
	}
	var doc struct {
		Command string `json:"command"`
		Flags   []struct {
			Name string `json:"name"`
		} `json:"flags"`
		Commands []string `json:"commands"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Command != "serve" || len(doc.Commands) != 0 {
		t.Fatalf("doc: %+v", doc)
	}
	var sawStdio bool
	for _, f := range doc.Flags {
		if f.Name == "stdio" {
			sawStdio = true
		}
	}
	if !sawStdio {
		t.Fatalf("flags: %+v", doc.Flags)
	}
	buf.Reset()
	if !jsonHelp(&buf, root, []string{"--help-json"}) {
		t.Fatal("expected root JSON help")
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(doc.Commands, ",")
	for _, name := range []string{"serve", "eval", "replay", "validate", "generate", "pack"} {
		if !strings.Contains(joined, name) {
			t.Fatalf("commands: %s", joined)
		}
	}
}

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
	if _, ok := loop.Model.(*agent.Scripted); !ok {
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
			live, ok := loop.Model.(*openai.Client)
			if !ok {
				t.Fatalf("model: %T", loop.Model)
			}
			if live.BaseURL != tc.want {
				t.Fatalf("base: %s", live.BaseURL)
			}
		})
	}
}

func TestPackPrintsTheDeleteCall(t *testing.T) {
	contract, err := filepath.Abs("../../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "veto.yaml")
	if err := os.WriteFile(path, []byte("contracts:\n  - "+contract+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loop, _, err := buildLoop(nil, path, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	text, err := packOutput(loop, "delete asset 123", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "assets.delete") {
		t.Fatalf("pack:\n%s", text)
	}
	raw, err := packOutput(loop, "delete asset 123", true)
	if err != nil {
		t.Fatal(err)
	}
	var pack struct {
		Index string `json:"Index"`
	}
	if err := json.Unmarshal([]byte(raw), &pack); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pack.Index, "assets.delete") {
		t.Fatalf("index: %s", pack.Index)
	}
}

func TestAuthSecretComesFromTheEnv(t *testing.T) {
	contract, err := filepath.Abs("../../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "veto.yaml")
	text := "auth:\n  bearerAuth: ASSET_TOKEN\ncontracts:\n  - " + contract + "\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "s3cret") {
		t.Fatal("secret was written into yaml")
	}
	t.Setenv("ASSET_TOKEN", "s3cret")
	loop, cfg, err := buildLoop(nil, path, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth["bearerAuth"] != "ASSET_TOKEN" {
		t.Fatalf("config auth: %#v", cfg.Auth)
	}
	exec, ok := loop.Exec.(execute.Client)
	if !ok || exec.Auth["bearerAuth"] != "s3cret" {
		t.Fatalf("client auth: %#v", loop.Exec)
	}
}
