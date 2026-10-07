package runtime_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/result"
	"github.com/aiveto/veto/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPageFollowCollectsAndDefaultStaysOne(t *testing.T) {
	var hits atomic.Int32
	op, ts := pageServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("cursor") == "b" {
			_, _ = w.Write([]byte(`{"items":[{"id":"2"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"1"}],"next":"b"}`))
	})
	defer ts.Close()

	single := runtime.Runtime{Catalog: pageCatalog(t, op), Exec: execute.Client{BaseURL: ts.URL}, Policy: policy.Builtin{}}
	one, err := single.Invoke(context.Background(), runtime.Request{Operation: "orders.list"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), hits.Load())
	assert.JSONEq(t, `{"items":[{"id":"1"}],"next":"b"}`, one.Body)

	hits.Store(0)
	rt := runtime.Runtime{Catalog: pageCatalog(t, op), Exec: execute.Client{BaseURL: ts.URL}, Policy: policy.Builtin{}, Pages: 5}
	many, err := rt.Invoke(context.Background(), runtime.Request{Operation: "orders.list"})
	require.NoError(t, err)
	assert.Equal(t, int32(2), hits.Load())
	assert.JSONEq(t, `[{"id":"1"},{"id":"2"}]`, many.Body)
	assert.False(t, many.Truncated)
}

func TestPageFollowStillWalksWhenFieldsAreSet(t *testing.T) {
	var hits atomic.Int32
	op, ts := pageServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("cursor") == "b" {
			_, _ = w.Write([]byte(`{"items":[{"id":"2","name":"bee"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"1","name":"aye"}],"next":"b"}`))
	})
	defer ts.Close()
	rt := runtime.Runtime{Catalog: pageCatalog(t, op), Exec: execute.Client{BaseURL: ts.URL}, Policy: policy.Builtin{}, Pages: 5}
	got, err := rt.Invoke(context.Background(), runtime.Request{Operation: "orders.list", Fields: []string{"id"}})
	require.NoError(t, err)
	assert.Equal(t, int32(2), hits.Load())
	assert.JSONEq(t, `[{"id":"1"},{"id":"2"}]`, got.Body)
	assert.False(t, got.Truncated)
	assert.NotContains(t, got.Body, "name")
	assert.NotContains(t, got.Body, "next")
}

