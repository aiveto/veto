package agent_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/openapi"
)

func TestFollowWalksDeclaredRelation(t *testing.T) {
	cat := joined(t)
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			_, _ = w.Write([]byte(`{"id":"1","teamsId":"9"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"9"}`))
	}))
	defer ts.Close()
	loop, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	calls, err := loop.Follow(context.Background(), "assets.get", map[string]string{"id": "1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].OperationID != "assets.get" || calls[1].OperationID != "teams.get" {
		t.Fatalf("calls: %+v", calls)
	}
	if len(paths) != 2 || paths[0] != "/assets/1" || paths[1] != "/teams/9" {
		t.Fatalf("paths: %v", paths)
	}
}

func TestFollowErrorsWhenTheFieldIsMissing(t *testing.T) {
	cat := joined(t)
	var hits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	defer ts.Close()
	loop, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = loop.Follow(context.Background(), "assets.get", map[string]string{"id": "1"}, "")
	if err == nil || !strings.Contains(err.Error(), "teamsId") {
		t.Fatalf("err: %v", err)
	}
	if hits != 1 {
		t.Fatalf("hits: %d", hits)
	}
}

func TestFollowDoesNotInventAnEdge(t *testing.T) {
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
	var hits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"id":"1","teamsId":"9"}`))
	}))
	defer ts.Close()
	loop, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	calls, err := loop.Follow(context.Background(), "assets.get", map[string]string{"id": "1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || hits != 1 {
		t.Fatalf("calls=%d hits=%d", len(calls), hits)
	}
}

func TestFollowUsesLinkParameterMapping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.yaml")
	if err := os.WriteFile(path, []byte(linkSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := openapi.Load(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/assets/1" {
			_, _ = w.Write([]byte(`{"teamsId":"9"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	loop, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	calls, err := loop.Follow(context.Background(), "assets.get", map[string]string{"id": "1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[1].OperationID != "teams.get" || len(paths) != 2 || paths[1] != "/teams/9" {
		t.Fatalf("calls=%+v paths=%v", calls, paths)
	}

	listed := 0
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		listed++
		_, _ = w.Write([]byte(`[{"teamsId":"9"}]`))
	}))
	defer ts2.Close()
	loop, err = agent.New(cat, nil, execute.Client{BaseURL: ts2.URL})
	if err != nil {
		t.Fatal(err)
	}
	calls, err = loop.Follow(context.Background(), "assets.list", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || listed != 1 {
		t.Fatalf("unmapped link was called: %+v hits=%d", calls, listed)
	}
}

func TestFollowStopsOnConfirmation(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cat.Links = append(cat.Links, catalog.OpLink{From: "assets.get", To: "assets.delete", Note: "Holding.id"})
	cat.Finalize()
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = w.Write([]byte(`{"id":"7"}`))
	}))
	defer ts.Close()
	loop, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	calls, err := loop.Follow(context.Background(), "assets.get", map[string]string{"id": "7"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[1].Status != "confirmation_required" {
		t.Fatalf("calls: %+v", calls)
	}
	if len(paths) != 1 || paths[0] != "/assets/7" {
		t.Fatalf("paths: %v", paths)
	}
}

func joined(t *testing.T) *catalog.Catalog {
	t.Helper()
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
	return cat
}

const linkSpec = `openapi: 3.0.3
info:
  title: Assets
  version: "1"
servers:
  - url: http://127.0.0.1:9
paths:
  /assets:
    get:
      operationId: assets.list
      responses:
        "200":
          description: list
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
          links:
            next:
              operationId: assets.get
  /assets/{id}:
    get:
      operationId: assets.get
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: one
          content:
            application/json:
              schema:
                type: object
          links:
            team:
              operationId: teams.get
              parameters:
                id: $response.body#/teamsId
  /teams/{id}:
    get:
      operationId: teams.get
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "204":
          description: ok
`
