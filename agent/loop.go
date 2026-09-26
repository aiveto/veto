package agent

import (
	"context"
	"fmt"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/flow"
	"github.com/aiveto/veto/memory"
	"github.com/aiveto/veto/model"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/semantics"
	"github.com/aiveto/veto/telemetry"
	"github.com/google/uuid"
)

// Call is one policy-gated execution of an operation.
type Call struct {
	Status      string
	ApprovalID  string
	OperationID string
	HTTPStatus  int
	Body        string
	Error       string
}

// Outcome is the result of one user turn.
type Outcome struct {
	OperationID string
	Status      string
	ApprovalID  string
	Text        string
	Pack        runctx.Pack
}

// New builds a loop with the in-tree defaults. Catalog is required.
func New(cat *catalog.Catalog, sem semantics.Provider, exec execute.Config) *Loop {
	if sem == nil && cat != nil {
		sem = semantics.NewDerived(cat)
	}
	return &Loop{
		Catalog:   cat,
		Semantics: sem,
		Model:     model.NewScripted(),
		Policy:    policy.Builtin{},
		State:     policy.NewState(),
		Execute:   exec,
		Memory:    memory.NewLocalMap(),
		Flows:     map[string]*flow.Definition{},
		Packs:     runctx.NewBuilder(0),
	}
}

// Loop runs one agent turn: model, policy, execute, result back to the model.
type Loop struct {
	Catalog   *catalog.Catalog
	Semantics semantics.Provider
	Model     model.Model
	Policy    policy.Hook
	State     *policy.State
	Execute   execute.Config
	Memory    memory.Memory
	Flows     map[string]*flow.Definition
	Packs     *runctx.Builder
}

// Run executes the loop for one user message.
func (l *Loop) Run(ctx context.Context, userText string) (Outcome, error) {
	span := telemetry.StartSpan(ctx, "agent.run")
	defer span.End()
	l.ready()
	span.SetAttributes(telemetry.Attr("user_message", userText))

	turns := l.memoryTurns(ctx, userText)
	turns = append(turns, runctx.Turn{Role: "user", Content: userText})
	pack := l.Packs.Build(l.Catalog, turns, nil, l.Semantics, nil)
	span.SetAttributes(telemetry.Attr("tools", pack.Index))
	resp, err := l.Model.Complete(ctx, model.Request{UserMessage: userText, Context: pack.Serialize()})
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
	followTurns := append(turns, runctx.Turn{Role: "tool", Content: call.Status + " " + call.OperationID})
	follow := l.Packs.Build(l.Catalog, followTurns, described, l.Semantics, pending)
	text := call.Status
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
	resp, err := execute.Invoke(ctx, l.Execute, op, params)
	if err != nil {
		return Call{Status: "error", Error: err.Error(), OperationID: operationID}, err
	}
	body, _ := execute.ReadBody(resp)
	return Call{
		Status:      "ok",
		OperationID: operationID,
		HTTPStatus:  resp.StatusCode,
		Body:        body,
	}, nil
}

func (l *Loop) runFlow(ctx context.Context, resp model.Response) (Call, error) {
	def := l.Flows[resp.FlowName]
	if def == nil {
		return Call{}, fmt.Errorf("unknown flow %q", resp.FlowName)
	}
	runner := flow.Runner{Invoke: func(ctx context.Context, operationID string, params map[string]string, approvalID string) (string, error) {
		if len(params) == 0 {
			params = resp.Params
		}
		call, err := l.Invoke(ctx, operationID, params, approvalID)
		if err != nil {
			return "", err
		}
		if call.Status != "ok" {
			return call.Status, fmt.Errorf("%s", call.Status)
		}
		return call.Status, nil
	}}
	results, err := runner.Run(ctx, def, resp.Params)
	if err != nil {
		return Call{Status: "error", OperationID: resp.OperationID, Error: err.Error()}, err
	}
	last := ""
	if n := len(def.Steps); n > 0 {
		last = def.Steps[n-1]
	}
	status := "ok"
	if len(results) > 0 {
		status = results[len(results)-1]
	}
	return Call{Status: status, OperationID: last}, nil
}

func (l *Loop) ready() {
	if l.Packs == nil {
		l.Packs = runctx.NewBuilder(0)
	}
	if l.State == nil {
		l.State = policy.NewState()
	}
	if l.Model == nil {
		l.Model = model.NewScripted()
	}
	if l.Memory == nil {
		l.Memory = memory.NewLocalMap()
	}
	if l.Flows == nil {
		l.Flows = map[string]*flow.Definition{}
	}
}

func (l *Loop) memoryTurns(ctx context.Context, userText string) []runctx.Turn {
	if l.Memory == nil {
		return nil
	}
	items, err := l.Memory.Search(ctx, userText)
	if err != nil || len(items) == 0 {
		return nil
	}
	turns := make([]runctx.Turn, 0, len(items))
	for _, it := range items {
		turns = append(turns, runctx.Turn{Role: "memory", Content: it.Content})
	}
	return turns
}