func TestPageFollowDoesNotHideALaterFailure(t *testing.T) {
	op, ts := pageServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "b" {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"down"}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"1"}],"next":"b"}`))
	})
	defer ts.Close()
	rt := runtime.Runtime{Catalog: pageCatalog(t, op), Exec: execute.Client{BaseURL: ts.URL}, Policy: policy.Builtin{}, Pages: 5}
	_, err := rt.Invoke(context.Background(), runtime.Request{Operation: "orders.list"})
	require.ErrorContains(t, err, "http 503")
}

func TestPageFollowStopsAtTheByteBudget(t *testing.T) {
	pad := strings.Repeat("x", 200)
	op, ts := pageServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "b" {
			_, _ = w.Write([]byte(`{"items":[{"id":"` + pad + `b"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"` + pad + `a"}],"next":"b"}`))
	})
	defer ts.Close()
	rt := runtime.Runtime{Catalog: pageCatalog(t, op), Exec: execute.Client{BaseURL: ts.URL}, Policy: policy.Builtin{}, Pages: 5, MaxBody: 260}
	got, err := rt.Invoke(context.Background(), runtime.Request{Operation: "orders.list"})
	require.NoError(t, err)
	assert.True(t, got.Truncated)
	assert.Contains(t, got.Body, pad+"a")
	assert.NotContains(t, got.Body, pad+"b")
}

func TestPageFollowKeepsTruncationAfterProjection(t *testing.T) {
	pad := strings.Repeat("x", 200)
	op, ts := pageServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "b" {
			_, _ = w.Write([]byte(`{"items":[{"id":"` + pad + `b","name":"bee"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"` + pad + `a","name":"aye"}],"next":"b"}`))
	})
	defer ts.Close()
	rt := runtime.Runtime{Catalog: pageCatalog(t, op), Exec: execute.Client{BaseURL: ts.URL, MaxBody: 260}, Policy: policy.Builtin{}, Pages: 5, MaxBody: 260}
	got, err := rt.Invoke(context.Background(), runtime.Request{Operation: "orders.list", Fields: []string{"id"}})
	require.NoError(t, err)
	assert.True(t, got.Truncated)
	assert.Contains(t, got.Body, pad+"a")
	assert.NotContains(t, got.Body, pad+"b")
	assert.NotContains(t, got.Body, "name")
}

func TestPageFollowMarksACapThatLeavesALaterPage(t *testing.T) {
	var hits atomic.Int32
	op, ts := pageServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Query().Get("cursor") {
		case "c":
			_, _ = w.Write([]byte(`{"items":[{"id":"3","name":"cee"}]}`))
		case "b":
			_, _ = w.Write([]byte(`{"items":[{"id":"2","name":"bee"}],"next":"c"}`))
		default:
			_, _ = w.Write([]byte(`{"items":[{"id":"1","name":"aye"}],"next":"b"}`))
		}
	})
	defer ts.Close()
	rt := runtime.Runtime{Catalog: pageCatalog(t, op), Exec: execute.Client{BaseURL: ts.URL}, Policy: policy.Builtin{}, Pages: 2}
	got, err := rt.Invoke(context.Background(), runtime.Request{Operation: "orders.list", Fields: []string{"id"}})
	require.NoError(t, err)
	assert.Equal(t, int32(2), hits.Load())
	assert.True(t, got.Truncated)
	assert.JSONEq(t, `[{"id":"1"},{"id":"2"}]`, got.Body)
	assert.NotContains(t, got.Body, "name")
}

func TestPageFollowAppliesDeploymentFields(t *testing.T) {
	op, ts := pageServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "b" {
			_, _ = w.Write([]byte(`{"items":[{"id":"2","secret":"later"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"1","secret":"first"}],"next":"b"}`))
	})
	defer ts.Close()
	rt := runtime.Runtime{
		Catalog: pageCatalog(t, op),
		Exec:    execute.Client{BaseURL: ts.URL, Fields: []string{"id"}},
		Policy:  policy.Builtin{},
		Pages:   5,
	}
	got, err := rt.Invoke(context.Background(), runtime.Request{Operation: "orders.list"})
	require.NoError(t, err)
	assert.Contains(t, got.Body, `"id":"1"`)
	assert.Contains(t, got.Body, `"id":"2"`)
	assert.NotContains(t, got.Body, "secret")
}

func TestPageFollowKeepsEvidenceWhenALaterPageIsNotAPage(t *testing.T) {
	op, ts := pageServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "b" {
			_, _ = w.Write([]byte(`not-a-page`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"1"}],"next":"b"}`))
	})
	defer ts.Close()
	rt := runtime.Runtime{Catalog: pageCatalog(t, op), Exec: execute.Client{BaseURL: ts.URL}, Policy: policy.Builtin{}, Pages: 5}
	got, err := rt.Invoke(context.Background(), runtime.Request{Operation: "orders.list"})
	require.ErrorContains(t, err, "not a page")
	assert.True(t, got.HTTP)
	assert.True(t, got.Sent)
	assert.Equal(t, http.StatusOK, got.HTTPStatus)
}

func TestPageFollowRejectsADerivedCursorInArguments(t *testing.T) {
	var hits atomic.Int32
	op, ts := pageServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("cursor") == "b" {
			_, _ = w.Write([]byte(`{"items":[{"id":"secret"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"ok"}],"next":"b"}`))
	})
	defer ts.Close()
	hook := policy.Wrap(nil, func(ctx context.Context, _ *catalog.Operation) (policy.Decision, bool, error) {
		if policy.InputFrom(ctx).Arguments["cursor"] == "b" {
			return policy.DecisionDeny, true, nil
		}
		return policy.DecisionAllow, true, nil
	})
	rt := runtime.Runtime{Catalog: pageCatalog(t, op), Exec: execute.Client{BaseURL: ts.URL}, Policy: hook, Pages: 5}
	got, err := rt.Invoke(context.Background(), runtime.Request{Operation: "orders.list"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), hits.Load())
	assert.True(t, got.Truncated)
	assert.Contains(t, got.Body, `"ok"`)
	assert.NotContains(t, got.Body, "secret")
}

