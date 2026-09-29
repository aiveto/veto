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
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Stop(context.Background())

	got, err := execute.Client{BaseURL: ts.URL}.Invoke(context.Background(), op, map[string]string{"id": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != "rate_limited" || !got.Retryable || got.Status != http.StatusTooManyRequests || got.Body != secret {
		t.Fatalf("result: %+v", got)
	}
	text := spanText(t, rec)
	if !strings.Contains(text, "http.status=429") || strings.Contains(text, secret) || strings.Contains(text, "http.body=") {
		t.Fatalf("span:\n%s", text)
	}

	rec2, err := telemetry.Record()
	if err != nil {
		t.Fatal(err)
	}
	defer rec2.Stop(context.Background())
	if _, err := (execute.Client{BaseURL: ts.URL, RecordBody: true}).Invoke(context.Background(), op, map[string]string{"id": "1"}); err != nil {
		t.Fatal(err)
	}
	kept := spanText(t, rec2)
	if !strings.Contains(kept, "http.body="+secret) {
		t.Fatalf("kept body missing:\n%s", kept)
	}
}

func TestClientTimeoutEndsAHungCall(t *testing.T) {
	cat := loadSpec(t, bodySpec)
	op := cat.ByID("assets.get")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer ts.Close()
	_, err := execute.Invoke(context.Background(), execute.Config{
		BaseURL: ts.URL,
		Client:  &http.Client{Timeout: 30 * time.Millisecond},
	}, op, map[string]string{"id": "1"})
	if err == nil {
		t.Fatal("expected the hung call to end")
	}
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
