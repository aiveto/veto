// Package eval runs agent cases with the scripted model and the real policy path.
package eval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/internal/yamlfile"
	"github.com/aiveto/veto/result"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/runtime"
)

type (
	Case struct {
		Name   string       `yaml:"name"`
		Input  string       `yaml:"input"`
		Expect Expectations `yaml:"expect"`
	}

	Expectations struct {
		ConfirmationRequired bool     `yaml:"confirmation_required"`
		OperationID          string   `yaml:"operation"`
		NoHTTP               bool     `yaml:"no_http"`
		PackContains         []string `yaml:"pack_contains"`
		PackExcludes         []string `yaml:"pack_excludes"`
		Related              []string `yaml:"related"`
	}

	httpGate struct {
		next agent.Executor
		hits int
	}

	Runner struct {
		Catalog   *catalog.Catalog
		Semantics agent.Notes
		Model     agent.Completer
		Loop      *agent.Loop
	}
)

func LoadCases(paths []string) ([]*Case, error) {
	files, err := caseFiles(paths)
	if err != nil {
		return nil, err
	}
	out := make([]*Case, 0, len(files))
	for _, path := range files {
		c, err := LoadCase(path)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func caseFiles(paths []string) ([]string, error) {
	var out []string
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("read case: %w", err)
		}
		if !info.IsDir() {
			out = append(out, path)
			continue
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, fmt.Errorf("read case dir: %w", err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml") {
				out = append(out, filepath.Join(path, e.Name()))
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func LoadCase(path string) (*Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read case: %w", err)
	}
	var c Case
	if err := decodeStrict(data, &c); err != nil {
		return nil, fmt.Errorf("parse case: %w", err)
	}
	return &c, nil
}

func (r *Runner) Run(ctx context.Context, c *Case) error {
	if r.Loop == nil {
		loop, err := agent.New(r.Catalog, r.Semantics, nil)
		if err != nil {
			return err
		}
		if r.Model != nil {
			loop.Model = r.Model
		}
		r.Loop = loop
	}
	if err := r.checkPack(c); err != nil {
		return err
	}
	if c.Expect.OperationID == "" && !c.Expect.ConfirmationRequired && !c.Expect.NoHTTP {
		return nil
	}
	var gate *httpGate
	if c.Expect.NoHTTP {
		gate = &httpGate{next: r.Loop.Exec}
		r.Loop.Exec = gate
		defer func() { r.Loop.Exec = gate.next }()
	}
	out, err := r.Loop.Run(ctx, c.Input)
	if gate != nil && gate.hits != 0 {
		return errors.New("http ran")
	}
	if err != nil {
		return err
	}
	if c.Expect.OperationID != "" && out.OperationID != c.Expect.OperationID {
		return fmt.Errorf("expected operation %q, got %q", c.Expect.OperationID, out.OperationID)
	}
	if c.Expect.ConfirmationRequired && out.Status != runtime.StatusConfirmationRequired {
		return fmt.Errorf("expected %s, got %q", runtime.StatusConfirmationRequired, out.Status)
	}
	return nil
}

func (r *Runner) checkPack(c *Case) error {
	if len(c.Expect.PackContains) == 0 && len(c.Expect.PackExcludes) == 0 && len(c.Expect.Related) == 0 {
		return nil
	}
	if r.Loop == nil || r.Loop.Packs == nil {
		return errors.New("pack builder required")
	}
	pack := r.Loop.Packs.Build(r.Loop.Catalog, []runctx.Turn{{Role: "user", Content: c.Input}}, nil, r.Loop.Semantics, nil)
	text := pack.Index + "\n" + pack.Serialize()
	for _, s := range c.Expect.PackContains {
		if !strings.Contains(text, s) {
			return fmt.Errorf("pack missing %q", s)
		}
	}
	for _, s := range c.Expect.PackExcludes {
		if strings.Contains(text, s) {
			return fmt.Errorf("pack contains %q", s)
		}
	}
	for _, id := range c.Expect.Related {
		if !strings.Contains(pack.Index, id) {
			return fmt.Errorf("pack missing related %q", id)
		}
	}
	return nil
}

func (g *httpGate) InvokeHTTPResult(context.Context, *catalog.Operation, map[string]string) (result.HTTPResult, error) {
	g.hits++
	return result.HTTPResult{}, errors.New("http ran")
}

func decodeStrict(data []byte, out any) error {
	return yamlfile.Decode(data, out)
}
