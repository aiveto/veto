package flow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/internal/jsonfield"
)

// Witness reports what a finished read-only run kept true.
// The lines name fields and operations. They do not include response values.
func Witness(def *Definition, cat *catalog.Catalog, bodies []string) ([]string, error) {
	if def == nil || strings.TrimSpace(def.Question) == "" {
		return nil, errors.New("task is missing")
	}
	name := def.Name
	if cat == nil {
		return nil, fmt.Errorf("task %s: catalog is missing", name)
	}
	if len(def.Steps) == 0 || len(bodies) != len(def.Steps) {
		return nil, fmt.Errorf("task %s: the run did not finish", name)
	}
	for _, step := range def.Steps {
		op := cat.ByID(step.Operation)
		if op == nil {
			return nil, fmt.Errorf("task %s: unknown operation %s", name, step.Operation)
		}
		if op.Kind != catalog.KindRead {
			return nil, fmt.Errorf("task %s: %s is not read-only", name, op.ID)
		}
	}
	var lines []string
	for i := 1; i < len(def.Steps); i++ {
		prev := def.Steps[i-1]
		field := strings.TrimSpace(prev.Output)
		if field == "" {
			return nil, fmt.Errorf("task %s: no relation binds %s to %s", name, prev.Operation, def.Steps[i].Operation)
		}
		if _, ok := jsonfield.String(bodies[i-1], field); !ok {
			return nil, fmt.Errorf("task %s can no longer obtain %s from %s", name, field, prev.Operation)
		}
		param := strings.TrimSpace(prev.To)
		if param == "" {
			param = field
		}
		lines = append(lines, prev.Operation+" supplied "+field)
		lines = append(lines, def.Steps[i].Operation+" accepted "+param)
	}
	last := def.Steps[len(def.Steps)-1]
	var missing []string
	for _, field := range def.Answer {
		if _, ok := jsonfield.String(bodies[len(bodies)-1], field); !ok {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("task %s can no longer read %s from %s", name, strings.Join(missing, ", "), last.Operation)
	}
	if len(def.Answer) > 0 {
		lines = append(lines, last.Operation+" included "+strings.Join(def.Answer, ", "))
	}
	lines = append(lines, name+" stayed read-only")
	return lines, nil
}

// RunFailure turns a stopped task run into the sentence the owner reviews.
func RunFailure(def *Definition, err error) error {
	if err == nil || def == nil {
		return err
	}
	if missing, ok := errors.AsType[MissingOutputError](err); ok {
		return fmt.Errorf("task %s can no longer obtain %s from %s", def.Name, missing.Field, missing.Operation)
	}
	if stopped, ok := errors.AsType[StoppedError](err); ok {
		return notAccepted(def, stopped.Operation, stopped.Why)
	}
	return err
}

func notAccepted(def *Definition, operation, stage string) error {
	param := ""
	for i, step := range def.Steps {
		if step.Operation != operation || i == 0 {
			continue
		}
		param = strings.TrimSpace(def.Steps[i-1].To)
		if param == "" {
			param = strings.TrimSpace(def.Steps[i-1].Output)
		}
		break
	}
	if param == "" {
		if stage == "" {
			return fmt.Errorf("task %s: %s did not accept the next call", def.Name, operation)
		}
		return fmt.Errorf("task %s: %s did not accept the next call: %s", def.Name, operation, stage)
	}
	if stage == "" {
		return fmt.Errorf("task %s: %s did not accept %s", def.Name, operation, param)
	}
	return fmt.Errorf("task %s: %s did not accept %s: %s", def.Name, operation, param, stage)
}
