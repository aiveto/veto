package model_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aiveto/veto/model"
)

func TestOpenAIRequiresKeyAndPack(t *testing.T) {
	if _, err := model.NewOpenAI("", "", ""); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("key: %v", err)
	}
	m, err := model.NewOpenAI("http://127.0.0.1", "test-key", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Complete(context.Background(), model.Request{UserMessage: "delete asset 123"}); err == nil || !strings.Contains(err.Error(), "context pack") {
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

	m, err := model.NewOpenAI(srv.URL, "test-key", "gpt-test")
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.Complete(context.Background(), model.Request{
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
