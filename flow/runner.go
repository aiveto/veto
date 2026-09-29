package flow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type (
	// Step is one operation. Output names a response field copied to the next step's parameter To.
	Step struct {
		Operation string `yaml:"operation"`
		Output    string `yaml:"output"`
		To        string `yaml:"to"`
	}

	// Definition is a named sequence of steps.
	Definition struct {
		Name  string `yaml:"name"`
		Steps []Step `yaml:"steps"`
	}

	// Stopped means a step returned a non-ok status, including confirmation.
	Stopped struct {
		Status    string
		Operation string
	}

	// Runner executes flow steps in order via invoke.
	Runner struct {
		Invoke func(ctx context.Context, operationID string, params map[string]string, approvalID string) (string, string, error)
	}
)

func (s Stopped) Error() string { return s.Status }

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

// Load reads a flow yaml file.
func Load(path string) (*Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read flow: %w", err)
	}
	var def Definition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parse flow: %w", err)
	}
	return &def, nil
}

// Run executes each step through the invoke callback.
// The output field of a step is copied onto the next step's named parameter.
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
			return results, Stopped{Status: status, Operation: step.Operation}
		}
		lastBody = body
	}
	return results, nil
}

func cloneParams(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
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
