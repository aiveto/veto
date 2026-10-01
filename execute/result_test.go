package execute_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPResultCarriesCodeAndReplayOmitsBody(t *testing.T) {
	cat := loadSpec(t, bodySpec)
	op := cat.ByID("orders.get")
	const secret = "token-in-body"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(secret))
	}))
	defer ts.Close()

	rec, err := telemetry.Record()
	require.NoError(t, err)
	defer func() { require.NoError(t, rec.Stop(context.Background())) }()

	got, err := execute.Client{BaseURL: ts.URL}.InvokeHTTPResult(context.Background(), op, map[string]string{"id": "1"})
	require.NoError(t, err)
	assert.Equal(t, "rate_limited", got.Code)
	assert.True(t, got.Retryable)
	assert.Equal(t, http.StatusTooManyRequests, got.Status)
	assert.Equal(t, secret, got.Body)
	text := spanText(t, rec)
	assert.Contains(t, text, "http.status=429")
	assert.NotContains(t, text, secret)
	assert.NotContains(t, text, "http.body=")

	rec2, err := telemetry.Record()
	require.NoError(t, err)
	defer func() { require.NoError(t, rec2.Stop(context.Background())) }()
	_, err = (execute.Client{BaseURL: ts.URL, RecordBody: true}).InvokeHTTPResult(context.Background(), op, map[string]string{"id": "1"})
	require.NoError(t, err)
	assert.Contains(t, spanText(t, rec2), "http.body="+secret)
}

func TestResponseCapOmitsParamValuesAndSetsToolAttributes(t *testing.T) {
	assert.Equal(t, int64(1<<20), execute.DefaultMaxResponseBytes)
	cat := loadSpec(t, bodySpec)
	op := cat.ByID("orders.get")
	const secret = "param-value-secret"
	cases := []struct {
		name    string
		max     int64
		wantErr string
	}{
		{name: "over the cap", max: 4, wantErr: "exceeds 4 bytes"},
		{name: "under the cap", max: 8},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("12345"))
	}))
	defer ts.Close()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, err := telemetry.Record()
			require.NoError(t, err)
			defer func() { require.NoError(t, rec.Stop(context.Background())) }()
			got, err := execute.Client{BaseURL: ts.URL, MaxBody: tc.max}.InvokeHTTPResult(context.Background(), op, map[string]string{"id": secret})
			text := spanText(t, rec)
			assert.Contains(t, text, "gen_ai.tool.name="+op.ID)
			assert.Contains(t, text, "gen_ai.operation.name="+op.ID)
			assert.Contains(t, text, "params=id")
			assert.NotContains(t, text, secret)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "12345", got.Body)
		})
	}
}

func TestClientTimeoutEndsAHungCall(t *testing.T) {
	cat := loadSpec(t, bodySpec)
	op := cat.ByID("orders.get")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer ts.Close()
	resp, err := execute.InvokeResponse(context.Background(), execute.Config{
		BaseURL: ts.URL,
		Client:  &http.Client{Timeout: 30 * time.Millisecond},
	}, op, map[string]string{"id": "1"})
	if resp != nil && resp.Body != nil {
		assert.NoError(t, resp.Body.Close())
	}
	assert.Error(t, err)
}

func spanText(t *testing.T, rec *telemetry.Recorder) string {
	t.Helper()
	var b strings.Builder
	for _, sp := range rec.Spans() {
		b.WriteString(sp.Name)
		for k, v := range sp.Attrs {
			b.WriteString(" ")
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(v)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