func TestPageFollowValidatesADerivedCursor(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"items":[{"id":"ok"}],"next":"nope"}`))
	}))
	defer ts.Close()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	spec := strings.ReplaceAll(pageEnumSpec, "http://127.0.0.1:9", ts.URL)
	require.NoError(t, os.WriteFile(path, []byte(spec), 0o600))
	cat, err := openapi.Load(context.Background(), path)
	require.NoError(t, err)
	rt := runtime.Runtime{Catalog: cat, Exec: execute.Client{BaseURL: ts.URL}, Policy: policy.Builtin{}, Pages: 5}
	got, err := rt.Invoke(context.Background(), runtime.Request{Operation: "orders.list"})
	require.Error(t, err)
	assert.Equal(t, int32(1), hits.Load())
	assert.True(t, got.HTTP)
	assert.True(t, got.Sent)
}

func TestPageFollowAuthorizesEachDerivedRequest(t *testing.T) {
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/private") {
			_, _ = w.Write([]byte(`{"items":[{"id":"secret"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"ok"}],"next":"private"}`))
	}))
	defer ts.Close()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(pageIDSpec, "http://127.0.0.1:9", ts.URL)), 0o600))
	cat, err := openapi.Load(context.Background(), path)
	require.NoError(t, err)
	hook := policy.Wrap(nil, func(ctx context.Context, _ *catalog.Operation) (policy.Decision, bool, error) {
		if policy.InputFrom(ctx).Params["id"] == "private" {
			return policy.DecisionDeny, true, nil
		}
		return policy.DecisionAllow, true, nil
	})
	rt := runtime.Runtime{Catalog: cat, Exec: execute.Client{BaseURL: ts.URL}, Policy: hook, Pages: 5}
	got, err := rt.Invoke(context.Background(), runtime.Request{Operation: "items.get", Arguments: map[string]any{"id": "public"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"/items/public"}, paths)
	assert.True(t, got.Truncated)
	assert.Contains(t, got.Body, `"ok"`)
	assert.NotContains(t, got.Body, "secret")
}

func TestPageFollowStopsWhenALaterPageNeedsConfirmation(t *testing.T) {
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/private") {
			_, _ = w.Write([]byte(`{"items":[{"id":"secret"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"ok"}],"next":"private"}`))
	}))
	defer ts.Close()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(pageIDSpec, "http://127.0.0.1:9", ts.URL)), 0o600))
	cat, err := openapi.Load(context.Background(), path)
	require.NoError(t, err)
	hook := policy.Wrap(nil, func(ctx context.Context, _ *catalog.Operation) (policy.Decision, bool, error) {
		if policy.InputFrom(ctx).Params["id"] == "private" {
			return policy.DecisionConfirmationNeeded, true, nil
		}
		return policy.DecisionAllow, true, nil
	})
	rt := runtime.Runtime{Catalog: cat, Exec: execute.Client{BaseURL: ts.URL}, Policy: hook, Pages: 5}
	got, err := rt.Invoke(context.Background(), runtime.Request{Operation: "items.get", Arguments: map[string]any{"id": "public"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"/items/public"}, paths)
	assert.True(t, got.Truncated)
	assert.NotContains(t, got.Body, "secret")
}

func TestPageFollowKeepsTypedPolicyArguments(t *testing.T) {
	op := catalog.Operation{
		ID: "orders.list", Method: http.MethodGet, PathTemplate: "/orders",
		Params: []catalog.Param{
			{Name: "cursor", In: "query", Schema: `{"type":"string"}`},
			{Name: "private", In: "query", Schema: `{"type":"boolean"}`},
		},
	}
	t.Run("derived boolean stays a boolean", func(t *testing.T) {
		op.Page = map[string]string{"cursor": "next", "private": "private"}
		var hits atomic.Int32
		var seen []any
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			if r.URL.Query().Get("cursor") == "b" {
				_, _ = w.Write([]byte(`{"items":[{"id":"secret"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[{"id":"ok"}],"next":"b","private":true}`))
		}))
		defer ts.Close()
		hook := policy.Wrap(nil, func(ctx context.Context, _ *catalog.Operation) (policy.Decision, bool, error) {
			v := policy.InputFrom(ctx).Arguments["private"]
			seen = append(seen, v)
			if v == true {
				return policy.DecisionDeny, true, nil
			}
			return policy.DecisionAllow, true, nil
		})
		rt := runtime.Runtime{Catalog: pageCatalog(t, &op), Exec: execute.Client{BaseURL: ts.URL}, Policy: hook, Pages: 5}
		got, err := rt.Invoke(context.Background(), runtime.Request{
			Operation: "orders.list",
			Arguments: map[string]any{"private": false},
		})
		require.NoError(t, err)
		assert.Equal(t, int32(1), hits.Load())
		assert.NotContains(t, got.Body, "secret")
		require.Len(t, seen, 2)
		assert.Equal(t, false, seen[0])
		assert.Equal(t, true, seen[1])
		assert.IsType(t, false, seen[1])
	})
	t.Run("an unchanged boolean stays false", func(t *testing.T) {
		listed := op
		listed.Page = map[string]string{"cursor": "next"}
		var hits atomic.Int32
		var seen []any
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			if r.URL.Query().Get("cursor") == "b" {
				_, _ = w.Write([]byte(`{"items":[{"id":"two"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[{"id":"one"}],"next":"b"}`))
		}))
		defer ts.Close()
		hook := policy.Wrap(nil, func(ctx context.Context, _ *catalog.Operation) (policy.Decision, bool, error) {
			seen = append(seen, policy.InputFrom(ctx).Arguments["private"])
			if policy.InputFrom(ctx).Arguments["private"] == true {
				return policy.DecisionDeny, true, nil
			}
			return policy.DecisionAllow, true, nil
		})
		rt := runtime.Runtime{Catalog: pageCatalog(t, &listed), Exec: execute.Client{BaseURL: ts.URL}, Policy: hook, Pages: 5}
		_, err := rt.Invoke(context.Background(), runtime.Request{
			Operation: "orders.list",
			Arguments: map[string]any{"private": false},
		})
		require.NoError(t, err)
		assert.Equal(t, int32(2), hits.Load())
		require.Len(t, seen, 2)
		assert.Equal(t, false, seen[0])
		assert.Equal(t, false, seen[1])
		assert.IsType(t, false, seen[1])
	})
	t.Run("direct boolean true is denied", func(t *testing.T) {
		var hits atomic.Int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
		}))
		defer ts.Close()
		hook := policy.Wrap(nil, func(ctx context.Context, _ *catalog.Operation) (policy.Decision, bool, error) {
			if policy.InputFrom(ctx).Arguments["private"] == true {
				return policy.DecisionDeny, true, nil
			}
			return policy.DecisionAllow, true, nil
		})
		rt := runtime.Runtime{Catalog: pageCatalog(t, &op), Exec: execute.Client{BaseURL: ts.URL}, Policy: hook, Pages: 5}
		got, err := rt.Invoke(context.Background(), runtime.Request{
			Operation: "orders.list",
			Arguments: map[string]any{"private": true},
		})
		require.NoError(t, err)
		assert.Equal(t, runtime.StatusDenied, got.Status)
		assert.Equal(t, int32(0), hits.Load())
		assert.False(t, got.HTTP)
		assert.False(t, got.Sent)
	})
}

func TestPageFollowKeepsEvidenceWhenCredentialsFail(t *testing.T) {
	var hits atomic.Int32
	op, ts := pageServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"items":[{"id":"ok"}],"next":"b"}`))
	})
	defer ts.Close()
	rt := runtime.Runtime{
		Catalog: pageCatalog(t, op),
		Exec:    &stopAfter{exec: execute.Client{BaseURL: ts.URL}},
		Policy:  policy.Builtin{},
		Pages:   5,
	}
	got, err := rt.Invoke(context.Background(), runtime.Request{Operation: "orders.list"})
	require.ErrorContains(t, err, "credentials")
	assert.Equal(t, int32(1), hits.Load())
	assert.True(t, got.HTTP)
	assert.True(t, got.Sent)
	assert.Equal(t, http.StatusOK, got.HTTPStatus)
	assert.Contains(t, got.Body, `"ok"`)
}

