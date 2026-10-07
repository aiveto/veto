package runtime

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/internal/jsonfield"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/result"
)

type pageFinisher interface {
	FinishPages(status int, body string, truncated bool, p Projection) (result.HTTPResult, error)
}

func (rt *Runtime) collectPages(ctx context.Context, op *catalog.Operation, params map[string]string, first result.HTTPResult, proj Projection) (result.HTTPResult, error) {
	items, ok := pageItems(first.Body)
	if !ok {
		return first, nil
	}
	limit := rt.bodyLimit()
	current := cloneParams(params)
	seen := map[string]bool{}
	truncated := first.Truncated
	body := first.Body
	pageCap := rt.Pages
	for page := 1; page < pageCap; page++ {
		next, ok := nextPage(op.Page, body)
		if !ok {
			break
		}
		sig := fmt.Sprint(next)
		if seen[sig] {
			break
		}
		seen[sig] = true
		maps.Copy(current, next)
		if err := op.CheckParams(current); err != nil {
			return sentPages(first.Status), err
		}
		if !rt.allowDerived(ctx, op, current) {
			truncated = true
			break
		}
		resp, err := rt.Exec.InvokeHTTPResult(ctx, op, current)
		if err != nil {
			return resp, err
		}
		if resp.Status < 200 || resp.Status >= 300 {
			return resp, fmt.Errorf("follow page: http %d", resp.Status)
		}
		body = resp.Body
		more, ok := pageItems(body)
		if !ok {
			return resp, errors.New("follow page: response is not a page")
		}
		if pageBytes(items)+pageBytes(more) > int(limit) {
			truncated = true
			break
		}
		items = append(items, more...)
		if page+1 == pageCap {
			later, found := nextPage(op.Page, body)
			if found && !seen[fmt.Sprint(later)] {
				truncated = true
			}
		}
	}
	raw, err := jsonv2.Marshal(items)
	if err != nil {
		return sentPages(first.Status), fmt.Errorf("collect pages: %w", err)
	}
	if int64(len(raw)) > limit {
		return sentPages(first.Status), fmt.Errorf("response exceeds %d bytes", limit)
	}
	merged := result.HTTPResult{
		Status:    first.Status,
		Body:      string(raw),
		Code:      first.Code,
		Retryable: first.Retryable,
		HTTP:      true,
		Sent:      true,
		Truncated: truncated,
	}
	if finisher, ok := rt.Exec.(pageFinisher); ok {
		return finisher.FinishPages(first.Status, merged.Body, truncated, proj)
	}
	return merged, nil
}

func (rt *Runtime) allowDerived(ctx context.Context, op *catalog.Operation, params map[string]string) bool {
	ctx = rt.policyContext(ctx, params, FromStrings(params))
	decision, err := rt.decide(ctx, op)
	return err == nil && decision == policy.DecisionAllow
}

func sentPages(status int) result.HTTPResult {
	return result.HTTPResult{Status: status, HTTP: true, Sent: true}
}

func (rt *Runtime) bodyLimit() int64 {
	if rt != nil && rt.MaxBody > 0 {
		return rt.MaxBody
	}
	return 1 << 20
}

func nextPage(mapping map[string]string, body string) (map[string]string, bool) {
	if len(mapping) == 0 {
		return nil, false
	}
	out := make(map[string]string, len(mapping))
	for name, expr := range mapping {
		val, ok := jsonfield.String(body, responseField(expr))
		if !ok || val == "" {
			return nil, false
		}
		out[name] = val
	}
	return out, true
}

func responseField(expr string) string {
	field := strings.TrimSpace(expr)
	field = strings.TrimPrefix(field, "$response.body#")
	return strings.TrimPrefix(field, "/")
}

func pageItems(body string) ([]json.RawMessage, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, false
	}
	if body[0] == '[' {
		var items []json.RawMessage
		if jsonv2.Unmarshal([]byte(body), &items) != nil {
			return nil, false
		}
		return items, true
	}
	var obj map[string]json.RawMessage
	if jsonv2.Unmarshal([]byte(body), &obj) != nil {
		return nil, false
	}
	raw, ok := obj["items"]
	if !ok {
		return nil, false
	}
	var items []json.RawMessage
	if jsonv2.Unmarshal(raw, &items) != nil {
		return nil, false
	}
	return items, true
}

func pageBytes(items []json.RawMessage) int {
	n := 2
	for _, item := range items {
		n += len(item) + 1
	}
	return n
}

func cloneParams(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}
