package flow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"gopkg.in/yaml.v3"
)

type (
	Step struct {
		Operation string `yaml:"operation"`
		Output    string `yaml:"output"` // copied onto the next step's parameter To
		To        string `yaml:"to"`
	}

	Definition struct {
		Name  string `yaml:"name"`
		Steps []Step `yaml:"steps"`
	}

	StoppedError struct {
		Status    string
		Operation string
	}

	Runner struct {
		Invoke func(ctx context.Context, operationID string, params map[string]string, approvalID string) (string, string, error)
	}
)

func (s StoppedError) Error() string { return s.Status }

func (s *Step) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		s.Operation = value.Value
		return nil
	}
	var raw struct {
		Operation string `yaml:"operation"`
		Output    string `yaml:"output"`
		To        string `yaml:"to"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	s.Operation = raw.Operation
	s.Output = raw.Output
	s.To = raw.To
	return nil
}

func Parse(data []byte) (*Definition, error) {
	var def Definition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parse flow: %w", err)
	}
	return &def, nil
}

func (r *Runner) Run(ctx context.Context, def *Definition, params map[string]string) ([]string, error) {
	current := cloneParams(params)
	var results []string
	var lastBody string
	for i, step := range def.Steps {
		if i > 0 {
			prev := def.Steps[i-1]
			if prev.Output != "" {
				val, ok := jsonField(lastBody, prev.Output)
				if !ok {
					return results, fmt.Errorf("step %s: output %s is missing", prev.Operation, prev.Output)
				}
				to := prev.To
				if to == "" {
					to = prev.Output
				}
				current[to] = val
			}
		}
		status, body, err := r.Invoke(ctx, step.Operation, current, "")
		if err != nil {
			return results, fmt.Errorf("step %s: %w", step.Operation, err)
		}
		results = append(results, status)
		if status != "ok" {
			return results, StoppedError{Status: status, Operation: step.Operation}
		}
		lastBody = body
	}
	return results, nil
}

func cloneParams(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}

func jsonField(body, field string) (string, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		return "", false
	}
	raw, ok := obj[field]
	if !ok {
		return "", false
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", false
		}
		return s, true
	}
	return string(raw), true
}
