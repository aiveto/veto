package execute

import (
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

// Config points invoke at a base URL and optional client.
type Config struct {
	BaseURL string
	Client  *http.Client
}

// Client is the HTTP writer the agent loop calls after policy allows an operation.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

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
	if formatted := formatParams(params); formatted != "" {
		span.SetAttributes(telemetry.Attr("params", formatted))
	}

	base := cfg.BaseURL
	if base == "" {
		base = op.BaseURL
	}
	if base == "" {
		return nil, fmt.Errorf("operation %s has no server URL", op.ID)
	}

	path := op.PathTemplate
	for _, p := range op.Params {
		if p.In != "path" {
			continue
		}
		val := params[p.Name]
		path = strings.Replace(path, "{"+p.Name+"}", url.PathEscape(val), 1)
	}
	endpoint := strings.TrimRight(base, "/") + path

	req, err := http.NewRequestWithContext(ctx, op.Method, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
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

	return cfg.Client.Do(req)
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
