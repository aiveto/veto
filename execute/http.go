package execute

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

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
		Project    Projection
		View       *View
	}

	Client struct {
		BaseURL     string
		HTTP        *http.Client
		RecordBody  bool
		Auth        map[string]string
		Creds       *auth.Resolver
		FollowPages int
		MaxBody     int64
		Fields      []string
		Limit       int
	}
)

func (c Client) UpstreamBase() string { return c.BaseURL }

func (c Client) InvokeHTTPResult(ctx context.Context, op *catalog.Operation, params map[string]string) (result.HTTPResult, error) {
	project := c.projection(ctx)
	follow := op != nil && c.FollowPages > 1 && len(op.Page) > 0
	var view View
	cfg := Config{
		BaseURL: c.BaseURL, Client: c.HTTP, RecordBody: c.RecordBody, Auth: c.Auth, Creds: c.Creds, MaxBody: c.MaxBody,
		Project: project, View: &view,
	}
	// Page cursors live on the raw body. Project the merged body after the walk.
	if follow && len(project.Fields) > 0 {
		cfg.Project = Projection{}
		cfg.View = nil
	}
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
	if follow {
		merged, err := followPages(ctx, cfg, op, params, body, c.FollowPages)
		if err != nil {
			return result.HTTPResult{}, err
		}
		out.Body = merged
	}
	if follow && len(project.Fields) > 0 {
		shaped, projected, err := shapeBody(resp.StatusCode, []byte(out.Body), false, Config{Project: project, MaxBody: c.MaxBody})
		if err != nil {
			return result.HTTPResult{}, err
		}
		out.Body = string(shaped)
		out.Truncated = projected.Truncated
		out.Page = projected.Page
		return out, nil
	}
	if view.Applied {
		out.Truncated = view.Truncated
		out.Page = view.Page
		return out, nil
	}
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

	req, err := prepareRequest(ctx, base, op, params, true)
	if err != nil {
		return nil, err
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
	raw, cut, err := consumeBody(resp, cfg)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized && refresh {
		refreshed, rerr := retryUnauthorized(ctx, cfg, op, req, secrets, queryKeys)
		if rerr != nil {
			return nil, rerr
		}
		resp = refreshed.resp
		raw = refreshed.raw
		cut = refreshed.cut
	}
	body, view, err := shapeBody(resp.StatusCode, raw, cut, cfg)
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if cfg.View != nil {
		*cfg.View = view
	}
	span.SetAttributes(telemetry.Attr("http.status", strconv.Itoa(resp.StatusCode)))
	if cfg.RecordBody && len(body) > 0 {
		recorded := redactBody(auth.Redact(string(body), secrets, queryKeys))
		span.SetAttributes(telemetry.Attr("http.body", recorded))
	}
	return resp, nil
}

type readResponse struct {
	resp *http.Response
	raw  []byte
	cut  bool
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
	raw, cut, err := consumeBody(resp, cfg)
	if err != nil {
		return readResponse{}, err
	}
	return readResponse{resp: resp, raw: raw, cut: cut}, nil
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
		delay := retryDelay(resp, try)
		if err := resp.Body.Close(); err != nil {
			return nil, fmt.Errorf("close body: %w", err)
		}
		if err := waitRetry(req.Context(), delay); err != nil {
			return nil, err
		}
	}
	return resp, err
}

const (
	retryBase    = 50 * time.Millisecond
	maxRetryWait = 30 * time.Second
)

func retryDelay(resp *http.Response, try int) time.Duration {
	if d, ok := retryAfter(resp); ok {
		return capRetryWait(d)
	}
	if try < 0 {
		try = 0
	}
	if try > 4 {
		try = 4
	}
	return capRetryWait(retryBase << try)
}

func retryAfter(resp *http.Response) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		if secs < 0 {
			return 0, true
		}
		return time.Duration(secs) * time.Second, true
	}
	when, err := http.ParseTime(raw)
	if err != nil {
		return 0, false
	}
	d := time.Until(when)
	if d < 0 {
		return 0, true
	}
	return d, true
}

