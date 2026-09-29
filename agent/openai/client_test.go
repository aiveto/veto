package openai_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/agent/openai"
)

func TestOpenAIEmptyBaseURLUsesDefaultHost(t *testing.T) {
	m, err := openai.New("", "test-key", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("base: %s", m.BaseURL)
	}
}

func TestOpenAIRequiresKeyAndPack(t *testing.T) {
	if _, err := openai.New("", "", ""); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("key: %v", err)
	}
	m, err := openai.New("http://127.0.0.1", "test-key", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Complete(context.Background(), agent.Request{UserMessage: "delete asset 123"}); err == nil || !strings.Contains(err.Error(), "context pack") {
		t.Fatalf("pack: %v", err)
	}
}

func TestOpenAIReadsThePack(t *testing.T) {
	var sawPack, sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sawPack = strings.Contains(string(body), "assets.delete") && strings.Contains(string(body), "delete asset 123")
		sawAuth = r.Header.Get("Authorization") == "Bearer test-key"
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"operation_id\":\"assets.delete\",\"params\":{\"id\":\"123\"},\"flow_name\":\"\"}"}}]}`)
	}))
	defer srv.Close()

	m, err := openai.New(srv.URL, "test-key", "gpt-test")
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.Complete(context.Background(), agent.Request{
		UserMessage: "delete asset 123",
		Context:     "index: assets.delete\nuser: delete asset 123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawPack || !sawAuth {
		t.Fatalf("request pack=%v auth=%v", sawPack, sawAuth)
	}
	if got.OperationID != "assets.delete" || got.Params["id"] != "123" {
		t.Fatalf("response: %+v", got)
	}
}
