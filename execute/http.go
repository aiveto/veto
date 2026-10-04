// Package execute sends the HTTP call for one operation.
package execute

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/result"
	"github.com/aiveto/veto/runtime"
	"github.com/aiveto/veto/telemetry"
	"github.com/google/uuid"
)

// DefaultMaxResponseBytes is 1 MiB when MaxBody is unset.
const DefaultMaxResponseBytes int64 = 1 << 20

// Client sends one operation over HTTP.
type Client struct {
	BaseURL         string
	HTTP            *http.Client
	RecordBody      bool
	Auth            map[string]string
	Creds           *auth.Resolver
	FollowPages     int
	FollowRedirects bool
	MaxBody         int64
	Fields          []string
	Limit           int
	Project         runtime.Projection
}

func (c Client) InvokeHTTPResult(ctx context.Context, op *catalog.Operation, params map[string]string) (result.HTTPResult, error) {
	project := c.projection(ctx)
	follow := op != nil && c.FollowPages > 1 && len(op.Page) > 0
	call := c
	call.Project = project
	// Page cursors live on the raw body. Project the merged body after the walk.
	if follow && len(project.Fields) > 0 {
		call.Project = runtime.Projection{}
	}
	resp, view, body, sent, err := invokeResponse(ctx, call, op, params)
	if err != nil {
		out := received(resp)
		out.Sent = sent || out.HTTP
		return out, err
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	code, retryable := classify(resp.StatusCode)
	out := result.HTTPResult{Status: resp.StatusCode, Body: string(body), Code: code, Retryable: retryable, HTTP: true, Sent: true}
	if follow {
		merged, cut, err := followPages(ctx, call, op, params, string(body), c.FollowPages)
		if err != nil {
			return out, err
		}
		out.Body = merged
		if cut {
			out.Truncated = true
		}
	}
	if follow && len(project.Fields) > 0 {
		shaped, projected, err := shapeBody(resp.StatusCode, []byte(out.Body), false, Client{Project: project, MaxBody: c.MaxBody})
		if err != nil {
			return out, err
		}
		out.Body = string(shaped)
		// The walk may already have stopped early. Field selection must not clear that.
		if projected.Truncated {
			out.Truncated = true
		}
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

func received(resp *http.Response) result.HTTPResult {
	if resp == nil {
		return result.HTTPResult{}
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	return result.HTTPResult{Status: resp.StatusCode, HTTP: true}
}

// InvokeResponse runs one call and returns the HTTP response.
func InvokeResponse(ctx context.Context, cfg Client, op *catalog.Operation, params map[string]string) (*http.Response, error) {
	resp, view, body, sent, err := invokeResponse(ctx, cfg, op, params)
	_ = view
	_ = body
	_ = sent
	return resp, err
}

func invokeResponse(ctx context.Context, cfg Client, op *catalog.Operation, params map[string]string) (*http.Response, View, []byte, bool, error) {
	if cfg.HTTP == nil {
		cfg.HTTP = http.DefaultClient
	}
	ctx, span := telemetry.StartSpan(ctx, "execute.invoke")
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
		return nil, View{}, nil, false, fmt.Errorf("operation %s has no server URL", op.ID)
	}

	req, err := prepareRequest(ctx, base, op, params, true)
	if err != nil {
		return nil, View{}, nil, false, err
	}
	refresh, creds, err := obtainAuth(ctx, cfg, op, req, false)
	if err != nil {
		return nil, View{}, nil, false, err
	}
	secrets, queryKeys, err := applyCredentials(req, creds)
	if err != nil {
		return nil, View{}, nil, false, err
	}
	cfg.HTTP = boundRedirects(cfg.HTTP, len(creds) > 0, cfg.FollowRedirects)
	if names := paramNames(params); names != "" {
		span.SetAttributes(telemetry.Attr("params", names))
	}

	resp, err := doRetry(cfg.HTTP, req, op)
	if err != nil {
		return resp, View{}, nil, true, scrubTransport(err, secrets, queryKeys)
	}
	raw, cut, err := consumeBody(resp, cfg)
	if err != nil {
		return resp, View{}, nil, true, err
	}
	if resp.StatusCode == http.StatusUnauthorized && refresh {
		refreshed, rerr := retryUnauthorized(ctx, cfg, op, req, secrets, queryKeys)
		if rerr != nil {
			if refreshed.resp != nil {
				return refreshed.resp, View{}, nil, true, rerr
			}
			return resp, View{}, nil, true, rerr
		}
		resp = refreshed.resp
		raw = refreshed.raw
		cut = refreshed.cut
	}
	body, view, err := shapeBody(resp.StatusCode, raw, cut, cfg)
	if err != nil {
		return resp, View{}, nil, true, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	span.SetAttributes(telemetry.Attr("http.status", strconv.Itoa(resp.StatusCode)))
	if cfg.RecordBody && len(body) > 0 {
		recorded := redactBody(auth.Redact(string(body), secrets, queryKeys))
		span.SetAttributes(telemetry.Attr("http.body", recorded))
	}
	return resp, view, body, true, nil
}

type readResponse struct {
	resp *http.Response
	raw  []byte
	cut  bool
}

func retryUnauthorized(ctx context.Context, cfg Client, op *catalog.Operation, req *http.Request, secrets, queryKeys []string) (readResponse, error) {
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
	resp, err := cfg.HTTP.Do(req)
	if err != nil {
		return readResponse{}, scrubTransport(err, secrets, queryKeys)
	}
	raw, cut, err := consumeBody(resp, cfg)
	if err != nil {
		return readResponse{}, err
	}
	return readResponse{resp: resp, raw: raw, cut: cut}, nil
}

// Credentialed calls stay on the origin unless FollowRedirects is set.
// Same scheme and host use the default redirect policy, including 301, 302, and 303.
func boundRedirects(client *http.Client, credentialed, follow bool) *http.Client {
	if client == nil || !credentialed || follow {
		return client
	}
	dup := *client
	prev := dup.CheckRedirect
	dup.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 || req == nil || req.URL == nil || via[0] == nil || via[0].URL == nil {
			return http.ErrUseLastResponse
		}
		if !sameOrigin(via[0].URL, req.URL) {
			return http.ErrUseLastResponse
		}
		if prev != nil {
			return prev(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &dup
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
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
	for try := range attempts {
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
			return resp, fmt.Errorf("close body: %w", err)
		}
		if err := waitRetry(req.Context(), delay); err != nil {
			return resp, err
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

// Draft is the upstream request without credentials, with secret values removed.
func (c Client) Draft(ctx context.Context, op *catalog.Operation, params map[string]string) (runtime.HTTPRequest, error) {
	if op == nil {
		return runtime.HTTPRequest{}, errors.New("missing operation")
	}
	req, err := c.draftRequest(ctx, op, params)
	if err != nil {
		return runtime.HTTPRequest{}, err
	}
	method, rawURL, headers, body := sanitize(req)
	return runtime.HTTPRequest{Method: method, URL: rawURL, Headers: headers, Body: body}, nil
}

func (c Client) draftRequest(ctx context.Context, op *catalog.Operation, params map[string]string) (*http.Request, error) {
	base := strings.TrimSpace(c.BaseURL)
	if base == "" {
		base = op.BaseURL
	}
	if base == "" {
		return nil, fmt.Errorf("operation %s has no server URL", op.ID)
	}
	return prepareRequest(ctx, base, op, params, false)
}

func prepareRequest(ctx context.Context, base string, op *catalog.Operation, params map[string]string, strict bool) (*http.Request, error) {
	if op == nil {
		return nil, errors.New("missing operation")
	}
	if strict {
		if err := op.CheckParams(params); err != nil {
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
	if key := runtime.IdempotencyFrom(ctx); key != "" {
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

func paramNames(params map[string]string) string {
	return strings.Join(slices.Sorted(maps.Keys(params)), ",")
}

func bodyLimit(limit int64) int64 {
	if limit <= 0 {
		return DefaultMaxResponseBytes
	}
	return limit
}

func readCapped(r io.Reader, limit int64) ([]byte, bool, error) {
	if r == nil {
		return nil, false, nil
	}
	limit = bodyLimit(limit)
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(b)) > limit {
		return b[:limit], true, nil
	}
	return b, false, nil
}
