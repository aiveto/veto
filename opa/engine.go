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
// The input is the call. A fact the call does not have is empty.

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
	next := e.next
	if next == nil {
		next = policy.Builtin{}
	}
	floor, err := next.Check(ctx, op)
	if err != nil || floor == policy.DecisionDeny {
		return floor, err
	}
	decision, _, err := e.evaluate(ctx, op)
	if err != nil || decision == policy.DecisionDeny {
		return decision, err
	}
	if decision == policy.DecisionConfirmationNeeded || floor == policy.DecisionConfirmationNeeded {
		return policy.DecisionConfirmationNeeded, nil
	}
	return policy.DecisionAllow, nil
}

func (e *Engine) evaluate(ctx context.Context, op *catalog.Operation) (policy.Decision, string, error) {
	if e == nil {
		return policy.DecisionDeny, "", fmt.Errorf("missing policy")
	}
	if op == nil {
		return policy.DecisionDeny, "", fmt.Errorf("missing operation")
	}
	in := policy.InputFrom(ctx)
	input := map[string]any{
		"operation":      op.ID,
		"params":         cloneParams(in.Params),
		"method":         op.Method,
		"path":           op.PathTemplate,
		"side_effect":    string(op.SideEffect),
		"permissions":    copyStrings(op.Permissions),
		"caller":         e.caller(in),
		"environment":    e.Environment,
		"auth_scheme":    schemeNames(op),
		"tags":           copyStrings(op.Tags),
		"resource_group": op.Group,
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

func (e *Engine) caller(in policy.Input) string {
	if in.Caller != "" {
		return in.Caller
	}
	if e == nil {
		return ""
	}
	return e.Principal
}

func cloneParams(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyStrings(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	return append([]string{}, in...)
}

func schemeNames(op *catalog.Operation) []string {
	names := []string{}
	if op == nil {
		return names
	}
	for _, a := range op.AuthSchemes() {
		if a.Name == "" {
			continue
		}
		names = append(names, a.Name)
	}
	return names
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
