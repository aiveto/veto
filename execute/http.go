package execute

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/telemetry"
)

type (
	// Config points invoke at a base URL and optional client.
	Config struct {
		BaseURL string
		Client  *http.Client
	}

	// Client is the HTTP writer the agent loop calls after policy allows an operation.
	Client struct {
		BaseURL string
		HTTP    *http.Client
	}
)

// Invoke performs the operation and returns the status and body.
func (c Client) Invoke(ctx context.Context, op *catalog.Operation, params map[string]string) (agent.HTTPResult, error) {
	resp, err := Invoke(ctx, Config{BaseURL: c.BaseURL, Client: c.HTTP}, op, params)
	if err != nil {
		return agent.HTTPResult{}, err
	}
	body, err := ReadBody(resp)
	if err != nil {
		return agent.HTTPResult{}, err
	}
	return agent.HTTPResult{Status: resp.StatusCode, Body: body}, nil
}

// Invoke performs the HTTP call described by op.
func Invoke(ctx context.Context, cfg Config, op *catalog.Operation, params map[string]string) (*http.Response, error) {
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	span := telemetry.StartSpan(ctx, "execute.invoke")
	defer span.End()
	span.SetAttributes(telemetry.Attr("operation.id", op.ID), telemetry.Attr("http.method", op.Method))

	base := cfg.BaseURL
	if base == "" {
		base = op.BaseURL
	}
	if base == "" {
		return nil, fmt.Errorf("operation %s has no server URL", op.ID)
	}

	if err := requireParams(op, params); err != nil {
		return nil, err
	}

	path := op.PathTemplate
	for _, p := range op.Params {
		if p.In != "path" {
			continue
		}
		path = strings.Replace(path, "{"+p.Name+"}", url.PathEscape(params[p.Name]), 1)
	}
	if strings.Contains(path, "{") {
		return nil, fmt.Errorf("operation %s: empty path parameter", op.ID)
	}
	endpoint := strings.TrimRight(base, "/") + path

	var body io.Reader
	if p, ok := op.BodyParam(); ok {
		raw := params[p.Name]
		if raw != "" {
			body = bytes.NewReader([]byte(raw))
		}
	}
	req, err := http.NewRequestWithContext(ctx, op.Method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	q := req.URL.Query()
	for _, p := range op.Params {
		if p.In != "query" {
			continue
		}
		if v := params[p.Name]; v != "" {
			q.Set(p.Name, v)
		}
	}
	req.URL.RawQuery = q.Encode()
	for _, p := range op.Params {
		if p.In != "header" {
			continue
		}
		if v := params[p.Name]; v != "" {
			req.Header.Set(p.Name, v)
		}
	}
	if formatted := formatParams(spanParams(op, params)); formatted != "" {
		span.SetAttributes(telemetry.Attr("params", formatted))
	}

	return cfg.Client.Do(req)
}

func requireParams(op *catalog.Operation, params map[string]string) error {
	for _, p := range op.Params {
		required := p.Required || p.In == "path"
		if !required {
			continue
		}
		if strings.TrimSpace(params[p.Name]) == "" {
			return fmt.Errorf("operation %s: %s required", op.ID, p.Name)
		}
	}
	return nil
}

func spanParams(op *catalog.Operation, params map[string]string) map[string]string {
	if len(params) == 0 {
		return nil
	}
	out := make(map[string]string, len(params))
	for k, v := range params {
		out[k] = v
	}
	if p, ok := op.BodyParam(); ok {
		delete(out, p.Name)
	}
	delete(out, "_body")
	return out
}

func formatParams(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	return strings.Join(parts, ",")
}

// ReadBody drains and closes the response body.
func ReadBody(resp *http.Response) (string, error) {
	if resp == nil || resp.Body == nil {
		return "", nil
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	return string(b), nil
}
