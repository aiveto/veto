// Package agent runs one turn. The model proposes, policy decides, and the follow-up pack goes back to the caller.
package agent

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/flow"
	"github.com/aiveto/veto/memory"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/result"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/runtime"
	"github.com/aiveto/veto/semantics"
	"github.com/aiveto/veto/telemetry"
	"github.com/google/uuid"
)

type (
	// Executor performs the HTTP call. The loop does not build the request.
	Executor = runtime.Executor

	Call struct {
		Status      string
		ApprovalID  string
		OperationID string
		HTTPStatus  int
		Body        string
		Code        string
		Retryable   bool
		Error       string
		Truncated   bool
		Page        *result.Page
		RetryAfter  string
	}

	Outcome struct {
		OperationID string
		Status      string
		ApprovalID  string
		Text        string
		Pack        runctx.Pack
	}

	Request struct {
		UserMessage string
		Context     string
	}

	Response struct {
		OperationID string
		Params      map[string]string
		FlowName    string
	}

	Completer interface {
		Complete(ctx context.Context, req Request) (Response, error)
	}

	Memory interface {
		Store(ctx context.Context, item memory.Item) error
		Search(ctx context.Context, query string) ([]memory.Item, error)
		Recent(ctx context.Context, n int) ([]memory.Item, error)
	}

	Notes interface {
		Note(operationID string) semantics.Note
		AllSynonyms() map[string][]string
	}

	// The follow-up pack is returned. The model is not called again.
	Loop struct {
		Catalog   *catalog.Catalog
		Semantics Notes
		Model     Completer
		Policy    policy.Hook
		base      policy.Hook
		State     *policy.State
		Exec      Executor
		Notify    policy.Notifier
		Memory    Memory
		Flows     map[string]*flow.Definition
		Packs     *runctx.Builder
		gate      *runtime.InvokeGate
	}
)

func New(cat *catalog.Catalog, sem Notes, exec Executor) (*Loop, error) {
	if cat == nil {
		return nil, errors.New("catalog required")
	}
	if sem == nil {
		sem = semantics.New(cat)
	}
	return &Loop{
		Catalog:   cat,
		Semantics: sem,
		Model:     NewScripted(),
		Policy:    policy.Builtin{},
		base:      policy.Builtin{},
		State:     policy.NewState(),
		Exec:      exec,
		Memory:    memory.New(),
		Flows:     map[string]*flow.Definition{},
		Packs:     runctx.NewBuilder(0),
		gate:      &runtime.InvokeGate{},
	}, nil
}

func (l *Loop) SetPolicy(hook policy.Hook) {
	if hook == nil {
		hook = policy.Builtin{}
	}
	l.Policy = hook
}

func (l *Loop) SetFloor(hook policy.Hook) {
	if l == nil {
		return
	}
	if hook == nil {
		hook = policy.Builtin{}
	}
	l.base = hook
}

// SetInvokeLimit sets calls per second per caller. Zero keeps 16.
func (l *Loop) SetInvokeLimit(n int) {
	if l == nil {
		return
	}
	if l.gate == nil {
		l.gate = &runtime.InvokeGate{}
	}
	l.gate.Per = n
}

func (l *Loop) WrapPolicy(around policy.Around) {
	next := l.Policy
	if next == nil {
		next = policy.Builtin{}
	}
	if l.base == nil {
		l.base = next
	}
	l.Policy = policy.Wrap(next, around)
}

func (l *Loop) Run(ctx context.Context, userText string) (Outcome, error) {
	ctx, span := telemetry.StartSpan(ctx, "agent.run")
	defer span.End()

	turns, err := l.memoryTurns(ctx, userText)
	if err != nil {
		return Outcome{}, fmt.Errorf("memory: %w", err)
	}
	turns = append(turns, runctx.Turn{Role: "user", Content: userText})
	pack := l.Packs.Build(l.Catalog, turns, nil, l.Semantics, nil)
	span.SetAttributes(telemetry.Attr("tools", pack.Index))
	resp, err := l.Model.Complete(ctx, Request{UserMessage: userText, Context: pack.Serialize()})
	if err != nil {
		return Outcome{}, fmt.Errorf("model: %w", err)
	}
	if resp.OperationID != "" {
		span.SetAttributes(telemetry.Attr("operation.id", resp.OperationID))
	}
	if resp.FlowName != "" {
		span.SetAttributes(telemetry.Attr("flow.name", resp.FlowName))
	}

	var call Call
	if resp.FlowName != "" {
		call, err = l.runFlow(ctx, resp)
	} else {
		call, err = l.Invoke(ctx, resp.OperationID, resp.Params, "")
	}
	if err != nil {
		return Outcome{}, err
	}

	described := l.Catalog.ByID(call.OperationID)
	var pending *policy.PendingConfirmation
	if call.ApprovalID != "" {
		pending = l.State.Pending(ctx, call.ApprovalID)
	}
	summary := call.Status
	if call.Status == "confirmation_required" {
		if pending == nil && call.ApprovalID != "" {
			pending = &policy.PendingConfirmation{
				ID:          call.ApprovalID,
				OperationID: call.OperationID,
				Params:      copyParams(resp.Params),
			}
		}
		var sentence map[string]string
		if pending != nil {
			sentence = pending.Params
		}
		summary = policy.ConfirmSentence(call.OperationID, sentence)
	} else if call.Code != "" {
		summary = fmt.Sprintf("%s code=%s retryable=%t", call.Status, call.Code, call.Retryable)
	}
	followTurns := slices.Clone(turns)
	followTurns = append(followTurns, runctx.Turn{Role: "tool", Content: summary + " " + call.OperationID})
	follow := l.Packs.Build(l.Catalog, followTurns, described, l.Semantics, pending)
	text := summary
	if l.Memory != nil {
		id := uuid.NewString()
		if err := l.Memory.Store(ctx, memory.Item{
			ID:      id,
			Content: userText + " " + call.Status,
			Tags:    []string{call.OperationID},
		}); err != nil {
			return Outcome{}, fmt.Errorf("memory: %w", err)
		}
	}
	opID := call.OperationID
	if opID == "" {
		opID = resp.OperationID
	}
	return Outcome{
		OperationID: opID,
		Status:      call.Status,
		ApprovalID:  call.ApprovalID,
		Text:        text,
		Pack:        follow,
	}, nil
}

