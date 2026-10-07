package capability_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/aiveto/veto/capability"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runtime"
	"github.com/aiveto/veto/semantics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchDescribeInvokeFollowsTheRelation(t *testing.T) {
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
	sem := semantics.New(cat)

	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/orders/1":
			_, _ = w.Write([]byte(`{"id":"1","customerId":"7"}`))
		case "/customers/7":
			_, _ = w.Write([]byte(`{"id":"7"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	rt := &runtime.Runtime{
		Catalog: cat,
		Policy:  policy.Builtin{},
		State:   policy.NewState(),
		Exec:    execute.Client{BaseURL: ts.URL, HTTP: ts.Client()},
	}
	srv := &capability.Server{Catalog: cat, Semantics: sem, Calls: rt}

	var operationID, param string
	for _, hit := range srv.Search("order", 0, 8) {
		raw, err := srv.Describe(hit.ID)
		require.NoError(t, err)
		var described struct {
			Operation struct {
				Method       string `json:"Method"`
				PathTemplate string `json:"PathTemplate"`
				Params       []struct {
					Name string `json:"Name"`
					In   string `json:"In"`
				} `json:"Params"`
			} `json:"operation"`
			Relation string `json:"relation"`
		}
		require.NoError(t, json.Unmarshal(raw, &described))
		if described.Operation.Method != http.MethodGet || described.Operation.PathTemplate != "/orders/{id}" {
			continue
		}
		require.Contains(t, described.Relation, "identifies customers.get")
		operationID = hit.ID
		for _, p := range described.Operation.Params {
			if p.In == "path" {
				param = p.Name
			}
		}
		break
	}
	require.NotEmpty(t, operationID)
	require.NotEmpty(t, param)

	first, err := srv.Call(context.Background(), runtime.Request{
		Operation: operationID,
		Arguments: map[string]any{param: "1"},
	})
	require.NoError(t, err)
	require.Equal(t, "ok", first.Status)
	require.Len(t, first.NextCalls, 1)
	next := first.NextCalls[0]
	wire, err := json.Marshal(first)
	require.NoError(t, err)
	assert.Contains(t, string(wire), `"next_calls"`)

	second, err := srv.Call(context.Background(), runtime.Request{
		Operation: next.OperationID,
		Arguments: runtime.FromStrings(next.Params),
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", second.Status)
	assert.Equal(t, []string{"/orders/1", "/customers/7"}, paths)
}
