// Package runtime is the invoke sequence shared by the CLI, MCP, and the library.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/result"
	"github.com/aiveto/veto/telemetry"
)

// ErrInvalidApproval is an approved id that does not match this caller, operation, and parameters.
var ErrInvalidApproval = errors.New("invalid approval")

type (
	// Executor performs the HTTP call. It does not apply policy.
	Executor interface {
		InvokeHTTPResult(ctx context.Context, op *catalog.Operation, params map[string]string) (result.HTTPResult, error)
	}

	// Request is one invoke. Caller is the caller or tenant. Arguments stay typed until HTTP serialization.
	// Fields names the response fields to return. An empty list leaves the body unchanged.
	Request struct {
		Operation   string
		Arguments   map[string]any
		Caller      string
		Approval    string
		Idempotency string
		Fields      []string
		Offset      int
		Limit       int
	}

	// Result is the shaped outcome of one invoke.
	Result struct {
		Status      string       `json:"Status"`
		ApprovalID  string       `json:"ApprovalID"`
		OperationID string       `json:"OperationID"`
		HTTPStatus  int          `json:"HTTPStatus"`
		Body        string       `json:"Body"`
		Code        string       `json:"Code"`
		Retryable   bool         `json:"Retryable"`
		RetryAfter  string       `json:"RetryAfter,omitempty"`
		Error       string       `json:"Error"`
		Truncated   bool         `json:"Truncated"`
		Page        *result.Page `json:"Page"`
		Why         string       `json:"Why,omitempty"`
		Caller      string       `json:"Caller,omitempty"`
		HTTP        bool         `json:"HTTP"`
	}

	// HTTPRequest is the call that would be sent, with secret values removed.
	HTTPRequest struct {
		Method  string            `json:"method,omitempty"`
		URL     string            `json:"url,omitempty"`
		Headers map[string]string `json:"headers,omitempty"`
		Body    string            `json:"body,omitempty"`
	}

	// Preview is resolve, validate, and policy with no token fetch and no upstream HTTP.
	Preview struct {
		OperationID      string      `json:"operation_id,omitempty"`
		Method           string      `json:"method,omitempty"`
		Path             string      `json:"path,omitempty"`
		Request          HTTPRequest `json:"request"`
		Errors           []string    `json:"errors,omitempty"`
		Decision         string      `json:"decision,omitempty"`
		ApprovalRequired bool        `json:"approval_required"`
	}

	// Runtime resolves, validates, checks policy, verifies approval, executes, shapes, and records.
	// The owner constructs Gate so copies share one limiter. A nil Gate is created on first use.
	Runtime struct {
		Catalog *catalog.Catalog
		Policy  policy.Hook
		Base    policy.Hook
		State   *policy.State
		Exec    Executor
		Notify  policy.Notifier
		Gate    *InvokeGate
		now     func() time.Time
	}
)

func (rt *Runtime) invokeGate() *InvokeGate {
	if rt.Gate == nil {
		rt.Gate = &InvokeGate{}
	}
	return rt.Gate
}

