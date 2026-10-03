// Package flow runs a sequence of steps the model may name.
package flow

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/aiveto/veto/internal/jsonfield"
	"github.com/aiveto/veto/internal/yamlfile"
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
	if value.Kind == yaml.AliasNode && value.Alias != nil {
		return s.UnmarshalYAML(value.Alias)
	}
	if value.Kind == yaml.ScalarNode {
		s.Operation = value.Value
		return nil
	}
	if value.Kind != yaml.MappingNode {
		return errors.New("flow step must be a string or mapping")
	}
	known := map[string]bool{"operation": true, "output": true, "to": true}
	seen := map[string]bool{}
	for i := 0; i+1 < len(value.Content); i += 2 {
		key := value.Content[i].Value
		if seen[key] {
			return fmt.Errorf("duplicate field %q", key)
		}
		seen[key] = true
		if !known[key] {
			return fmt.Errorf("unknown field %q", key)
		}
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
	if err := decodeStrict(data, &def); err != nil {
		return nil, fmt.Errorf("parse flow: %w", err)
	}
	return &def, nil
}

func decodeStrict(data []byte, out any) error {
	return yamlfile.Decode(data, out)
}

func (r *Runner) Run(ctx context.Context, def *Definition, params map[string]string) ([]string, error) {
	current := cloneParams(params)
	var results []string
	var lastBody string
	for i, step := range def.Steps {
		if i > 0 {
			prev := def.Steps[i-1]
			if prev.Output != "" {
				val, ok := jsonfield.String(lastBody, prev.Output)
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
