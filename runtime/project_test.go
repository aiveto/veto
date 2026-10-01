package runtime_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runtime"
	"github.com/aiveto/veto/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectedResponseKeepsNamedFieldsAndMarksTheCap(t *testing.T) {
	const secret = "super-secret-token"
	blob := strings.Repeat("BLOBDATA", 30)
	one := fmt.Sprintf(`{"id":"0","name":"row","access_token":"%s","blob":"%s"}`, secret, blob)
	var raw strings.Builder
	raw.WriteByte('[')
	for i := 0; i < 30; i++ {
		if i > 0 {
			raw.WriteByte(',')
		}
		fmt.Fprintf(&raw, `{"id":"%d","name":"row","access_token":"%s","blob":"%s"}`, i, secret, blob)
	}
	raw.WriteByte(']')
	body := raw.String()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer ts.Close()

	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID:           "orders.list",
		Method:       http.MethodGet,
		PathTemplate: "/orders",
	}}}
	cat.Finalize()
	// One complete object fits. The rest of the array is past the cap.
	maxBody := int64(len("[") + len(one))

	rec, err := telemetry.Record()
	require.NoError(t, err)
	defer rec.Stop(context.Background())

	rt := runtime.Runtime{
		Catalog: cat,
		State:   policy.NewState(),
		Exec:    execute.Client{BaseURL: ts.URL, MaxBody: maxBody, RecordBody: true},
	}
	out, err := rt.Invoke(context.Background(), runtime.Request{
		Operation: "orders.list",
		Fields:    []string{"id", "name"},
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", out.Status)
	assert.True(t, out.Truncated)
	require.NotNil(t, out.Page)
	assert.Greater(t, out.Page.Returned, 0)
	assert.Less(t, out.Page.Returned, 30)

	var items []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out.Body), &items))
	require.Len(t, items, out.Page.Returned)
	for _, item := range items {
		assert.ElementsMatch(t, []string{"id", "name"}, mapKeys(item))
		assert.NotContains(t, item, "access_token")
		assert.NotContains(t, item, "blob")
	}
	assert.NotContains(t, out.Body, secret)
	assert.NotContains(t, out.Body, "BLOBDATA")
	text := spanText(t, rec)
	assert.NotContains(t, text, secret)
	assert.NotContains(t, text, "BLOBDATA")

	rec.Stop(context.Background())
	rec2, err := telemetry.Record()
	require.NoError(t, err)
	defer rec2.Stop(context.Background())
	plain := runtime.Runtime{
		Catalog: cat,
		State:   policy.NewState(),
		Exec:    execute.Client{BaseURL: ts.URL, MaxBody: maxBody, RecordBody: true},
	}
	_, err = plain.Invoke(context.Background(), runtime.Request{Operation: "orders.list"})
	assert.ErrorContains(t, err, fmt.Sprintf("exceeds %d bytes", maxBody))
	assert.NotContains(t, spanText(t, rec2), secret)

	small := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[{"id":"1","name":"ada","access_token":"%s"},{"id":"2","name":"grace","access_token":"%s"}]`, secret, secret)
	}))
	defer small.Close()
	configured := runtime.Runtime{
		Catalog: cat,
		State:   policy.NewState(),
		Exec: execute.Client{
			BaseURL: small.URL,
			Fields:  []string{"id"},
			Limit:   1,
		},
	}
	paged, err := configured.Invoke(context.Background(), runtime.Request{Operation: "orders.list"})
	require.NoError(t, err)
	assert.True(t, paged.Truncated)
	require.NotNil(t, paged.Page)
	assert.Equal(t, 1, paged.Page.Limit)
	assert.Equal(t, 1, paged.Page.Returned)
	assert.NotContains(t, paged.Body, secret)
	assert.Contains(t, paged.Body, `"id"`)
	assert.NotContains(t, paged.Body, `"name"`)
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func spanText(t *testing.T, rec *telemetry.Recorder) string {
	t.Helper()
	var b strings.Builder
	for _, sp := range rec.Spans() {
		b.WriteString(sp.Name)
		for k, v := range sp.Attrs {
			b.WriteByte(' ')
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(v)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