// Invoke runs one operation. MCP uses this directly. It does not run the agent loop.
func (rt *Runtime) Invoke(ctx context.Context, req Request) (Result, error) {
	if rt == nil {
		return Result{Status: "error"}, errors.New("runtime required")
	}
	caller := requestCaller(ctx, req)
	ctx = auth.WithCaller(ctx, caller)
	if ok, wait := rt.invokeGate().allow(caller, rt.clock()); !ok {
		retry := ""
		if wait > 0 {
			retry = wait.String()
		}
		return rt.record(ctx, Result{
			Status:      "limited",
			OperationID: req.Operation,
			Code:        "invoke_limited",
			Error:       "invoke limit",
			RetryAfter:  retry,
		}), nil
	}
	op := rt.operation(req.Operation)
	if op == nil {
		err := fmt.Errorf("unknown operation %q", req.Operation)
		return rt.record(ctx, errorResult(req.Operation, "", err)), err
	}
	if op.Exposure == catalog.ExposureDiscovery {
		err := fmt.Errorf("operation %q is discovery-only", req.Operation)
		return rt.record(ctx, errorResult(req.Operation, "not_callable", err)), err
	}

	args, err := wire(req.Arguments)
	if err != nil {
		return rt.record(ctx, errorResult(op.ID, "", err)), err
	}
	if err := op.CheckParams(args); err != nil {
		return rt.record(ctx, errorResult(op.ID, "", err)), err
	}

	ctx = policy.WithInput(ctx, policy.Input{
		Params:    args,
		Arguments: req.Arguments,
		Caller:    caller,
	})
	decision, err := rt.decide(ctx, op)
	if err != nil {
		return rt.record(ctx, errorResult(req.Operation, "", err)), err
	}
	if decision == policy.DecisionConfirmationNeeded {
		if rt.State == nil {
			err := errors.New("confirmation state is not set")
			return rt.record(ctx, errorResult(req.Operation, "", err)), err
		}
		ctx, span := telemetry.StartSpan(ctx, "policy.confirmation")
		defer span.End()
		span.SetAttributes(telemetry.Attr("operation.id", req.Operation))
		if req.Approval == "" {
			id, err := rt.State.RequestFor(ctx, caller, req.Operation, args)
			if err != nil {
				return rt.record(ctx, errorResult(req.Operation, "", err)), err
			}
			span.SetAttributes(telemetry.Attr("approval.id", id))
			res := Result{
				Status:      "confirmation_required",
				ApprovalID:  id,
				OperationID: req.Operation,
			}
			if err := rt.notify(ctx, policy.Notice{ID: id, Operation: req.Operation, Caller: caller}); err != nil {
				res.Error = err.Error()
			}
			return rt.record(ctx, res), nil
		}
		ok, err := rt.State.ConsumeFor(ctx, caller, req.Approval, req.Operation, args)
		if err != nil {
			return rt.record(ctx, errorResult(req.Operation, "", err)), err
		}
		if !ok {
			return rt.record(ctx, errorResult(req.Operation, "", ErrInvalidApproval)), ErrInvalidApproval
		}
		span.SetAttributes(telemetry.Attr("approval.id", req.Approval))
		decision = policy.DecisionAllow
	}
	if decision != policy.DecisionAllow {
		return rt.record(ctx, Result{Status: "denied", OperationID: req.Operation}), nil
	}
	if rt.Exec == nil {
		err := errors.New("missing executor")
		return rt.record(ctx, errorResult(req.Operation, "", err)), err
	}
	ctx = WithIdempotency(ctx, req.Idempotency)
	if len(req.Fields) > 0 || req.Limit > 0 || req.Offset > 0 {
		ctx = WithProjection(ctx, Projection{
			Fields: req.Fields,
			Offset: req.Offset,
			Limit:  req.Limit,
		})
	}
	call, err := rt.Exec.InvokeHTTPResult(ctx, op, args)
	if err != nil {
		return rt.record(ctx, errorResult(req.Operation, "", err)), err
	}
	status := call.Code
	if status == "" {
		status = "ok"
	}
	return rt.record(ctx, Result{
		Status:      status,
		OperationID: req.Operation,
		HTTPStatus:  call.Status,
		Body:        call.Body,
		Code:        call.Code,
		Retryable:   call.Retryable,
		Truncated:   call.Truncated,
		Page:        call.Page,
		HTTP:        true,
	}), nil
}

