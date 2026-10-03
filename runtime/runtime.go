// Package runtime is the invoke sequence shared by the CLI, MCP, and the library.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
	// A nil Gate is created on first use. Copies made before that call do not share the gate.
	// Runtime is stored and returned by value, so the publish lock stays off the struct.
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

// lazyGate publishes a nil Gate.
// Runtime is stored and returned by value, so this lock cannot be a field on it.
var lazyGate sync.Mutex

// invokeGate publishes one gate. A gate set before the first call is kept.
// Concurrent first calls on the same Runtime share that gate.
func (rt *Runtime) invokeGate() *InvokeGate {
	lazyGate.Lock()
	defer lazyGate.Unlock()
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
	ctx = auth.WithCaller(ctx, caller)
	op := rt.operation(req.Operation)
	if op == nil {
		return rt.record(ctx, Result{Status: "error"}), fmt.Errorf("unknown operation %q", req.Operation)
	}
	if op.Exposure == catalog.ExposureDiscovery {
		err := fmt.Errorf("operation %q is discovery-only", req.Operation)
		return rt.record(ctx, Result{
			Status:      "error",
			OperationID: req.Operation,
			Code:        "not_callable",
			Error:       err.Error(),
		}), err
	}

	args, err := wire(req.Arguments)
	if err != nil {
		return rt.record(ctx, Result{Status: "error", OperationID: op.ID}), err
	}
	if err := op.CheckParams(args); err != nil {
		res := Result{Status: "error", OperationID: op.ID}
		if _, ok := errors.AsType[result.ParamError](err); ok {
			res.Code = "missing_param"
		}
		return rt.record(ctx, res), err
	}

	ctx = policy.WithInput(ctx, policy.Input{
		Params:    args,
		Arguments: req.Arguments,
		Caller:    caller,
	})
	decision, err := rt.decide(ctx, op)
	if err != nil {
		return rt.record(ctx, Result{Status: "error"}), err
	}
	if decision == policy.DecisionConfirmationNeeded {
		if rt.State == nil {
			err := errors.New("confirmation state is not set")
			return rt.record(ctx, Result{
				Status:      "error",
				OperationID: req.Operation,
				Error:       err.Error(),
			}), err
		}
		ctx, span := telemetry.StartSpan(ctx, "policy.confirmation")
		defer span.End()
		span.SetAttributes(telemetry.Attr("operation.id", req.Operation))
		if req.Approval == "" {
			id, err := rt.State.RequestFor(caller, req.Operation, args)
			if err != nil {
				return rt.record(ctx, Result{Status: "error", OperationID: req.Operation}), err
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
		ok, err := rt.State.ConsumeFor(caller, req.Approval, req.Operation, args)
		if err != nil {
			return rt.record(ctx, Result{Status: "error"}), err
		}
		if !ok {
			return rt.record(ctx, Result{Status: "error", OperationID: req.Operation, Error: ErrInvalidApproval.Error()}), ErrInvalidApproval
		}
		span.SetAttributes(telemetry.Attr("approval.id", req.Approval))
		decision = policy.DecisionAllow
	}
	if decision != policy.DecisionAllow {
		return rt.record(ctx, Result{Status: "denied", OperationID: req.Operation}), nil
	}
	if rt.Exec == nil {
		return rt.record(ctx, Result{Status: "error"}), errors.New("missing executor")
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
		res := Result{Status: "error", OperationID: req.Operation}
		if _, ok := errors.AsType[result.ParamError](err); ok {
			res.Code = "missing_param"
		}
		return rt.record(ctx, res), err
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
	if floor == policy.DecisionDeny {
		decision = policy.DecisionDeny
	} else if floor == policy.DecisionConfirmationNeeded && decision != policy.DecisionDeny {
		decision = policy.DecisionConfirmationNeeded
	}
	if op != nil && op.RequiresConfirmation && decision != policy.DecisionDeny {
		return policy.DecisionConfirmationNeeded, nil
	}
	return decision, nil
}

func (rt *Runtime) record(ctx context.Context, res Result) Result {
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
