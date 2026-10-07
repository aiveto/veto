package runtime_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvokeNamesTheNextCallFromTheResponse(t *testing.T) {
	cat := joinedCatalog(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"1","customerId":"7"}`))
	}))
	defer ts.Close()
	out, err := invokeOrdersGet(t, cat, ts, nil)
	require.NoError(t, err)
	assert.Equal(t, "ok", out.Status)
	require.Len(t, out.NextCalls, 1)
	assert.Equal(t, "customers.get", out.NextCalls[0].OperationID)
	assert.Equal(t, map[string]string{"id": "7"}, out.NextCalls[0].Params)
}

func TestInvokeOmitsANextCallWhenTheFieldIsMissing(t *testing.T) {
	cat := joinedCatalog(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	defer ts.Close()
	out, err := invokeOrdersGet(t, cat, ts, nil)
	require.NoError(t, err)
	assert.Equal(t, "ok", out.Status)
	assert.Empty(t, out.NextCalls)
}

func TestInvokeOmitsANextCallWhenTheFieldIsProjectedAway(t *testing.T) {
	cat := joinedCatalog(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"1","customerId":"7"}`))
	}))
	defer ts.Close()
	out, err := invokeOrdersGet(t, cat, ts, []string{"id"})
	require.NoError(t, err)
	assert.Equal(t, "ok", out.Status)
	assert.NotContains(t, out.Body, "customerId")
	assert.Empty(t, out.NextCalls)
}

func TestInvokeOmitsNextCallsOnAFailedResponse(t *testing.T) {
	cat := joinedCatalog(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"id":"1","customerId":"7"}`))
	}))
	defer ts.Close()
	out, err := invokeOrdersGet(t, cat, ts, nil)
	require.NoError(t, err)
	assert.Equal(t, "not_found", out.Status)
	assert.Empty(t, out.NextCalls)
}

func TestInvokeOmitsNextCallsWhenConfirmationIsRequired(t *testing.T) {
	cat := joinedCatalog(t)
	var hits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	rt := runtime.Runtime{
		Catalog: cat,
		Policy:  policy.Builtin{},
		State:   policy.NewState(),
		Exec:    execute.Client{BaseURL: ts.URL, HTTP: ts.Client()},
	}
	out, err := rt.Invoke(context.Background(), runtime.Request{
		Operation: "orders.delete",
		Arguments: map[string]any{"id": "1"},
	})
	require.NoError(t, err)
	assert.Equal(t, runtime.StatusConfirmationRequired, out.Status)
	assert.Empty(t, out.NextCalls)
	assert.Equal(t, 0, hits)
}

func TestInvokeOmitsAHeaderNextCall(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{
			ID: "orders.get", Method: http.MethodGet, PathTemplate: "/orders/{id}",
			Params: []catalog.Param{{Name: "id", In: "path", Required: true}},
		},
		{
			ID: "secret.get", Method: http.MethodGet, PathTemplate: "/secret",
			Params: []catalog.Param{{Name: "Authorization", In: "header"}},
		},
	}, Links: []catalog.OpLink{{
		From: "orders.get", To: "secret.get", Params: map[string]string{"Authorization": "token"},
	}}}
	cat.Finalize()
	const secret = "s3cret-value"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"id":"1","token":"%s"}`, secret)
	}))
	defer ts.Close()
	out, err := invokeOrdersGet(t, cat, ts, nil)
	require.NoError(t, err)
	assert.Equal(t, "ok", out.Status)
	assert.Contains(t, out.Body, secret)
	assert.Empty(t, out.NextCalls)
}

func TestInvokeCapsNextCalls(t *testing.T) {
	ops := make([]catalog.Operation, 0, 10)
	ops = append(ops, catalog.Operation{
		ID: "orders.get", Method: http.MethodGet, PathTemplate: "/orders/{id}",
		Params: []catalog.Param{{Name: "id", In: "path", Required: true}},
	})
	links := make([]catalog.OpLink, 0, 9)
	for i := range 9 {
		id := fmt.Sprintf("next.get%d", i)
		ops = append(ops, catalog.Operation{
			ID: id, Method: http.MethodGet, PathTemplate: "/n/{id}",
			Params: []catalog.Param{{Name: "id", In: "path", Required: true}},
		})
		links = append(links, catalog.OpLink{From: "orders.get", To: id, Note: "Order.id"})
	}
	cat := &catalog.Catalog{Operations: ops, Links: links}
	cat.Finalize()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"7"}`))
	}))
	defer ts.Close()
	out, err := invokeOrdersGet(t, cat, ts, nil)
	require.NoError(t, err)
	assert.Len(t, out.NextCalls, 8)
}

func TestPointerIndexDoesNotWrap(t *testing.T) {
	_, _, err := runtime.LinkParams(catalog.OpLink{
		From: "orders.get", To: "customers.get",
		Params: map[string]string{"id": "$response.body#/18446744073709551616"},
	}, `["wrong"]`, nil)
	require.Error(t, err)

	got, ok, err := runtime.LinkParams(catalog.OpLink{
		From: "orders.get", To: "customers.get",
		Params: map[string]string{"id": "$response.body#/0"},
	}, `["right"]`, nil)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, map[string]string{"id": "right"}, got)
}

func invokeOrdersGet(t *testing.T, cat *catalog.Catalog, ts *httptest.Server, fields []string) (runtime.Result, error) {
	t.Helper()
	rt := runtime.Runtime{
		Catalog: cat,
		Policy:  policy.Builtin{},
		State:   policy.NewState(),
		Exec:    execute.Client{BaseURL: ts.URL, HTTP: ts.Client()},
	}
	return rt.Invoke(context.Background(), runtime.Request{
		Operation: "orders.get",
		Arguments: map[string]any{"id": "1"},
		Fields:    fields,
	})
}

func joinedCatalog(t *testing.T) *catalog.Catalog {
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
