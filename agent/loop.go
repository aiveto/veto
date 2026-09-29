package agent

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/flow"
	"github.com/aiveto/veto/memory"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/semantics"
	"github.com/aiveto/veto/telemetry"
	"github.com/google/uuid"
)

type (
	HTTPResult struct {
		Status    int
		Body      string
		Code      string
		Retryable bool
	}

	// The loop does not build the request.
	Executor interface {
		InvokeHTTPResult(ctx context.Context, op *catalog.Operation, params map[string]string) (HTTPResult, error)
	}

	ParamError struct {
		Operation string
		Name      string
	}

	Call struct {
		Status      string
		ApprovalID  string
		OperationID string
		HTTPStatus  int
		Body        string
		Code        string
		Retryable   bool
		Error       string
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
		State     *policy.State
		Exec      Executor
		Memory    Memory
		Flows     map[string]*flow.Definition
		Packs     *runctx.Builder
	}
)

func (e ParamError) Error() string {
	if e.Name == "" {
		return fmt.Sprintf("operation %s: empty path parameter", e.Operation)
	}
	return fmt.Sprintf("operation %s: %s required", e.Operation, e.Name)
}

func New(cat *catalog.Catalog, sem Notes, exec Executor) (*Loop, error) {
	if cat == nil {
		return nil, fmt.Errorf("catalog required")
	}
	if sem == nil {
		sem = semantics.NewDerived(cat)
	}
	return &Loop{
		Catalog:   cat,
		Semantics: sem,
		Model:     NewScripted(),
		Policy:    policy.Builtin{},
		State:     policy.NewState(),
		Exec:      exec,
		Memory:    memory.NewLocalMap(),
		Flows:     map[string]*flow.Definition{},
		Packs:     runctx.NewBuilder(0),
	}, nil
}

func (l *Loop) WrapPolicy(around policy.Around) {
	l.Policy = policy.Wrap(around)
}

func (l *Loop) Run(ctx context.Context, userText string) (Outcome, error) {
	span := telemetry.StartSpan(ctx, "agent.run")
	defer span.End()
	span.SetAttributes(telemetry.Attr("user_message", userText))

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
		pending = l.State.Pending(call.ApprovalID)
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
	followTurns := append(turns, runctx.Turn{Role: "tool", Content: summary + " " + call.OperationID})
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

func (l *Loop) Invoke(ctx context.Context, operationID string, params map[string]string, approvalID string) (Call, error) {
	op := l.Catalog.ByID(operationID)
	if op == nil {
		return Call{Status: "error"}, fmt.Errorf("unknown operation %q", operationID)
	}
	decision, err := policy.Check(ctx, l.Policy, op)
	if err != nil {
		return Call{Status: "error"}, err
	}
	if decision == policy.DecisionConfirmationNeeded {
		span := telemetry.StartSpan(ctx, "policy.confirmation")
		defer span.End()
		span.SetAttributes(telemetry.Attr("operation.id", operationID))
		if approvalID == "" {
			id := l.State.RequestConfirmation(operationID, params)
			span.SetAttributes(telemetry.Attr("approval.id", id))
			return Call{
				Status:      "confirmation_required",
				ApprovalID:  id,
				OperationID: operationID,
			}, nil
		}
		if !l.State.ConsumeConfirmation(approvalID, operationID, params) {
			return Call{Status: "error"}, fmt.Errorf("invalid approval")
		}
		span.SetAttributes(telemetry.Attr("approval.id", approvalID))
		decision = policy.DecisionAllow
	}
	if decision != policy.DecisionAllow {
		return Call{Status: "denied", OperationID: operationID}, nil
	}
	if l.Exec == nil {
		return Call{Status: "error"}, fmt.Errorf("missing executor")
	}
	result, err := l.Exec.InvokeHTTPResult(ctx, op, params)
	if err != nil {
		call := Call{Status: "error", OperationID: operationID}
		if _, ok := errors.AsType[ParamError](err); ok {
			call.Code = "missing_param"
		}
		return call, err
	}
	return Call{
		Status:      "ok",
		OperationID: operationID,
		HTTPStatus:  result.Status,
		Body:        result.Body,
		Code:        result.Code,
		Retryable:   result.Retryable,
	}, nil
}

func (l *Loop) runFlow(ctx context.Context, resp Response) (Call, error) {
	def := l.Flows[resp.FlowName]
	if def == nil {
		return Call{}, fmt.Errorf("unknown flow %q", resp.FlowName)
	}
	runner := flow.Runner{Invoke: func(ctx context.Context, operationID string, params map[string]string, approvalID string) (string, string, error) {
		if len(params) == 0 {
			params = resp.Params
		}
		call, err := l.Invoke(ctx, operationID, params, approvalID)
		if err != nil {
			return "", "", err
		}
		return call.Status, call.Body, nil
	}}
	results, err := runner.Run(ctx, def, resp.Params)
	if stopped, ok := errors.AsType[flow.Stopped](err); ok {
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

func copyParams(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	return maps.Clone(in)
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