// Preview resolves, validates, and checks policy. It does not fetch a token or call upstream.
func (rt *Runtime) Preview(ctx context.Context, req Request) (Preview, error) {
	caller := requestCaller(ctx, req)
	ctx = auth.WithCaller(ctx, caller)
	op := rt.operation(req.Operation)
	if op == nil {
		return Preview{Errors: []string{fmt.Sprintf("unknown operation %q", req.Operation)}}, nil
	}
	out := Preview{OperationID: op.ID, Method: op.Method, Path: op.PathTemplate}
	if op.Exposure == catalog.ExposureDiscovery {
		out.Errors = append(out.Errors, fmt.Sprintf("operation %q is discovery-only", req.Operation))
	}
	args, err := wire(req.Arguments)
	if err != nil {
		out.Errors = append(out.Errors, err.Error())
	} else if err := op.CheckParams(args); err != nil {
		out.Errors = append(out.Errors, err.Error())
	}
	if drafter, ok := rt.Exec.(Drafter); ok {
		draft, err := drafter.Draft(ctx, op, args)
		if err != nil {
			out.Errors = append(out.Errors, err.Error())
		}
		out.Request = draft
	}
	ctx = policy.WithInput(ctx, policy.Input{
		Params:    args,
		Arguments: req.Arguments,
		Caller:    caller,
	})
	decision, err := rt.decide(ctx, op)
	if err != nil {
		out.Errors = append(out.Errors, err.Error())
		return out, nil
	}
	if len(out.Errors) == 0 {
		out.Decision = string(decision)
		out.ApprovalRequired = decision == policy.DecisionConfirmationNeeded
	} else if decision == policy.DecisionDeny {
		out.Decision = string(decision)
	}
	return out, nil
}

func (rt *Runtime) clock() time.Time {
	if rt.now != nil {
		return rt.now()
	}
	return time.Now()
}

func errorResult(op, code string, err error) Result {
	res := Result{Status: "error", OperationID: op, Code: code}
	if err != nil {
		res.Error = err.Error()
		if res.Code == "" {
			res.Code = paramCode(err)
		}
	}
	return res
}

func paramCode(err error) string {
	if _, ok := errors.AsType[result.ParamError](err); ok {
		return "missing_param"
	}
	if _, ok := errors.AsType[result.BodyError](err); ok {
		return "invalid_body"
	}
	if missingAuth(err) {
		return "missing_auth"
	}
	return ""
}

func missingAuth(err error) bool {
	return err != nil && strings.Contains(err.Error(), " is unset")
}

func whyOf(res Result) string {
	if res.HTTP && (res.Status == "ok" || res.Status == "") {
		return ""
	}
	switch res.Status {
	case "confirmation_required":
		return "held until you approve"
	case "denied":
		return "policy denied"
	case "limited":
		return "invoke limit"
	}
	switch res.Code {
	case "invalid_body":
		if res.Error != "" {
			return res.Error
		}
		return "body does not match the schema"
	case "missing_param":
		if res.Error != "" {
			return res.Error
		}
		return "a required parameter is missing"
	case "missing_auth":
		return "missing auth"
	case "not_callable":
		if res.Error != "" {
			return res.Error
		}
	}
	if !res.HTTP && res.Error != "" {
		return res.Error
	}
	return ""
}

func requestCaller(ctx context.Context, req Request) string {
	if req.Caller != "" {
		return req.Caller
	}
	return auth.Caller(ctx)
}

func (rt *Runtime) notify(ctx context.Context, notice policy.Notice) error {
	if rt.Notify == nil {
		return nil
	}
	if err := rt.Notify.Pending(ctx, notice); err != nil {
		return fmt.Errorf("approval webhook: %w", err)
	}
	return nil
}

func (rt *Runtime) operation(id string) *catalog.Operation {
	if rt.Catalog == nil {
		return nil
	}
	return rt.Catalog.ByID(id)
}

func (rt *Runtime) decide(ctx context.Context, op *catalog.Operation) (policy.Decision, error) {
	base := rt.Base
	if base == nil {
		base = policy.Builtin{}
	}
	decision, err := policy.Check(ctx, rt.Policy, op)
	if err != nil {
		return decision, err
	}
	floor, ferr := policy.Check(ctx, base, op)
	if ferr != nil {
		return floor, ferr
	}
	return policy.Combine(decision, floor, op), nil
}

func (rt *Runtime) record(ctx context.Context, res Result) Result {
	if res.Caller == "" {
		res.Caller = auth.Caller(ctx)
	}
	if res.Why == "" {
		res.Why = whyOf(res)
	}
	_, span := telemetry.StartSpan(ctx, "runtime.outcome")
	defer span.End()
	if res.OperationID != "" {
		span.SetAttributes(telemetry.Attr("operation.id", res.OperationID))
	}
	if res.Status != "" {
		span.SetAttributes(telemetry.Attr("decision", res.Status))
	}
	return res
}
