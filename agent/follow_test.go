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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err)
	calls, err := loop.Follow(context.Background(), "assets.get", map[string]string{"id": "1"}, "")
	require.NoError(t, err)
	require.Len(t, calls, 2)
	assert.Equal(t, "assets.get", calls[0].OperationID)
	assert.Equal(t, "teams.get", calls[1].OperationID)
	assert.Equal(t, []string{"/assets/1", "/teams/9"}, paths)
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
	require.NoError(t, err)
	_, err = loop.Follow(context.Background(), "assets.get", map[string]string{"id": "1"}, "")
	assert.ErrorContains(t, err, "teamsId")
	assert.Equal(t, 1, hits)
}

func TestFollowDoesNotInventAnEdge(t *testing.T) {
	assets, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	require.NoError(t, err)
	teams, err := openapi.Load(context.Background(), "../testdata/teams.yaml")
	require.NoError(t, err)
	cat, err := catalog.Merge(assets, teams)
	require.NoError(t, err)
	var hits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"id":"1","teamsId":"9"}`))
	}))
	defer ts.Close()
	loop, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	require.NoError(t, err)
	calls, err := loop.Follow(context.Background(), "assets.get", map[string]string{"id": "1"}, "")
	require.NoError(t, err)
	assert.Len(t, calls, 1)
	assert.Equal(t, 1, hits)
}

func TestFollowUsesLinkParameterMapping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(linkSpec), 0o644))
	cat, err := openapi.Load(context.Background(), path)
	require.NoError(t, err)
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
	require.NoError(t, err)
	calls, err := loop.Follow(context.Background(), "assets.get", map[string]string{"id": "1"}, "")
	require.NoError(t, err)
	require.Len(t, calls, 2)
	assert.Equal(t, "teams.get", calls[1].OperationID)
	assert.Equal(t, []string{"/assets/1", "/teams/9"}, paths)

	listed := 0
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		listed++
		_, _ = w.Write([]byte(`[{"teamsId":"9"}]`))
	}))
	defer ts2.Close()
	loop, err = agent.New(cat, nil, execute.Client{BaseURL: ts2.URL})
	require.NoError(t, err)
	calls, err = loop.Follow(context.Background(), "assets.list", nil, "")
	require.NoError(t, err)
	assert.Len(t, calls, 1)
	assert.Equal(t, 1, listed)
}

func TestFollowStopsOnConfirmation(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/openapi.yaml")
	require.NoError(t, err)
	cat.Links = append(cat.Links, catalog.OpLink{From: "assets.get", To: "assets.delete", Note: "Holding.id"})
	cat.Finalize()
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = w.Write([]byte(`{"id":"7"}`))
	}))
	defer ts.Close()
	loop, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	require.NoError(t, err)
	calls, err := loop.Follow(context.Background(), "assets.get", map[string]string{"id": "7"}, "")
	require.NoError(t, err)
	require.Len(t, calls, 2)
	assert.Equal(t, "confirmation_required", calls[1].Status)
	assert.Equal(t, []string{"/assets/7"}, paths)
}

func joined(t *testing.T) *catalog.Catalog {
	t.Helper()
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
