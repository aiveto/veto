package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/aiveto/veto/agentmeta"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/flow"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/runtime"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newInitCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "init [contract...]",
		Short: "Write veto.yaml and guide the first read.",
		Args:  cobra.MinimumNArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			notes, err := writeStarter(".", args)
			if err != nil {
				fmt.Fprintf(os.Stderr, "init: %v\n", err)
				exitMain(1)
			}
			for _, line := range notes {
				fmt.Println(line)
			}
		},
	}
	return c
}

func writeStarter(dir string, contracts []string) ([]string, error) {
	if len(contracts) == 0 {
		return nil, errors.New("contract required")
	}
	configPath := filepath.Join(dir, "veto.yaml")
	if _, err := os.Stat(configPath); err == nil {
		return nil, errors.New("veto.yaml already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read veto.yaml: %w", err)
	}
	if err := writeIfMissing(filepath.Join(dir, "relations.yaml"), []byte("relations: []\n")); err != nil {
		return nil, err
	}
	cat, def, err := suggestedTask(dir, contracts)
	if err != nil {
		return nil, err
	}
	flowFile, err := writeSuggestedFlow(dir, def)
	if err != nil {
		return nil, err
	}
	agentFile, err := writeSuggestedAgent(dir, def)
	if err != nil {
		return nil, err
	}
	configBody, err := starterYAML(contracts, flowFile, agentFile, authEnv(cat, def))
	if err != nil {
		return nil, err
	}
	if err := writeNew(configPath, configBody); err != nil {
		return nil, err
	}
	return guideRead(configPath, def)
}