func TestInvokeRejectsTheQueryTextThatWouldBeSent(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	defer ts.Close()
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID: "orders.list", Method: http.MethodGet, PathTemplate: "/orders",
		Params: []catalog.Param{{Name: "q", In: "query", Schema: `{"type":"string","enum":["safe"]}`}},
	}}}
	cat.Finalize()
	rt := runtime.Runtime{Catalog: cat, Exec: execute.Client{BaseURL: ts.URL}, Policy: policy.Builtin{}}
	_, err := rt.Invoke(context.Background(), runtime.Request{
		Operation: "orders.list",
		Arguments: map[string]any{"q": " safe "},
	})
	require.Error(t, err)
	assert.Equal(t, int32(0), hits.Load())
	_, err = rt.Invoke(context.Background(), runtime.Request{
		Operation: "orders.list",
		Arguments: map[string]any{"q": "safe"},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), hits.Load())
}

type stopAfter struct {
	exec execute.Client
	n    int
}

func (s *stopAfter) InvokeHTTPResult(ctx context.Context, op *catalog.Operation, params map[string]string) (result.HTTPResult, error) {
	s.n++
	if s.n > 1 {
		return result.HTTPResult{}, errors.New("credentials")
	}
	return s.exec.InvokeHTTPResult(ctx, op, params)
}

