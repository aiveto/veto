package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aiveto/veto/telemetry"
)

// OpenAI calls an OpenAI-compatible chat completions endpoint.
// The context pack is the system message. The reply is JSON for one operation.
type OpenAI struct {
	BaseURL string
	APIKey  string
	Name    string
	Client  *http.Client
}

// NewOpenAI builds a live model. An empty key is an error. An empty base URL uses the OpenAI API.
func NewOpenAI(baseURL, apiKey, name string) (*OpenAI, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("openai model requires an API key")
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if name == "" {
		name = "gpt-4o-mini"
	}
	return &OpenAI{BaseURL: baseURL, APIKey: apiKey, Name: name, Client: http.DefaultClient}, nil
}

func (m *OpenAI) Complete(ctx context.Context, req Request) (Response, error) {
	span := telemetry.StartSpan(ctx, "model.request")
	defer span.End()
	if strings.TrimSpace(req.UserMessage) != "" {
		span.SetAttributes(telemetry.Attr("user_message", req.UserMessage))
	}
	if strings.TrimSpace(req.Context) == "" {
		return Response{}, fmt.Errorf("openai model requires a context pack")
	}
	if m.Client == nil {
		m.Client = http.DefaultClient
	}
	body, err := json.Marshal(chatRequest{
		Model: m.Name,
		Messages: []chatMessage{
			{Role: "system", Content: req.Context + "\nReply with JSON only: {\"operation_id\":\"\",\"params\":{},\"flow_name\":\"\"}"},
			{Role: "user", Content: req.UserMessage},
		},
	})
	if err != nil {
		return Response{}, fmt.Errorf("encode model request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(m.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("build model request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+m.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := m.Client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("model request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("read model response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, fmt.Errorf("model status %d", resp.StatusCode)
	}
	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Response{}, fmt.Errorf("parse model response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Response{}, fmt.Errorf("model returned no choices")
	}
	out, err := parseModelJSON(parsed.Choices[0].Message.Content)
	if err != nil {
		return Response{}, err
	}
	span.SetAttributes(telemetry.Attr("operation.id", out.OperationID))
	return out, nil
}

func parseModelJSON(content string) (Response, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	var wire struct {
		OperationID string            `json:"operation_id"`
		Params      map[string]string `json:"params"`
		FlowName    string            `json:"flow_name"`
	}
	if err := json.Unmarshal([]byte(content), &wire); err != nil {
		return Response{}, fmt.Errorf("parse model json: %w", err)
	}
	return Response{OperationID: wire.OperationID, Params: wire.Params, FlowName: wire.FlowName}, nil
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}
