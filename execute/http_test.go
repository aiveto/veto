package execute

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aiveto/veto/catalog"
)

func TestInvokePostWithBody(t *testing.T) {
	var receivedBody string
	var receivedContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	op := &catalog.Operation{
		ID:           "assets.create",
		Method:       "POST",
		PathTemplate: "/assets",
		RequestBody:  "Holding",
		BaseURL:      server.URL,
	}

	params := map[string]string{
		"_body": `{"id":"123","teamsId":"team1"}`,
	}

	resp, err := Invoke(context.Background(), Config{BaseURL: server.URL}, op, params)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected status 201, got %d", resp.StatusCode)
	}
	if receivedContentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", receivedContentType)
	}
	if receivedBody != `{"id":"123","teamsId":"team1"}` {
		t.Errorf("expected body %q, got %q", `{"id":"123","teamsId":"team1"}`, receivedBody)
	}
}

func TestInvokeWithoutBody(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	op := &catalog.Operation{
		ID:           "assets.get",
		Method:       "GET",
		PathTemplate: "/assets/123",
		RequestBody:  "",
		BaseURL:      server.URL,
	}

	params := map[string]string{}

	resp, err := Invoke(context.Background(), Config{BaseURL: server.URL}, op, params)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if receivedBody != "" {
		t.Errorf("expected no body, got %q", receivedBody)
	}
}

func TestInvokeWithBodyButNoBodyParam(t *testing.T) {
	var receivedContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	op := &catalog.Operation{
		ID:           "assets.create",
		Method:       "POST",
		PathTemplate: "/assets",
		RequestBody:  "Holding",
		BaseURL:      server.URL,
	}

	params := map[string]string{}

	resp, err := Invoke(context.Background(), Config{BaseURL: server.URL}, op, params)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected status 201, got %d", resp.StatusCode)
	}
	if receivedContentType != "" {
		t.Errorf("expected no Content-Type when _body is empty, got %s", receivedContentType)
	}
}

func TestInvokePutWithBody(t *testing.T) {
	var receivedBody string
	var receivedContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	op := &catalog.Operation{
		ID:           "assets.update",
		Method:       "PUT",
		PathTemplate: "/assets/123",
		RequestBody:  "Holding",
		BaseURL:      server.URL,
	}

	params := map[string]string{
		"id":     "123",
		"_body":  `{"id":"123","teamsId":"team2"}`,
	}

	resp, err := Invoke(context.Background(), Config{BaseURL: server.URL}, op, params)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if receivedContentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", receivedContentType)
	}
	if receivedBody != `{"id":"123","teamsId":"team2"}` {
		t.Errorf("expected body %q, got %q", `{"id":"123","teamsId":"team2"}`, receivedBody)
	}
}
