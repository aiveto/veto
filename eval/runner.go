package eval

import (
	"context"
	"fmt"
	"os"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/model"
	"github.com/aiveto/veto/semantics"
	"gopkg.in/yaml.v3"
)

type (
	// Case is one eval fixture.
	Case struct {
		Name   string       `yaml:"name"`
		Input  string       `yaml:"input"`
		Expect Expectations `yaml:"expect"`
	}

	// Expectations are deterministic assertions.
	Expectations struct {
		ConfirmationRequired bool   `yaml:"confirmation_required"`
		OperationID          string `yaml:"operation"`
	}

	// Runner executes eval cases without a network LLM.
	Runner struct {
		Catalog   *catalog.Catalog
		Semantics semantics.Provider
		Model     model.Model
		Loop      *agent.Loop
	}
)

// LoadCase reads a case yaml file.
func LoadCase(path string) (*Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read case: %w", err)
	}
	var c Case
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse case: %w", err)
	}
	return &c, nil
}

// Run executes one case and returns an error on assertion failure.
func (r *Runner) Run(ctx context.Context, c *Case) error {
	if r.Loop == nil {
		r.Loop = &agent.Loop{
			Catalog:   r.Catalog,
			Semantics: r.Semantics,
			Model:     r.Model,
		}
	}
	out, err := r.Loop.Run(ctx, c.Input)
	if err != nil {
		return err
	}
	if c.Expect.OperationID != "" && out.OperationID != c.Expect.OperationID {
		return fmt.Errorf("expected operation %q, got %q", c.Expect.OperationID, out.OperationID)
	}
	if c.Expect.ConfirmationRequired && out.Status != "confirmation_required" {
		return fmt.Errorf("expected confirmation_required, got %q", out.Status)
	}
	return nil
}