func pageServer(t *testing.T, h http.HandlerFunc) (*catalog.Operation, *httptest.Server) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(pageSpec), 0o600))
	cat, err := openapi.Load(context.Background(), path)
	require.NoError(t, err)
	return cat.ByID("orders.list"), httptest.NewServer(h)
}

func pageCatalog(t *testing.T, op *catalog.Operation) *catalog.Catalog {
	t.Helper()
	cat := &catalog.Catalog{Operations: []catalog.Operation{*op}}
	cat.Finalize()
	return cat
}

const pageSpec = `openapi: 3.0.3
info:
  title: Orders
  version: "1"
servers:
  - url: http://127.0.0.1:9
paths:
  /orders:
    get:
      operationId: orders.list
      parameters:
        - name: cursor
          in: query
          schema:
            type: string
      responses:
        "200":
          description: page
          content:
            application/json:
              schema:
                type: object
                properties:
                  items:
                    type: array
                    items:
                      type: object
                  next:
                    type: string
          links:
            next:
              operationId: orders.list
              parameters:
                cursor: $response.body#/next
`

const pageEnumSpec = `openapi: 3.0.3
info:
  title: Orders
  version: "1"
servers:
  - url: http://127.0.0.1:9
paths:
  /orders:
    get:
      operationId: orders.list
      parameters:
        - name: cursor
          in: query
          schema:
            type: string
            enum: [b]
      responses:
        "200":
          description: page
          content:
            application/json:
              schema:
                type: object
                properties:
                  items:
                    type: array
                    items:
                      type: object
                  next:
                    type: string
          links:
            next:
              operationId: orders.list
              parameters:
                cursor: $response.body#/next
`

const pageIDSpec = `openapi: 3.0.3
info:
  title: Items
  version: "1"
servers:
  - url: http://127.0.0.1:9
paths:
  /items/{id}:
    get:
      operationId: items.get
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: page
          content:
            application/json:
              schema:
                type: object
                properties:
                  items:
                    type: array
                    items:
                      type: object
                  next:
                    type: string
          links:
            next:
              operationId: items.get
              parameters:
                id: $response.body#/next
`
