package execute_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/aiveto/veto/execute"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInheritedPathParameterIsSent(t *testing.T) {
	var gotPath, gotLimit string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotLimit = r.URL.Query().Get("limit")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	cat := loadSpec(t, pathExecSpec)
	_, err := (execute.Client{BaseURL: ts.URL}).InvokeHTTPResult(context.Background(), cat.ByID("orders.get"), map[string]string{
		"id":    "123",
		"limit": "5",
	})
	require.NoError(t, err)
	assert.Equal(t, "/orders/123", gotPath)
	assert.Equal(t, "5", gotLimit)
}

func TestExecuteUsesOperationServerOverDocumentServer(t *testing.T) {
	var hits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		assert.Equal(t, "/orders/7", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	cat := loadSpec(t, fmt.Sprintf(serverExecSpec, ts.URL))
	op := cat.ByID("orders.get")
	require.Equal(t, ts.URL, op.BaseURL)
	_, err := (execute.Client{}).InvokeHTTPResult(context.Background(), op, map[string]string{"id": "7"})
	require.NoError(t, err)
	assert.Equal(t, 1, hits)
}

func TestExecuteUsesPathServerOverDocumentServer(t *testing.T) {
	var hits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		assert.Equal(t, "/items", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	cat := loadSpec(t, fmt.Sprintf(pathServerExecSpec, ts.URL))
	op := cat.ByID("items.list")
	require.Equal(t, ts.URL, op.BaseURL)
	_, err := (execute.Client{}).InvokeHTTPResult(context.Background(), op, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, hits)
}

func TestQueryStyleAndExplodeReachTheWire(t *testing.T) {
	var raw string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	cat := loadSpec(t, queryExecSpec)
	_, err := (execute.Client{BaseURL: ts.URL}).InvokeHTTPResult(context.Background(), cat.ByID("search.find"), map[string]string{
		"colors": `["red","blue"]`,
		"tags":   `["a","b"]`,
		"ids":    `["a","b"]`,
		"pipes":  `["a","b"]`,
		"filter": `{"status":"open","limit":1}`,
		"box":    `{"h":2,"w":1}`,
	})
	require.NoError(t, err)
	q, err := url.ParseQuery(raw)
	require.NoError(t, err)
	assert.Equal(t, []string{"red", "blue"}, q["colors"])
	assert.Equal(t, "a,b", q.Get("tags"))
	assert.Equal(t, "a b", q.Get("ids"))
	assert.Equal(t, "a|b", q.Get("pipes"))
	assert.Equal(t, "open", q.Get("filter[status]"))
	assert.Equal(t, "1", q.Get("filter[limit]"))
	assert.Equal(t, "h,2,w,1", q.Get("box"))
	assert.NotContains(t, raw, "%5B%22red%22")
}

func TestStructuredQueryKeepsLargeIntegersOnTheWire(t *testing.T) {
	const id = "9007199254740993"
	var raw string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	cat := loadSpec(t, queryExecSpec)
	_, err := (execute.Client{BaseURL: ts.URL}).InvokeHTTPResult(context.Background(), cat.ByID("search.find"), map[string]string{
		"ids":    `["` + id + `"]`,
		"filter": `{"limit":` + id + `}`,
	})
	require.NoError(t, err)
	assert.Contains(t, raw, id)
	assert.NotContains(t, raw, "9007199254740992")
}

const pathExecSpec = `openapi: 3.0.3
info: {title: t, version: "1"}
paths:
  /orders/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema: {type: string}
      - name: limit
        in: query
        schema: {type: integer}
    get:
      operationId: orders.get
      responses:
        "200": {description: ok}
`

const serverExecSpec = `openapi: 3.0.3
info: {title: t, version: "1"}
servers:
  - url: http://127.0.0.1:1
paths:
  /orders/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema: {type: string}
    get:
      operationId: orders.get
      servers:
        - url: %s
      responses:
        "200": {description: ok}
`

const pathServerExecSpec = `openapi: 3.0.3
info: {title: t, version: "1"}
servers:
  - url: http://127.0.0.1:1
paths:
  /items:
    servers:
      - url: %s
    get:
      operationId: items.list
      responses:
        "200": {description: ok}
`

const queryExecSpec = `openapi: 3.0.3
info: {title: t, version: "1"}
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
              limit: {type: integer}
        - name: box
          in: query
          style: form
          explode: false
          schema:
            type: object
            properties:
              h: {type: integer}
              w: {type: integer}
      responses:
        "200": {description: ok}
`
