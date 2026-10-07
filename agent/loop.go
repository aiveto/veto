// Package agent runs one turn. The model proposes, policy decides, and the follow-up pack goes back to the caller.
package agent

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/flow"
	"github.com/aiveto/veto/jsonopts"
	"github.com/aiveto/veto/memory"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/runtime"
	"github.com/aiveto/veto/semantics"
	"github.com/aiveto/veto/telemetry"
	"github.com/google/uuid"
)

var ErrEmptyPack = errors.New("empty context pack")

type (
	// Executor performs the HTTP call. The loop does not build the request.
	Executor = runtime.Executor

	// Call is the invoke outcome. The loop does not reshape it.
	Call = runtime.Result

	Outcome struct {
		OperationID string
		Status      string
		ApprovalID  string
		Text        string
		Pack        runctx.Pack
		Result      runtime.Result
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

	// The follow-up pack is returned. The model is not called again.
	Loop struct {
		Catalog   *catalog.Catalog
		Semantics semantics.Notes
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
		JSON      jsonopts.Set
		Pages     int
		MaxBody   int64
		calls     *runtime.Runtime
	}
)

func New(cat *catalog.Catalog, sem semantics.Notes, exec Executor) (*Loop, error) {
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
	if l == nil {
		return
	}
	if hook == nil {
		hook = policy.Builtin{}
	}
	l.Policy = hook
	if l.calls != nil {
		l.calls.Policy = hook
	}
}

func (l *Loop) SetFloor(hook policy.Hook) {
	if l == nil {
		return
	}
	if hook == nil {
		hook = policy.Builtin{}
	}
	l.base = hook
	if l.calls != nil {
		l.calls.Base = hook
	}
}

// SetInvokeLimit sets calls per second per caller. Zero keeps 16.
func (l *Loop) SetInvokeLimit(n int) {
	if l == nil {
		return
	}
	if l.calls != nil {
		if l.calls.Gate == nil {
			l.calls.Gate = &runtime.InvokeGate{}
		}
		l.calls.Gate.Per = n
		l.gate = l.calls.Gate
		return
	}
	if l.gate == nil {
		l.gate = &runtime.InvokeGate{}
	}
	l.gate.Per = n
}

// SetGate shares one limiter with another Runtime.
func (l *Loop) SetGate(gate *runtime.InvokeGate) {
	if l == nil {
		return
	}
	if gate == nil {
		gate = &runtime.InvokeGate{}
	}
	l.gate = gate
	if l.calls != nil {
		l.calls.Gate = gate
	}
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
	if l.calls != nil {
		l.calls.Policy = l.Policy
	}
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
	req := Request{UserMessage: userText, Context: pack.Serialize()}
	if err := emptyPack(req); err != nil {
		return Outcome{}, fmt.Errorf("model: %w", err)
	}
	resp, err := l.Model.Complete(ctx, req)
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
	out, memErr := l.outcome(ctx, userText, turns, resp, call, err == nil)
	if err != nil {
		return out, err
	}
	return out, memErr
}

func (l *Loop) outcome(ctx context.Context, userText string, turns []runctx.Turn, resp Response, call Call, store bool) (Outcome, error) {
	described := l.Catalog.ByID(call.OperationID)
	var pending *policy.PendingConfirmation
	if call.ApprovalID != "" && l.State != nil {
		pending = l.State.Pending(ctx, call.ApprovalID)
	}
	summary := call.Status
	if call.Status == runtime.StatusConfirmationRequired {
		if pending == nil && call.ApprovalID != "" {
			pending = &policy.PendingConfirmation{
				ID:          call.ApprovalID,
				OperationID: call.OperationID,
				Params:      copyParams(resp.Params),
				Caller:      call.Caller,
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
	content := summary + " " + call.OperationID
	if call.Body != "" {
		content += "\n" + call.Body
	}
	followTurns := slices.Clone(turns)
	followTurns = append(followTurns, runctx.Turn{Role: "tool", Content: content})
	follow := l.Packs.Build(l.Catalog, followTurns, described, l.Semantics, pending)
	opID := call.OperationID
	if opID == "" {
		opID = resp.OperationID
	}
	out := Outcome{
		OperationID: opID,
		Status:      call.Status,
		ApprovalID:  call.ApprovalID,
		Text:        summary,
		Pack:        follow,
		Result:      call,
	}
	if store && l.Memory != nil {
		if err := l.Memory.Store(ctx, memory.Item{
			ID:      uuid.NewString(),
			Content: userText + " " + call.Status,
			Tags:    []string{call.OperationID},
		}); err != nil {
			return out, fmt.Errorf("memory: %w", err)
		}
	}
	return out, nil
}

// SetRuntime uses rt for invoke. The loop still owns the model, memory, and context pack.
func (l *Loop) SetRuntime(rt *runtime.Runtime) {
	if l == nil || rt == nil {
		return
	}
	l.calls = rt
	if rt.Catalog != nil {
		l.Catalog = rt.Catalog
	}
	l.Policy = rt.Policy
	l.base = rt.Base
	l.State = rt.State
	l.Exec = rt.Exec
	l.Notify = rt.Notify
	l.gate = rt.Gate
	l.JSON = rt.JSON
	l.Pages = rt.Pages
	l.MaxBody = rt.MaxBody
}

// Runtime is the invoke sequence this loop uses. Policy and state are the loop's current values.
// The snapshot copies exported fields. The shared runtime, including its gate lock, stays on RuntimePtr.
func (l *Loop) Runtime() runtime.Runtime {
	if l != nil && l.calls != nil {
		return runtimeSnapshot(l.calls)
	}
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
		JSON:    l.JSON,
		Pages:   l.Pages,
		MaxBody: l.MaxBody,
	}
}

func runtimeSnapshot(rt *runtime.Runtime) runtime.Runtime {
	return runtime.Runtime{
		Catalog: rt.Catalog,
		Policy:  rt.Policy,
		Base:    rt.Base,
		State:   rt.State,
		Exec:    rt.Exec,
		Notify:  rt.Notify,
		Gate:    rt.Gate,
		JSON:    rt.JSON,
		Pages:   rt.Pages,
		MaxBody: rt.MaxBody,
	}
}

// RuntimePtr is the runtime Invoke uses. A shared runtime is that pointer.
func (l *Loop) RuntimePtr() *runtime.Runtime {
	if l == nil {
		return nil
	}
	if l.calls != nil {
		return l.calls
	}
	rt := l.Runtime()
	return &rt
}

func (l *Loop) Invoke(ctx context.Context, operationID string, params map[string]string, approvalID string) (Call, error) {
	rt := l.RuntimePtr()
	return rt.Invoke(ctx, runtime.Request{
		Operation: operationID,
		Arguments: runtime.FromStrings(params),
		Approval:  approvalID,
	})
}

func (l *Loop) runFlow(ctx context.Context, resp Response) (Call, error) {
	def := l.Flows[resp.FlowName]
	if def == nil {
		return Call{}, fmt.Errorf("unknown flow %q", resp.FlowName)
	}
	var paused Call
	var last Call
	runner := flow.Runner{Invoke: func(ctx context.Context, operationID string, params map[string]string, approvalID string) (string, string, error) {
		if len(params) == 0 {
			params = resp.Params
		}
		call, err := l.Invoke(ctx, operationID, params, approvalID)
		if err != nil {
			last = call
			return "", "", err
		}
		last = call
		if call.Status == runtime.StatusConfirmationRequired {
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
		if last.OperationID != "" {
			return last, err
		}
		return Call{Status: runtime.StatusError, OperationID: resp.OperationID}, err
	}
	if last.OperationID != "" {
		return last, nil
	}
	step := ""
	if n := len(def.Steps); n > 0 {
		step = def.Steps[n-1].Operation
	}
	status := runtime.StatusOK
	if len(results) > 0 {
		status = results[len(results)-1]
	}
	return Call{Status: status, OperationID: step}, nil
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

func emptyPack(req Request) error {
	if strings.TrimSpace(req.Context) == "" {
		return ErrEmptyPack
	}
	return nil
}
