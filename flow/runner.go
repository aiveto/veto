package flow

import (
	"context"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type (
	// Definition is a named sequence of operation ids.
	Definition struct {
		Name  string   `yaml:"name"`
		Steps []string `yaml:"steps"`
	}

	// Runner executes flow steps in order via invoke.
	Runner struct {
		Invoke func(ctx context.Context, operationID string, params map[string]string, approvalID string) (string, error)
	}
)

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
func (r *Runner) Run(ctx context.Context, def *Definition, params map[string]string) ([]string, error) {
	var results []string
	for _, step := range def.Steps {
		out, err := r.Invoke(ctx, step, params, "")
		if err != nil {
			return results, fmt.Errorf("step %s: %w", step, err)
		}
		results = append(results, out)
	}
	return results, nil
}
