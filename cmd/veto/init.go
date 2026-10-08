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

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/config"
	"github.com/aiveto/veto/flow"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/runtime"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type (
	agentLine struct {
		Operation string `yaml:"operation"`
		Exposure  string `yaml:"exposure,omitempty"`
	}

	agentDocument struct {
		Operations []agentLine `yaml:"operations"`
	}
)

func newInitCommand() *cobra.Command {
	c := &cobra.Command{
		Use:     "init <contract> [contract...]",
		Short:   "Write veto.yaml from OpenAPI files.",
		Example: "  veto init orders.yaml\n  veto init orders.yaml customers.yaml",
		Args:    initArgs,
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

func initArgs(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return errors.New("OpenAPI file required, for example: veto init orders.yaml")
	}
	return nil
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
	cat, err := starterCatalog(dir, contracts)
	if err != nil {
		return nil, err
	}
	relationsPath := filepath.Join(dir, "relations.yaml")
	if err := writeIfMissing(relationsPath, []byte("relations: []\n")); err != nil {
		return nil, err
	}
	if err := applyStarterRelations(relationsPath, cat); err != nil {
		return nil, err
	}
	def := flow.SuggestIn(cat, firstOperationIDs(dir, contracts))
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
	notes, err := guideRead(configPath, def)
	if err != nil {
		return nil, err
	}
	return append(notes, starterNotes(cat, def)...), nil
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

func applyStarterRelations(path string, cat *catalog.Catalog) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rels, err := catalog.ParseRelations(data)
	if err != nil {
		return err
	}
	return catalog.ApplyRelations(cat, rels)
}

func firstOperationIDs(dir string, contracts []string) []string {
	if len(contracts) == 0 {
		return nil
	}
	path := contracts[0]
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	one, err := openapi.Load(context.Background(), path)
	if err != nil || one == nil {
		return nil
	}
	ids := make([]string, 0, len(one.Operations))
	for _, op := range one.Operations {
		ids = append(ids, op.ID)
	}
	return ids
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
	if err := enc.Encode(agentDocument{Operations: ops}); err != nil {
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

func agentOps(def *flow.Definition) []agentLine {
	if def == nil {
		return nil
	}
	seen := map[string]bool{}
	var ops []agentLine
	for _, step := range def.Steps {
		if step.Operation == "" || seen[step.Operation] {
			continue
		}
		seen[step.Operation] = true
		ops = append(ops, agentLine{Operation: step.Operation, Exposure: catalog.ExposureDirect})
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
	if blocked {
		notes = append(notes, exportLines(cfg)...)
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

func exportLines(cfg config.File) []string {
	var names []string
	for name, src := range cfg.Auth {
		if src.Env == "" || os.Getenv(src.Env) != "" {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)
	notes := make([]string, 0, len(names))
	for _, name := range names {
		notes = append(notes, "Export "+cfg.Auth[name].Env+" to the token "+name+" sends.")
	}
	return notes
}

func starterNotes(cat *catalog.Catalog, def *flow.Definition) []string {
	var notes []string
	if line := relationGap(cat); line != "" {
		notes = append(notes, line)
	}
	if def != nil && strings.TrimSpace(def.Question) != "" {
		notes = append(notes, "question: "+def.Question)
	}
	if line := runHint(cat, def); line != "" {
		notes = append(notes, line)
	}
	if line := invokeHint(cat, def); line != "" {
		notes = append(notes, line)
	}
	return notes
}

func invokeHint(cat *catalog.Catalog, def *flow.Definition) string {
	if def == nil || len(def.Steps) == 0 || def.Steps[0].Operation == "" {
		return ""
	}
	cmd := "veto invoke " + def.Steps[0].Operation
	if cat == nil {
		return cmd
	}
	if name := requiredParamName(cat.ByID(def.Steps[0].Operation)); name != "" {
		return cmd + " --param " + name + "="
	}
	return cmd
}

func runHint(cat *catalog.Catalog, def *flow.Definition) string {
	if def == nil || def.Name == "" {
		return ""
	}
	cmd := "veto run " + def.Name
	if cat == nil || len(def.Steps) == 0 {
		return cmd
	}
	if name := requiredParamName(cat.ByID(def.Steps[0].Operation)); name != "" {
		return cmd + " --param " + name + "="
	}
	return cmd
}

func requiredParamName(op *catalog.Operation) string {
	if op == nil {
		return ""
	}
	for _, p := range op.Params {
		if p.In != "path" && !p.Required {
			continue
		}
		if strings.TrimSpace(p.Default) != "" {
			continue
		}
		return p.Name
	}
	return ""
}

func starterCatalog(dir string, contracts []string) (*catalog.Catalog, error) {
	parts := make([]*catalog.Catalog, 0, len(contracts))
	for _, name := range contracts {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, name)
		}
		cat, err := openapi.Load(context.Background(), path)
		if err != nil {
			return nil, err
		}
		parts = append(parts, cat)
	}
	return catalog.Merge(parts...)
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