// Runtime is the invoke sequence this loop uses. Policy and state are the loop's current values.
func (l *Loop) Runtime() runtime.Runtime {
	if l.base == nil {
		l.base = policy.Builtin{}
	}
	if l.gate == nil {
		l.gate = &runtime.InvokeGate{}
	}
	return runtime.Runtime{
		Catalog: l.Catalog,
		Policy:  l.Policy,
		Base:    l.base,
		State:   l.State,
		Exec:    l.Exec,
		Notify:  l.Notify,
		Gate:    l.gate,
	}
}

func (l *Loop) Invoke(ctx context.Context, operationID string, params map[string]string, approvalID string) (Call, error) {
	rt := l.Runtime()
	out, err := rt.Invoke(ctx, runtime.Request{
		Operation: operationID,
		Arguments: runtime.FromStrings(params),
		Approval:  approvalID,
	})
	return Call{
		Status:      out.Status,
		ApprovalID:  out.ApprovalID,
		OperationID: out.OperationID,
		HTTPStatus:  out.HTTPStatus,
		Body:        out.Body,
		Code:        out.Code,
		Retryable:   out.Retryable,
		Error:       out.Error,
		Truncated:   out.Truncated,
		Page:        out.Page,
		RetryAfter:  out.RetryAfter,
	}, err
}

func (l *Loop) runFlow(ctx context.Context, resp Response) (Call, error) {
	def := l.Flows[resp.FlowName]
	if def == nil {
		return Call{}, fmt.Errorf("unknown flow %q", resp.FlowName)
	}
	var paused Call
	runner := flow.Runner{Invoke: func(ctx context.Context, operationID string, params map[string]string, approvalID string) (string, string, error) {
		if len(params) == 0 {
			params = resp.Params
		}
		call, err := l.Invoke(ctx, operationID, params, approvalID)
		if err != nil {
			return "", "", err
		}
		if call.Status == "confirmation_required" {
			paused = call
		}
		return call.Status, call.Body, nil
	}}
	results, err := runner.Run(ctx, def, resp.Params)
	if stopped, ok := errors.AsType[flow.StoppedError](err); ok {
		if paused.ApprovalID != "" && (stopped.Operation == "" || stopped.Operation == paused.OperationID) {
			paused.Status = stopped.Status
			return paused, nil
		}
		return Call{Status: stopped.Status, OperationID: stopped.Operation}, nil
	}
	if err != nil {
		return Call{Status: "error", OperationID: resp.OperationID}, err
	}
	last := ""
	if n := len(def.Steps); n > 0 {
		last = def.Steps[n-1].Operation
	}
	status := "ok"
	if len(results) > 0 {
		status = results[len(results)-1]
	}
	return Call{Status: status, OperationID: last}, nil
}

func (l *Loop) memoryTurns(ctx context.Context, userText string) ([]runctx.Turn, error) {
	if l.Memory == nil {
		return nil, nil
	}
	recent, err := l.Memory.Recent(ctx, 8)
	if err != nil {
		return nil, err
	}
	found, err := l.Memory.Search(ctx, userText)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var turns []runctx.Turn
	add := func(it memory.Item) {
		if it.ID == "" || seen[it.ID] {
			return
		}
		seen[it.ID] = true
		turns = append(turns, runctx.Turn{Role: "memory", Content: it.Content})
	}
	for _, it := range recent {
		add(it)
	}
	for _, it := range found {
		add(it)
	}
	return turns, nil
}

func copyParams(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	return maps.Clone(in)
}
