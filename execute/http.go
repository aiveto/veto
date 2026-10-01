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

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/result"
	"github.com/aiveto/veto/telemetry"
	"github.com/google/uuid"
)

const DefaultMaxResponseBytes int64 = 1 << 20

type (
	Config struct {
		BaseURL    string
		Client     *http.Client
		RecordBody bool
		Auth       map[string]string // secret by scheme name; never read from yaml
		Creds      *auth.Resolver
		MaxBody    int64
	}

	Client struct {
		BaseURL     string
		HTTP        *http.Client
		RecordBody  bool
		Auth        map[string]string
		Creds       *auth.Resolver
		FollowPages int
		MaxBody     int64
	}
)

func (c Client) InvokeHTTPResult(ctx context.Context, op *catalog.Operation, params map[string]string) (result.HTTPResult, error) {
	cfg := Config{BaseURL: c.BaseURL, Client: c.HTTP, RecordBody: c.RecordBody, Auth: c.Auth, Creds: c.Creds, MaxBody: c.MaxBody}
	resp, err := InvokeResponse(ctx, cfg, op, params)
	if err != nil {
		return result.HTTPResult{}, err
	}
	body, err := readBody(resp, cfg.MaxBody)
	if err != nil {
		return result.HTTPResult{}, err
	}
	code, retryable := classify(resp.StatusCode)
	out := result.HTTPResult{Status: resp.StatusCode, Body: body, Code: code, Retryable: retryable}
	if c.FollowPages <= 1 || len(op.Page) == 0 {
		return out, nil
	}
	merged, err := followPages(ctx, cfg, op, params, body, c.FollowPages)
	if err != nil {
		return result.HTTPResult{}, err
	}
	out.Body = merged
	return out, nil
}

func InvokeResponse(ctx context.Context, cfg Config, op *catalog.Operation, params map[string]string) (*http.Response, error) {
	cfg.Client = auth.WithEnvProxy(cfg.Client)
	span := telemetry.StartSpan(ctx, "execute.invoke")
	defer span.End()
	span.SetAttributes(
		telemetry.Attr("operation.id", op.ID),
		telemetry.Attr("http.method", op.Method),
		telemetry.Attr(telemetry.ToolNameAttr, op.ID),
		telemetry.Attr(telemetry.OperationIDAttr, op.ID),
	)

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

	var bodyBytes []byte
	media := ""
	if p, ok := op.BodyParam(); ok {
		raw := params[p.Name]
		if raw != "" {
			bodyBytes = []byte(raw)
			media = p.MediaType
			if media == "" {
				media = "application/json"
			}
		}
	}
	var body io.Reader
	if len(bodyBytes) > 0 {
		body = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(ctx, op.Method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if len(bodyBytes) > 0 {
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}
	q := req.URL.Query()
	for _, p := range op.Params {
		if p.In != "query" {
			continue
		}
		if v := params[p.Name]; v != "" {
			writeQuery(q, p, v)
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
	if op.Idempotency == "key" {
		req.Header.Set("Idempotency-Key", uuid.NewString())
	}
	refresh, creds, err := obtainAuth(ctx, cfg, op, req, false)
	if err != nil {
		return nil, err
	}
	secrets, queryKeys, err := applyCredentials(req, creds)
	if err != nil {
		return nil, err
	}
	if names := paramNames(params); names != "" {
		span.SetAttributes(telemetry.Attr("params", names))
	}

	resp, err := doRetry(cfg.Client, req, op)
	if err != nil {
		return nil, scrubTransport(err, secrets, queryKeys)
	}
	raw, err := readLimited(resp.Body, cfg.MaxBody)
	if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized && refresh {
		refreshed, rerr := retryUnauthorized(ctx, cfg, op, req, secrets, queryKeys)
		if rerr != nil {
			return nil, rerr
		}
		resp = refreshed.resp
		raw = refreshed.raw
	}
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	span.SetAttributes(telemetry.Attr("http.status", strconv.Itoa(resp.StatusCode)))
	if cfg.RecordBody && len(raw) > 0 {
		span.SetAttributes(telemetry.Attr("http.body", auth.Redact(string(raw), secrets, queryKeys)))
	}
	return resp, nil
}

type readResponse struct {
	resp *http.Response
	raw  []byte
}

func retryUnauthorized(ctx context.Context, cfg Config, op *catalog.Operation, req *http.Request, secrets, queryKeys []string) (readResponse, error) {
	_, creds, err := obtainAuth(ctx, cfg, op, req, true)
	if err != nil {
		return readResponse{}, err
	}
	moreSecrets, moreKeys, err := applyCredentials(req, creds)
	if err != nil {
		return readResponse{}, err
	}
	secrets = append(secrets, moreSecrets...)
	queryKeys = append(queryKeys, moreKeys...)
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return readResponse{}, fmt.Errorf("retry body: %w", err)
		}
		req.Body = body
	}
	resp, err := cfg.Client.Do(req)
	if err != nil {
		return readResponse{}, scrubTransport(err, secrets, queryKeys)
	}
	raw, err := readLimited(resp.Body, cfg.MaxBody)
	if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return readResponse{}, fmt.Errorf("read body: %w", err)
	}
	return readResponse{resp: resp, raw: raw}, nil
}

func scrubTransport(err error, secrets, queryKeys []string) error {
	if err == nil {
		return nil
	}
	msg := auth.Redact(err.Error(), secrets, queryKeys)
	if msg == err.Error() {
		return err
	}
	return fmt.Errorf("%s", msg)
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
			return result.ParamError{Operation: op.ID, Name: p.Name}
		}
	}
	return nil
}

func paramNames(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func ReadBody(resp *http.Response) (string, error) {
	return readBody(resp, 0)
}

func readBody(resp *http.Response, max int64) (string, error) {
	if resp == nil || resp.Body == nil {
		return "", nil
	}
	b, err := readLimited(resp.Body, max)
	if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	return string(b), nil
}

func readLimited(r io.Reader, max int64) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	if max <= 0 {
		max = DefaultMaxResponseBytes
	}
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("response exceeds %d bytes", max)
	}
	return b, nil
}
