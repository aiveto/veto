package opa

import (
	"context"
	"fmt"
	"os"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/policy"
	"github.com/open-policy-agent/opa/v1/rego"
)

// data.veto.decision is allow, deny, or confirmation. data.veto.reason is a short string.

type Engine struct {
	query       rego.PreparedEvalQuery
	next        policy.Hook
	Environment string
	Principal   string
}

func New(ctx context.Context, file, bundle string, next policy.Hook) (*Engine, error) {
	if file == "" && bundle == "" {
		return nil, fmt.Errorf("policy opa needs policy_file or policy_bundle")
	}
	if file != "" && bundle != "" {
		return nil, fmt.Errorf("policy opa takes policy_file or policy_bundle")
	}
	if next == nil {
		next = policy.Builtin{}
	}
	opts := []func(*rego.Rego){
		rego.Query(`{"decision": data.veto.decision, "reason": data.veto.reason}`),
	}
	if file != "" {
		src, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read policy: %w", err)
		}
		opts = append(opts, rego.Module("policy.rego", string(src)))
	} else {
		opts = append(opts, rego.LoadBundle(bundle))
	}
	prepared, err := rego.New(opts...).PrepareForEval(ctx)
	if err != nil {
		return nil, fmt.Errorf("prepare policy: %w", err)
	}
	return &Engine{query: prepared, next: next}, nil
}

func (e *Engine) Check(ctx context.Context, op *catalog.Operation) (policy.Decision, error) {
	decision, _, err := e.evaluate(ctx, op)
	if err != nil || decision != policy.DecisionAllow {
		return decision, err
	}
	next := e.next
	if next == nil {
		next = policy.Builtin{}
	}
	return next.Check(ctx, op)
}

func (e *Engine) evaluate(ctx context.Context, op *catalog.Operation) (policy.Decision, string, error) {
	if e == nil {
		return policy.DecisionDeny, "", fmt.Errorf("missing policy")
	}
	if op == nil {
		return policy.DecisionDeny, "", fmt.Errorf("missing operation")
	}
	in := policy.InputFrom(ctx)
	params := map[string]string{}
	for k, v := range in.Params {
		params[k] = v
	}
	input := map[string]any{
		"operation": op.ID,
		"params":    params,
	}
	if e.Environment != "" {
		input["environment"] = e.Environment
	}
	if e.Principal != "" {
		input["principal"] = e.Principal
	}
	rs, err := e.query.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return policy.DecisionDeny, "", fmt.Errorf("policy: %w", err)
	}
	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return policy.DecisionDeny, "", fmt.Errorf("policy returned no decision")
	}
	decision, reason, err := decisionFields(rs[0].Expressions[0].Value)
	if err != nil {
		return policy.DecisionDeny, "", err
	}
	switch decision {
	case "allow":
		return policy.DecisionAllow, reason, nil
	case "deny":
		return policy.DecisionDeny, reason, nil
	case "confirmation", "confirmation_required":
		return policy.DecisionConfirmationNeeded, reason, nil
	default:
		return policy.DecisionDeny, reason, fmt.Errorf("policy decision %q", decision)
	}
}

func decisionFields(value any) (string, string, error) {
	obj, ok := value.(map[string]any)
	if !ok {
		return "", "", fmt.Errorf("policy decision is not an object")
	}
	decision, _ := obj["decision"].(string)
	reason, _ := obj["reason"].(string)
	if decision == "" {
		return "", reason, fmt.Errorf("policy returned no decision")
	}
	return decision, reason, nil
}
