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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIEmptyBaseURLUsesDefaultHost(t *testing.T) {
	m, err := openai.New("", "test-key", "")
	require.NoError(t, err)
	assert.Equal(t, "https://api.openai.com/v1", m.BaseURL)
}

func TestOpenAIRequiresKeyAndPack(t *testing.T) {
	_, err := openai.New("", "", "")
	assert.ErrorContains(t, err, "API key")
	m, err := openai.New("http://127.0.0.1", "test-key", "")
	require.NoError(t, err)
	_, err = m.Complete(context.Background(), agent.Request{UserMessage: "delete asset 123"})
	assert.ErrorContains(t, err, "context pack")
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
	require.NoError(t, err)
	got, err := m.Complete(context.Background(), agent.Request{
		UserMessage: "delete asset 123",
		Context:     "index: assets.delete\nuser: delete asset 123",
	})
	require.NoError(t, err)
	assert.True(t, sawPack)
	assert.True(t, sawAuth)
	assert.Equal(t, "assets.delete", got.OperationID)
	assert.Equal(t, "123", got.Params["id"])
}

func TestOpenAICoercesNumberAndObjectParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"operation_id\":\"assets.create\",\"params\":{\"id\":123,\"body\":{\"name\":\"kit\"}},\"flow_name\":\"\"}"}}]}`)
	}))
	defer srv.Close()
	m, err := openai.New(srv.URL, "test-key", "gpt-test")
	require.NoError(t, err)
	got, err := m.Complete(context.Background(), agent.Request{UserMessage: "create", Context: "index: assets.create"})
	require.NoError(t, err)
	assert.Equal(t, "123", got.Params["id"])
	assert.Equal(t, `{"name":"kit"}`, got.Params["body"])
}