func starterYAML(contracts []string, flowFile, agentFile string, auth map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	err := enc.Encode(struct {
		Contracts     []string          `yaml:"contracts"`
		RelationsFile string            `yaml:"relations_file"`
		FlowFile      string            `yaml:"flow_file,omitempty"`
		AgentFile     string            `yaml:"agent_file,omitempty"`
		Auth          map[string]string `yaml:"auth,omitempty"`
	}{
		Contracts:     contracts,
		RelationsFile: "relations.yaml",
		FlowFile:      flowFile,
		AgentFile:     agentFile,
		Auth:          auth,
	})
	if err != nil {
		return nil, fmt.Errorf("write veto.yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("write veto.yaml: %w", err)
	}
	return buf.Bytes(), nil
}

func suggestedTask(dir string, contracts []string) (*catalog.Catalog, *flow.Definition, error) {
	cat, ok := starterCatalog(dir, contracts)
	if !ok {
		return nil, nil, nil
	}
	if data, err := os.ReadFile(filepath.Join(dir, "relations.yaml")); err == nil {
		rels, err := catalog.ParseRelations(data)
		if err != nil {
			return nil, nil, err
		}
		if err := catalog.ApplyRelations(cat, rels); err != nil {
			return nil, nil, err
		}
	}
	return cat, flow.Suggest(cat), nil
}

func writeSuggestedFlow(dir string, def *flow.Definition) (string, error) {
	if def == nil {
		return "", nil
	}
	body, err := flow.Format([]*flow.Definition{def})
	if err != nil {
		return "", fmt.Errorf("write flow.yaml: %w", err)
	}
	path := filepath.Join(dir, "flow.yaml")
	if _, statErr := os.Stat(path); statErr != nil {
		if !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		if err := writeNew(path, body); err != nil {
			return "", err
		}
		return "flow.yaml", nil
	}
	return "", nil
}

func writeSuggestedAgent(dir string, def *flow.Definition) (string, error) {
	ops := agentOps(def)
	if len(ops) == 0 {
		return "", nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(agentmeta.File{Operations: ops}); err != nil {
		return "", fmt.Errorf("write agent.yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("write agent.yaml: %w", err)
	}
	path := filepath.Join(dir, "agent.yaml")
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}
	if err := writeNew(path, buf.Bytes()); err != nil {
		return "", err
	}
	return "agent.yaml", nil
}

func agentOps(def *flow.Definition) []agentmeta.Entry {
	if def == nil {
		return nil
	}
	direct := catalog.ExposureDirect
	seen := map[string]bool{}
	var ops []agentmeta.Entry
	for _, step := range def.Steps {
		if step.Operation == "" || seen[step.Operation] {
			continue
		}
		seen[step.Operation] = true
		ops = append(ops, agentmeta.Entry{Operation: step.Operation, Exposure: &direct})
	}
	return ops
}

func authEnv(cat *catalog.Catalog, def *flow.Definition) map[string]string {
	schemes := taskSchemes(cat, def)
	if len(schemes) == 0 {
		return nil
	}
	out := make(map[string]string, len(schemes))
	for _, scheme := range schemes {
		out[scheme.Name] = envName(scheme.Name)
	}
	return out
}

func taskSchemes(cat *catalog.Catalog, def *flow.Definition) []catalog.Auth {
	if cat == nil || def == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []catalog.Auth
	for _, step := range def.Steps {
		op := cat.ByID(step.Operation)
		if op == nil {
			continue
		}
		for _, scheme := range op.AuthSchemes() {
			if scheme.Name == "" || seen[scheme.Name] {
				continue
			}
			seen[scheme.Name] = true
			out = append(out, scheme)
		}
	}
	slices.SortFunc(out, func(a, b catalog.Auth) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

func envName(scheme string) string {
	var b strings.Builder
	prevLower := false
	for _, r := range scheme {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteByte(byte(r - 'a' + 'A'))
			prevLower = true
		case r >= 'A' && r <= 'Z':
			if prevLower {
				b.WriteByte('_')
			}
			b.WriteByte(byte(r))
			prevLower = false
		case r >= '0' && r <= '9':
			b.WriteByte(byte(r))
			prevLower = false
		default:
			if b.Len() > 0 && b.String()[b.Len()-1] != '_' {
				b.WriteByte('_')
			}
			prevLower = false
		}
	}
	return strings.Trim(b.String(), "_")
}

func guideRead(configPath string, def *flow.Definition) ([]string, error) {
	if def == nil || len(def.Steps) == 0 {
		return nil, nil
	}
	srv, cfg, err := buildServer(nil, configPath, "", "", "")
	if err != nil {
		return nil, err
	}
	opID := def.Steps[0].Operation
	var notes []string
	blocked := false
	if op := srv.Catalog.ByID(opID); op != nil {
		creds := authResolver(cfg)
		for _, scheme := range op.AuthSchemes() {
			lines := creds.Blockers(scheme)
			notes = append(notes, lines...)
			if len(lines) > 0 {
				blocked = true
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	out, err := srv.Preview(ctx, runtime.Request{Operation: opID})
	if err != nil {
		return nil, err
	}
	if len(out.Errors) > 0 {
		for _, line := range out.Errors {
			notes = append(notes, "preview "+opID+": "+line)
		}
		return notes, nil
	}
	notes = append(notes, "preview "+opID+": ok")
	if blocked || srv.Calls == nil {
		return notes, nil
	}
	res, err := srv.Calls.Invoke(ctx, runtime.Request{Operation: opID})
	if err != nil {
		notes = append(notes, opID+": "+err.Error())
		return notes, nil
	}
	if res.Status != runtime.StatusOK {
		why := res.Why
		if why == "" {
			why = res.Status
		}
		notes = append(notes, opID+": "+why)
		return notes, nil
	}
	notes = append(notes, opID+": ok")
	return notes, nil
}

func starterCatalog(dir string, contracts []string) (*catalog.Catalog, bool) {
	parts := make([]*catalog.Catalog, 0, len(contracts))
	for _, name := range contracts {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, name)
		}
		cat, err := openapi.Load(context.Background(), path)
		if err != nil {
			return nil, false
		}
		parts = append(parts, cat)
	}
	cat, err := catalog.Merge(parts...)
	if err != nil {
		return nil, false
	}
	return cat, true
}

func writeIfMissing(path string, body []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	_, werr := f.Write(body)
	cerr := f.Close()
	if werr != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), werr)
	}
	if cerr != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), cerr)
	}
	return nil
}

func writeNew(path string, body []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s already exists", filepath.Base(path))
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	_, werr := f.Write(body)
	cerr := f.Close()
	if werr != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), werr)
	}
	if cerr != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), cerr)
	}
	return nil
}
