package openapi_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPathItemParametersAreInherited(t *testing.T) {
	cat := loadFixture(t, pathParamSpec)
	list := cat.ByID("orders.list")
	require.NotNil(t, list)
	limit := paramNamed(t, list.Params, "limit")
	assert.Equal(t, "query", limit.In)
	assert.True(t, limit.Required)
	assert.Equal(t, "from operation", limit.Description)
	item := cat.ByID("orders.get")
	require.NotNil(t, item)
	id := paramNamed(t, item.Params, "id")
	assert.Equal(t, "path", id.In)
	assert.True(t, id.Required)
	verbose := paramNamed(t, item.Params, "verbose")
	assert.Equal(t, "query", verbose.In)
}

func TestServersPreferOperationThenPath(t *testing.T) {
	cat := loadFixture(t, serverSpec)
	assert.Equal(t, "http://doc.example", cat.ByID("orders.list").BaseURL)
	assert.Equal(t, "http://path.example", cat.ByID("items.list").BaseURL)
	assert.Equal(t, []string{"http://path.example"}, serverURLs(cat.ByID("items.list").Servers))
	got := cat.ByID("orders.get")
	assert.Equal(t, "http://op.example", got.BaseURL)
	assert.Equal(t, []string{"http://op.example"}, serverURLs(got.Servers))
}

func TestFallbackOperationIDsDoNotCollide(t *testing.T) {
	cat := loadFixture(t, idSpec)
	list := cat.ByID("orders.get")
	item := cat.ByID("orders.by.id.get")
	require.NotNil(t, list)
	require.NotNil(t, item)
	assert.Equal(t, "/orders", list.PathTemplate)
	assert.Equal(t, "/orders/{id}", item.PathTemplate)
	assert.NotEqual(t, list.ID, item.ID)
}

func TestQueryStyleAndExplodeAreKept(t *testing.T) {
	cat := loadFixture(t, queryStyleSpec)
	op := cat.ByID("search.find")
	require.NotNil(t, op)
	colors := paramNamed(t, op.Params, "colors")
	assert.Equal(t, "form", colors.Style)
	assert.True(t, colors.Explode)
	assert.Contains(t, colors.Schema, `"array"`)
	tags := paramNamed(t, op.Params, "tags")
	assert.Equal(t, "form", tags.Style)
	assert.False(t, tags.Explode)
	filter := paramNamed(t, op.Params, "filter")
	assert.Equal(t, "deepObject", filter.Style)
	assert.True(t, filter.Explode)
	assert.Contains(t, filter.Schema, `"object"`)
	ids := paramNamed(t, op.Params, "ids")
	assert.Equal(t, "spaceDelimited", ids.Style)
	assert.False(t, ids.Explode)
	pipes := paramNamed(t, op.Params, "pipes")
	assert.Equal(t, "pipeDelimited", pipes.Style)
	assert.False(t, pipes.Explode)
}

func loadFixture(t *testing.T, body string) *catalog.Catalog {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	cat, err := openapi.Load(context.Background(), path)
	require.NoError(t, err)
	return cat
}

func paramNamed(t *testing.T, params []catalog.Param, name string) catalog.Param {
	t.Helper()
	for _, p := range params {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("missing param %s", name)
	return catalog.Param{}
}

func serverURLs(servers []catalog.Server) []string {
	out := make([]string, 0, len(servers))
	for _, s := range servers {
		out = append(out, s.URL)
	}
	return out
}

const pathParamSpec = `openapi: 3.0.3
info:
  title: t
  version: "1"
paths:
  /orders:
    parameters:
      - name: limit
        in: query
        description: from path
        schema: {type: integer}
    get:
      operationId: orders.list
      parameters:
        - name: limit
          in: query
          description: from operation
          required: true
          schema: {type: integer}
      responses:
        "200": {description: ok}
  /orders/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema: {type: string}
    get:
      operationId: orders.get
      parameters:
        - name: verbose
          in: query
          schema: {type: boolean}
      responses:
        "200": {description: ok}
`

const serverSpec = `openapi: 3.0.3
info:
  title: t
  version: "1"
servers:
  - url: http://doc.example
paths:
  /orders:
    get:
      operationId: orders.list
      responses:
        "200": {description: ok}
  /items:
    servers:
      - url: http://path.example
    get:
      operationId: items.list
      responses:
        "200": {description: ok}
  /orders/{id}:
    servers:
      - url: http://path.example
    parameters:
      - name: id
        in: path
        required: true
        schema: {type: string}
    get:
      operationId: orders.get
      servers:
        - url: http://op.example
      responses:
        "200": {description: ok}
`

const idSpec = `openapi: 3.0.3
info:
  title: t
  version: "1"
paths:
  /orders:
    get:
      responses:
        "200": {description: ok}
  /orders/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema: {type: string}
    get:
      responses:
        "200": {description: ok}
`

const queryStyleSpec = `openapi: 3.0.3
info:
  title: t
  version: "1"
paths:
  /search:
    get:
      operationId: search.find
      parameters:
        - name: colors
          in: query
          style: form
          explode: true
          schema: {type: array, items: {type: string}}
        - name: tags
          in: query
          style: form
          explode: false
          schema: {type: array, items: {type: string}}
        - name: ids
          in: query
          style: spaceDelimited
          explode: false
          schema: {type: array, items: {type: string}}
        - name: pipes
          in: query
          style: pipeDelimited
          explode: false
          schema: {type: array, items: {type: string}}
        - name: filter
          in: query
          style: deepObject
          explode: true
          schema:
            type: object
            properties:
              status: {type: string}
      responses:
        "200": {description: ok}
`
