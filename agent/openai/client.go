package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/telemetry"
)

type (
	// Client calls an OpenAI-compatible chat completions endpoint.
	// The pack is the system message. The reply is JSON for one operation.
	Client struct {
		BaseURL string
		APIKey  string
		Name    string
		HTTP    *http.Client
	}

	chatRequest struct {
		Model    string        `json:"model"`
		Messages []chatMessage `json:"messages"`
	}

	chatMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	chatResponse struct {
		Choices []struct {
			Message chatMessage `json:"message"`
		} `json:"choices"`
	}
)

// New builds a client. An empty key is an error. An empty base URL uses the OpenAI API.
func New(baseURL, apiKey, name string) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("openai model requires an API key")
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if name == "" {
		name = "gpt-4o-mini"
	}
	return &Client{BaseURL: baseURL, APIKey: apiKey, Name: name, HTTP: http.DefaultClient}, nil
}

func (c *Client) Complete(ctx context.Context, req agent.Request) (agent.Response, error) {
	span := telemetry.StartSpan(ctx, "model.request")
	defer span.End()
	if strings.TrimSpace(req.UserMessage) != "" {
		span.SetAttributes(telemetry.Attr("user_message", req.UserMessage))
	}
	if strings.TrimSpace(req.Context) == "" {
		return agent.Response{}, fmt.Errorf("openai model requires a context pack")
	}
	if c.HTTP == nil {
		c.HTTP = http.DefaultClient
	}
	body, err := json.Marshal(chatRequest{
		Model: c.Name,
		Messages: []chatMessage{
			{Role: "system", Content: req.Context + "\nReply with JSON only: {\"operation_id\":\"\",\"params\":{},\"flow_name\":\"\"}"},
			{Role: "user", Content: req.UserMessage},
		},
	})
	if err != nil {
		return agent.Response{}, fmt.Errorf("encode model request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return agent.Response{}, fmt.Errorf("build model request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return agent.Response{}, fmt.Errorf("model request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return agent.Response{}, fmt.Errorf("read model response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return agent.Response{}, fmt.Errorf("model status %d", resp.StatusCode)
	}
	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return agent.Response{}, fmt.Errorf("parse model response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return agent.Response{}, fmt.Errorf("model returned no choices")
	}
	out, err := parseReply(parsed.Choices[0].Message.Content)
	if err != nil {
		return agent.Response{}, err
	}
	span.SetAttributes(telemetry.Attr("operation.id", out.OperationID))
	return out, nil
}

func parseReply(content string) (agent.Response, error) {
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
		return agent.Response{}, fmt.Errorf("parse model json: %w", err)
	}
	return agent.Response{OperationID: wire.OperationID, Params: wire.Params, FlowName: wire.FlowName}, nil
}
