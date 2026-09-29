package execute

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/telemetry"
	"github.com/google/uuid"
)

type (
	Config struct {
		BaseURL    string
		Client     *http.Client
		RecordBody bool
		Auth       map[string]string // secret by scheme name; never read from yaml
	}

	Client struct {
		BaseURL     string
		HTTP        *http.Client
		RecordBody  bool
		Auth        map[string]string
		FollowPages int
	}
)

func (c Client) InvokeHTTPResult(ctx context.Context, op *catalog.Operation, params map[string]string) (agent.HTTPResult, error) {
	resp, err := InvokeResponse(ctx, Config{BaseURL: c.BaseURL, Client: c.HTTP, RecordBody: c.RecordBody, Auth: c.Auth}, op, params)
	if err != nil {
		return agent.HTTPResult{}, err
	}
	body, err := ReadBody(resp)
	if err != nil {
		return agent.HTTPResult{}, err
	}
	code, retryable := classify(resp.StatusCode)
	result := agent.HTTPResult{Status: resp.StatusCode, Body: body, Code: code, Retryable: retryable}
	if c.FollowPages <= 1 || len(op.Page) == 0 {
		return result, nil
	}
	merged, err := followPages(ctx, Config{BaseURL: c.BaseURL, Client: c.HTTP, RecordBody: c.RecordBody, Auth: c.Auth}, op, params, body, c.FollowPages)
	if err != nil {
		return agent.HTTPResult{}, err
	}
	result.Body = merged
	return result, nil
}

func InvokeResponse(ctx context.Context, cfg Config, op *catalog.Operation, params map[string]string) (*http.Response, error) {
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
	media := ""
	if p, ok := op.BodyParam(); ok {
		raw := params[p.Name]
		if raw != "" {
			body = bytes.NewReader([]byte(raw))
			media = p.MediaType
			if media == "" {
				media = "application/json"
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, op.Method, endpoint, body)
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
		if v := headerValue(p, params); v != "" {
			req.Header.Set(p.Name, v)
		}
	}
	if media != "" {
		req.Header.Set("Content-Type", media)
	}
	for _, a := range op.Auth {
		if a.Kind != "bearer" {
			continue
		}
		val := cfg.Auth[a.Name]
		if val == "" {
			return nil, fmt.Errorf("operation %s: %s is unset", op.ID, a.Name)
		}
		req.Header.Set(a.Header, "Bearer "+val)
	}
	if op.Idempotency == "key" {
		req.Header.Set("Idempotency-Key", uuid.NewString())
	}
	if formatted := formatParams(spanParams(op, params)); formatted != "" {
		span.SetAttributes(telemetry.Attr("params", formatted))
	}

	resp, err := doRetry(cfg.Client, req, op)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(resp.Body)
	if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	span.SetAttributes(telemetry.Attr("http.status", strconv.Itoa(resp.StatusCode)))
	if cfg.RecordBody && len(raw) > 0 {
		span.SetAttributes(telemetry.Attr("http.body", string(raw)))
	}
	return resp, nil
}

func doRetry(client *http.Client, req *http.Request, op *catalog.Operation) (*http.Response, error) {
	attempts := 1
	if callIsIdempotent(op, req) {
		attempts += retryCount(op)
	}
	var resp *http.Response
	var err error
	for try := 0; try < attempts; try++ {
		if try > 0 && req.GetBody != nil {
			body, bodyErr := req.GetBody()
			if bodyErr != nil {
				return nil, fmt.Errorf("retry body: %w", bodyErr)
			}
			req.Body = body
		}
		resp, err = client.Do(req)
		if err != nil {
			return nil, err
		}
		if try+1 == attempts || !retryStatus(resp.StatusCode) {
			return resp, nil
		}
		if err := resp.Body.Close(); err != nil {
			return nil, fmt.Errorf("close body: %w", err)
		}
	}
	return resp, err
}

func callIsIdempotent(op *catalog.Operation, req *http.Request) bool {
	if op.Idempotency == "key" && req.Header.Get("Idempotency-Key") != "" {
		return true
	}
	switch op.Method {
	case http.MethodGet, http.MethodHead, http.MethodPut:
		return true
	default:
		return false
	}
}

func retryCount(op *catalog.Operation) int {
	switch strings.TrimSpace(op.Retry) {
	case "", "never":
		return 0
	default:
		n, err := strconv.Atoi(op.Retry)
		if err != nil || n <= 0 {
			return 0
		}
		if n > 3 {
			return 3
		}
		return n
	}
}

func retryStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

func classify(status int) (string, bool) {
	switch {
	case status >= 200 && status < 400:
		return "ok", false
	case status == http.StatusBadRequest:
		return "invalid", false
	case status == http.StatusUnauthorized:
		return "unauthorized", false
	case status == http.StatusForbidden:
		return "forbidden", false
	case status == http.StatusNotFound:
		return "not_found", false
	case status == http.StatusConflict:
		return "conflict", false
	case status == http.StatusTooManyRequests:
		return "rate_limited", true
	case status >= 500:
		return "upstream", true
	default:
		return "rejected", false
	}
}

func headerValue(p catalog.Param, params map[string]string) string {
	if v := strings.TrimSpace(params[p.Name]); v != "" {
		return v
	}
	return strings.TrimSpace(p.Default)
}

func requireParams(op *catalog.Operation, params map[string]string) error {
	for _, p := range op.Params {
		required := p.Required || p.In == "path"
		if !required {
			continue
		}
		v := strings.TrimSpace(params[p.Name])
		if v == "" && p.In == "header" {
			v = strings.TrimSpace(p.Default)
		}
		if v == "" {
			return agent.ParamError{Operation: op.ID, Name: p.Name}
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

func ReadBody(resp *http.Response) (string, error) {
	if resp == nil || resp.Body == nil {
		return "", nil
	}
	b, err := io.ReadAll(resp.Body)
	if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	return string(b), nil
}