// A long Retry-After must not stall the call indefinitely.
func capRetryWait(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	if d > maxRetryWait {
		return maxRetryWait
	}
	return d
}

func waitRetry(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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

type idempotencyKey struct{}

// WithIdempotency carries the caller's idempotency key onto the HTTP request.
func WithIdempotency(ctx context.Context, key string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, idempotencyKey{}, key)
}

// IdempotencyFrom returns the key set by WithIdempotency.
func IdempotencyFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	key, _ := ctx.Value(idempotencyKey{}).(string)
	return key
}

// CheckParams reports a missing required parameter before HTTP.
func CheckParams(op *catalog.Operation, params map[string]string) error {
	return requireParams(op, params)
}

// DraftRequest is the upstream request without credentials. strict checks stay on the real call.
func DraftRequest(ctx context.Context, base string, op *catalog.Operation, params map[string]string) (*http.Request, error) {
	if op == nil {
		return nil, fmt.Errorf("missing operation")
	}
	if base == "" {
		base = op.BaseURL
	}
	if base == "" {
		return nil, fmt.Errorf("operation %s has no server URL", op.ID)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return prepareRequest(ctx, base, op, params, false)
}

func prepareRequest(ctx context.Context, base string, op *catalog.Operation, params map[string]string, strict bool) (*http.Request, error) {
	if strict {
		if err := requireParams(op, params); err != nil {
			return nil, err
		}
	}
	path := op.PathTemplate
	for _, p := range op.Params {
		if p.In != "path" {
			continue
		}
		if params[p.Name] == "" {
			continue
		}
		path = strings.Replace(path, "{"+p.Name+"}", url.PathEscape(params[p.Name]), 1)
	}
	if strict && strings.Contains(path, "{") {
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
	if key := IdempotencyFrom(ctx); key != "" {
		req.Header.Set("Idempotency-Key", key)
	} else if strict && op.Idempotency == "key" {
		req.Header.Set("Idempotency-Key", uuid.NewString())
	}
	return req, nil
}

func headerValue(p catalog.Param, params map[string]string) string {
	if v := strings.TrimSpace(params[p.Name]); v != "" {
		return v
	}
	return strings.TrimSpace(p.Default)
}

func requireParams(op *catalog.Operation, params map[string]string) error {
	if op == nil {
		return fmt.Errorf("missing operation")
	}
	for _, p := range op.Params {
		if why := Unserializable(p); why != "" {
			return fmt.Errorf("operation %s: parameter %s cannot be serialized: %s", op.ID, p.Name, why)
		}
		required := p.Required || p.In == "path"
		v := strings.TrimSpace(params[p.Name])
		if v == "" && p.In == "header" {
			v = strings.TrimSpace(p.Default)
		}
		if required && v == "" {
			return result.ParamError{Operation: op.ID, Name: p.Name}
		}
		if p.In == "body" && v != "" && schemaType(p.Schema) == "object" && !jsonObject(v) {
			return fmt.Errorf("operation %s: %s must be a JSON object", op.ID, p.Name)
		}
	}
	return nil
}

func jsonObject(raw string) bool {
	dec := json.NewDecoder(strings.NewReader(raw))
	var value any
	if err := dec.Decode(&value); err != nil {
		return false
	}
	if _, ok := value.(map[string]any); !ok {
		return false
	}
	var extra any
	err := dec.Decode(&extra)
	return err == io.EOF
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

func bodyLimit(max int64) int64 {
	if max <= 0 {
		return DefaultMaxResponseBytes
	}
	return max
}

func readCapped(r io.Reader, max int64) ([]byte, bool, error) {
	if r == nil {
		return nil, false, nil
	}
	max = bodyLimit(max)
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(b)) > max {
		return b[:max], true, nil
	}
	return b, false, nil
}

func readLimited(r io.Reader, max int64) ([]byte, error) {
	b, cut, err := readCapped(r, max)
	if err != nil {
		return nil, err
	}
	if cut {
		return nil, fmt.Errorf("response exceeds %d bytes", bodyLimit(max))
	}
	return b, nil
}
