package agent

import (
	"context"
	"errors"
	"fmt"

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
	// HTTPResult is the status, body, and stable code of one allowed HTTP call.
	HTTPResult struct {
		Status    int
		Body      string
		Code      string
		Retryable bool
	}

	// Executor performs one HTTP call. The loop does not build the request.
	Executor interface {
		Invoke(ctx context.Context, op *catalog.Operation, params map[string]string) (HTTPResult, error)
	}

	// ParamError is a required parameter that was empty.
	ParamError struct {
		Operation string
		Name      string
	}

	// Call is one policy-gated execution of an operation.
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

	// Outcome is the result of one user turn.
	Outcome struct {
		OperationID string
		Status      string
		ApprovalID  string
		Text        string
		Pack        runctx.Pack
	}

	// Request is input to Complete.
	Request struct {
		UserMessage string
		Context     string
	}

	// Response is the chosen operation and parameters.
	Response struct {
		OperationID string
		Params      map[string]string
		FlowName    string
	}

	// Completer selects an operation or flow from the pack.
	Completer interface {
		Complete(ctx context.Context, req Request) (Response, error)
	}

	// Loop runs one turn: model, policy, then HTTP when the call is allowed.
	// The follow-up pack is returned to the caller. The model is not called again.
	Loop struct {
		Catalog   *catalog.Catalog
		Semantics semantics.Provider
		Model     Completer
		Policy    policy.Hook
		State     *policy.State
		Exec      Executor
		Memory    memory.Memory
		Flows     map[string]*flow.Definition
		Packs     *runctx.Builder
	}
)

// New builds a loop with the in-tree defaults. Catalog is required.
func New(cat *catalog.Catalog, sem semantics.Provider, exec Executor) *Loop {
	if sem == nil && cat != nil {
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
	}
}

// WrapPolicy installs a hook that calls Builtin unless around stops.
func (l *Loop) WrapPolicy(around policy.Around) {
	l.Policy = policy.Wrap(around)
}

// Run executes the loop for one user message.
func (l *Loop) Run(ctx context.Context, userText string) (Outcome, error) {
	span := telemetry.StartSpan(ctx, "agent.run")
	defer span.End()
	l.ready()
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
		summary = policy.ConfirmSentence(call.OperationID, callParams(l, call.ApprovalID))
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

func (e ParamError) Error() string {
	if e.Name == "" {
		return fmt.Sprintf("operation %s: empty path parameter", e.Operation)
	}
	return fmt.Sprintf("operation %s: %s required", e.Operation, e.Name)
}

// Invoke checks policy and calls HTTP only when the call is allowed.
func (l *Loop) Invoke(ctx context.Context, operationID string, params map[string]string, approvalID string) (Call, error) {
	l.ready()
	op := l.Catalog.ByID(operationID)
	if op == nil {
		return Call{Status: "error", Error: "unknown operation"}, fmt.Errorf("unknown operation %q", operationID)
	}
	decision, err := policy.Check(ctx, l.Policy, op)
	if err != nil {
		return Call{Status: "error", Error: err.Error()}, err
	}
	if decision == policy.DecisionConfirmationNeeded {
		span := telemetry.StartSpan(ctx, "policy.confirmation")
		defer span.End()
		span.SetAttributes(telemetry.Attr("operation.id", operationID))
		if approvalID != "" && l.State.ConsumeConfirmation(approvalID, operationID) {
			span.SetAttributes(telemetry.Attr("approval.id", approvalID))
			decision = policy.DecisionAllow
		} else if approvalID == "" {
			id := l.State.RequestConfirmation(operationID, params)
			span.SetAttributes(telemetry.Attr("approval.id", id))
			return Call{
				Status:      "confirmation_required",
				ApprovalID:  id,
				OperationID: operationID,
			}, nil
		} else {
			return Call{Status: "error", Error: "invalid approval"}, fmt.Errorf("invalid approval")
		}
	}
	if decision != policy.DecisionAllow {
		return Call{Status: "denied", OperationID: operationID}, nil
	}
	if l.Exec == nil {
		return Call{Status: "error", Error: "missing executor"}, fmt.Errorf("missing executor")
	}
	result, err := l.Exec.Invoke(ctx, op, params)
	if err != nil {
		call := Call{Status: "error", Error: err.Error(), OperationID: operationID}
		var missing ParamError
		if errors.As(err, &missing) {
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
	var stopped flow.Stopped
	if errors.As(err, &stopped) {
		return Call{Status: stopped.Status, OperationID: stopped.Operation}, nil
	}
	if err != nil {
		return Call{Status: "error", OperationID: resp.OperationID, Error: err.Error()}, err
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

func callParams(l *Loop, approvalID string) map[string]string {
	if l == nil || l.State == nil || approvalID == "" {
		return nil
	}
	pending := l.State.Pending(approvalID)
	if pending == nil {
		return nil
	}
	return pending.Params
}

func (l *Loop) ready() {
	if l.Packs == nil {
		l.Packs = runctx.NewBuilder(0)
	}
	if l.State == nil {
		l.State = policy.NewState()
	}
	if l.Model == nil {
		l.Model = NewScripted()
	}
	if l.Memory == nil {
		l.Memory = memory.NewLocalMap()
	}
	if l.Flows == nil {
		l.Flows = map[string]*flow.Definition{}
	}
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
