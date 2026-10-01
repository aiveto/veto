package eval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/result"
	"github.com/aiveto/veto/runctx"
	"gopkg.in/yaml.v3"
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
	if c.Expect.ConfirmationRequired && out.Status != "confirmation_required" {
		return fmt.Errorf("expected confirmation_required, got %q", out.Status)
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
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	var extra yaml.Node
	err := dec.Decode(&extra)
	if err == nil {
		return errors.New("extra document")
	}
	if !errors.Is(err, io.EOF) {
		return err
	}
	if err := rejectDup(&doc, map[*yaml.Node]struct{}{}); err != nil {
		return err
	}
	known := yaml.NewDecoder(bytes.NewReader(data))
	known.KnownFields(true)
	return known.Decode(out)
}

func rejectDup(n *yaml.Node, active map[*yaml.Node]struct{}) error {
	if n == nil {
		return nil
	}
	if _, seen := active[n]; seen {
		return errors.New("yaml alias cycle")
	}
	active[n] = struct{}{}
	defer delete(active, n)
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range n.Content {
			if err := rejectDup(child, active); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		keys := map[string]struct{}{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			if _, ok := keys[key]; ok {
				return fmt.Errorf("duplicate field %q", key)
			}
			keys[key] = struct{}{}
			if err := rejectDup(n.Content[i+1], active); err != nil {
				return err
			}
		}
	case yaml.AliasNode:
		return rejectDup(n.Alias, active)
	case yaml.ScalarNode:
		return nil
	}
	return nil
}
