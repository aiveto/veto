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
	op := cat.ByID("assets.get")
	const secret = "token-in-body"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(secret))
	}))
	defer ts.Close()

	rec, err := telemetry.Record()
	require.NoError(t, err)
	defer rec.Stop(context.Background())

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
	defer rec2.Stop(context.Background())
	_, err = (execute.Client{BaseURL: ts.URL, RecordBody: true}).InvokeHTTPResult(context.Background(), op, map[string]string{"id": "1"})
	require.NoError(t, err)
	assert.Contains(t, spanText(t, rec2), "http.body="+secret)
}

func TestClientTimeoutEndsAHungCall(t *testing.T) {
	cat := loadSpec(t, bodySpec)
	op := cat.ByID("assets.get")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer ts.Close()
	_, err := execute.InvokeResponse(context.Background(), execute.Config{
		BaseURL: ts.URL,
		Client:  &http.Client{Timeout: 30 * time.Millisecond},
	}, op, map[string]string{"id": "1"})
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
