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
	"github.com/aiveto/veto/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFollowWalksDeclaredRelation(t *testing.T) {
	cat := joined(t)
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.HasPrefix(r.URL.Path, "/orders/") {
			_, _ = w.Write([]byte(`{"id":"123","customerId":"7"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"7"}`))
	}))
	defer ts.Close()
	loop, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	require.NoError(t, err)
	calls, err := loop.Follow(context.Background(), "orders.get", map[string]string{"id": "123"}, "")
	require.NoError(t, err)
	require.Len(t, calls, 2)
	assert.Equal(t, "orders.get", calls[0].OperationID)
	assert.Equal(t, "customers.get", calls[1].OperationID)
	assert.Equal(t, []string{"/orders/123", "/customers/7"}, paths)
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
	_, err = loop.Follow(context.Background(), "orders.get", map[string]string{"id": "1"}, "")
	assert.ErrorContains(t, err, "customerId")
	assert.Equal(t, 1, hits)
}

func TestFollowDoesNotInventAnEdge(t *testing.T) {
	orders, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	customers, err := openapi.Load(context.Background(), "../testdata/customers.yaml")
	require.NoError(t, err)
	cat, err := catalog.Merge(orders, customers)
	require.NoError(t, err)
	var hits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"id":"1","customerId":"7"}`))
	}))
	defer ts.Close()
	loop, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	require.NoError(t, err)
	calls, err := loop.Follow(context.Background(), "orders.get", map[string]string{"id": "1"}, "")
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
		if r.URL.Path == "/orders/1" {
			_, _ = w.Write([]byte(`{"customerId":"7"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	loop, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	require.NoError(t, err)
	calls, err := loop.Follow(context.Background(), "orders.get", map[string]string{"id": "1"}, "")
	require.NoError(t, err)
	require.Len(t, calls, 2)
	assert.Equal(t, "customers.get", calls[1].OperationID)
	assert.Equal(t, []string{"/orders/1", "/customers/7"}, paths)

	listed := 0
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		listed++
		_, _ = w.Write([]byte(`[{"customerId":"7"}]`))
	}))
	defer ts2.Close()
	loop, err = agent.New(cat, nil, execute.Client{BaseURL: ts2.URL})
	require.NoError(t, err)
	calls, err = loop.Follow(context.Background(), "orders.list", nil, "")
	require.NoError(t, err)
	assert.Len(t, calls, 1)
	assert.Equal(t, 1, listed)
}

func TestFollowPointerAndUnmappedLink(t *testing.T) {
	orders, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	customers, err := openapi.Load(context.Background(), "../testdata/customers.yaml")
	require.NoError(t, err)
	base, err := catalog.Merge(orders, customers)
	require.NoError(t, err)

	cases := []struct {
		name    string
		expr    string
		body    string
		path    string
		hits    int
		wantErr string
	}{
		{name: "nested object", expr: "$response.body#/customer/id", body: `{"customer":{"id":"7"}}`, path: "/customers/7", hits: 2},
		{name: "one index", expr: "$response.body#/items/0/id", body: `{"items":[{"id":"7"}]}`, path: "/customers/7", hits: 2},
		{name: "index then object", expr: "$response.body#/addresses/0/customer/id", body: `{"addresses":[{"customer":{"id":"7"}}]}`, path: "/customers/7", hits: 2},
		{name: "top field", expr: "$response.body#/id", body: `{"id":"7"}`, path: "/customers/7", hits: 2},
		{name: "two indexes", expr: "$response.body#/rows/0/cols/1", body: `{"rows":[{"cols":["a","7"]}]}`, hits: 1, wantErr: "more than one index"},
		{name: "no parameter mapping", hits: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cat := *base
			cat.Links = append([]catalog.OpLink{}, base.Links...)
			if tc.expr != "" {
				cat.Links = append(cat.Links, catalog.OpLink{
					From:   "orders.get",
					To:     "customers.get",
					Params: map[string]string{"id": tc.expr},
				})
			}
			cat.Finalize()
			var paths []string
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				if r.URL.Path == "/orders/1" {
					_, _ = w.Write([]byte(tc.body))
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer ts.Close()
			loop, err := agent.New(&cat, nil, execute.Client{BaseURL: ts.URL})
			require.NoError(t, err)
			_, err = loop.Follow(context.Background(), "orders.get", map[string]string{"id": "1"}, "")
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Len(t, paths, tc.hits)
			if tc.path != "" {
				assert.Contains(t, paths, tc.path)
			} else {
				assert.NotContains(t, paths, "/customers/7")
			}
		})
	}
}

func TestFollowStopsAfterTheCallCap(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	for i := 0; i < 20; i++ {
		cat.Links = append(cat.Links, catalog.OpLink{
			From:   "orders.get",
			To:     "orders.get",
			Params: map[string]string{"id": "$response.body#/id"},
		})
	}
	cat.Finalize()
	var hits int
	loop, err := agent.New(cat, nil, countExec{hits: &hits, body: `{"id":"1"}`})
	require.NoError(t, err)
	calls, err := loop.Follow(context.Background(), "orders.get", map[string]string{"id": "1"}, "")
	assert.ErrorContains(t, err, "follow stopped after 8")
	assert.Len(t, calls, 8)
	assert.Equal(t, 8, hits)
}

type countExec struct {
	hits *int
	body string
}

func (c countExec) InvokeHTTPResult(ctx context.Context, op *catalog.Operation, params map[string]string) (result.HTTPResult, error) {
	*c.hits++
	return result.HTTPResult{Status: http.StatusOK, Body: c.body, Code: "ok"}, nil
}

func TestFollowStopsOnConfirmation(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	cat.Links = append(cat.Links, catalog.OpLink{From: "orders.get", To: "orders.delete", Note: "Order.id"})
	cat.Finalize()
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = w.Write([]byte(`{"id":"7"}`))
	}))
	defer ts.Close()
	loop, err := agent.New(cat, nil, execute.Client{BaseURL: ts.URL})
	require.NoError(t, err)
	calls, err := loop.Follow(context.Background(), "orders.get", map[string]string{"id": "7"}, "")
	require.NoError(t, err)
	require.Len(t, calls, 2)
	assert.Equal(t, "confirmation_required", calls[1].Status)
	assert.Equal(t, []string{"/orders/7"}, paths)
}

func joined(t *testing.T) *catalog.Catalog {
	t.Helper()
	orders, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	customers, err := openapi.Load(context.Background(), "../testdata/customers.yaml")
	require.NoError(t, err)
	cat, err := catalog.Merge(orders, customers)
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
  title: Orders
  version: "1"
servers:
  - url: http://127.0.0.1:9
paths:
  /orders:
    get:
      operationId: orders.list
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
              operationId: orders.get
  /orders/{id}:
    get:
      operationId: orders.get
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
            customer:
              operationId: customers.get
              parameters:
                id: $response.body#/customerId
  /customers/{id}:
    get:
      operationId: customers.get
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
