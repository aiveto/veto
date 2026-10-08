package main

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/aiveto/veto/agentmeta"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/config"
	"github.com/aiveto/veto/eval"
	"github.com/aiveto/veto/flow"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var errNotInRef = errors.New("path is not in the git ref")

type (
	baseline struct {
		Operations    map[string]catalog.OpFact
		Cases         []eval.CaseExpect
		Confirmations map[string]*bool
		Tasks         []string
	}

	snapshotFile struct {
		Operations    map[string]catalog.OpFact `json:"operations"`
		Cases         []eval.CaseExpect         `json:"cases"`
		Confirmations map[string]*bool          `json:"confirmations,omitempty"`
		Tasks         []string                  `json:"tasks,omitempty"`
	}

	checkCmd struct {
		evalCmd
		against string
		bundle  string
	}
)

func newCheckCommand() *cobra.Command {
	cmd := &checkCmd{}
	c := &cobra.Command{
		Use:   "check",
		Short: "Load the catalog, print joins, and run eval cases.",
		Run: func(*cobra.Command, []string) {
			runCheck(*cmd)
		},
	}
	c.Flags().StringArrayVar(&cmd.contract, "contract", nil, "OpenAPI file. Repeat to register another API. Overrides config.")
	c.Flags().StringArrayVar(&cmd.cases, "case", nil, "Eval case file or directory. Repeat to add another. Defaults to cases in the bundle.")
	c.Flags().StringVar(&cmd.config, "config", "", "Path to veto.yaml provider keys.")
	c.Flags().StringVar(&cmd.bundle, "bundle", "", "Directory or zip of contracts, relations, and check cases.")
	c.Flags().StringVar(&cmd.agent, "agent", "", "Path to agent.yaml. Overrides agent_file.")
	c.Flags().StringVar(&cmd.relations, "relations", "", "Relations file. Overrides relations_file.")
	c.Flags().StringVar(&cmd.baseURL, "base-url", "", "Override the server URL on every operation. Empty uses each contract server.")
	c.Flags().StringVar(&cmd.against, "against", "", "Git ref or snapshot JSON. Fail if a joined operation disappeared, confirmation was dropped without an agent.yaml change or confirmation: false, a new destructive operation appeared, an eval expectation changed, or a task lost a binding or an answer field.")
	return c
}

func runCheck(cmd checkCmd) {
	if err := runChecked(cmd); err != nil {
		releaseBundles()
		fmt.Fprintf(os.Stderr, "check: %v\n", err)
		exitMain(1)
	}
}

func runChecked(cmd checkCmd) error {
	loop, cfg, err := buildLoopBundle(cmd.contract, cmd.config, cmd.bundle, cmd.agent, cmd.relations, cmd.baseURL)
	if err != nil {
		return err
	}
	fmt.Printf("ok: %s (%d operations)\n", loop.Catalog.Title, len(loop.Catalog.Operations))
	if line := confirmationNotice(cfg); line != "" {
		fmt.Println(line)
	}
	for _, line := range loop.Catalog.Joins() {
		fmt.Println(line)
	}
	for _, line := range flow.Lines(loop.Flows) {
		fmt.Println(line)
	}
	if len(cmd.cases) == 0 {
		cmd.cases = cfg.Cases
	}
	if len(cmd.cases) == 0 {
		return errors.New("case required")
	}
	if cmd.against != "" {
		if err := diffAgainst(cmd, loop.Catalog, flow.Lines(loop.Flows)); err != nil {
			return err
		}
	}
	return runCases(loop, cmd.cases)
}

func diffAgainst(cmd checkCmd, cat *catalog.Catalog, tasks []string) error {
	base, err := loadBaseline(cmd)
	if err != nil {
		return err
	}
	src, err := resolveBundle(cmd.config, cmd.bundle, cmd.contract, cmd.relations, cmd.agent)
	if err != nil {
		return err
	}
	agentPath := src.agent
	curAgent := agentmeta.File{}
	if agentPath != "" {
		curAgent, err = agentmeta.Load(agentPath)
		if err != nil {
			return err
		}
	}
	cases, err := eval.LoadCases(cmd.cases)
	if err != nil {
		return err
	}
	lines := catalog.SurfaceRegressions(base.Operations, catalog.Facts(cat), agentmeta.ChangedConfirmations(base.Confirmations, agentmeta.Confirmations(curAgent)), !src.cfg.Confirms())
	lines = append(lines, eval.Drift(base.Cases, eval.Expects(cases))...)
	lines = append(lines, flow.TaskRegressions(base.Tasks, tasks)...)
	lines = withStages(lines)
	if len(lines) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(lines, "\n"))
}

func loadBaseline(cmd checkCmd) (baseline, error) {
	info, err := os.Stat(cmd.against)
	if err == nil && info.Mode().IsRegular() {
		return readSnapshot(cmd.against)
	}
	return baselineFromGit(cmd)
}

func readSnapshot(path string) (baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return baseline{}, fmt.Errorf("read snapshot: %w", err)
	}
	var file snapshotFile
	if err := jsonv2.Unmarshal(data, &file); err != nil {
		return baseline{}, fmt.Errorf("parse snapshot: %w", err)
	}
	if file.Operations == nil {
		file.Operations = map[string]catalog.OpFact{}
	}
	return baseline(file), nil
}

func baselineFromGit(cmd checkCmd) (baseline, error) {
	start := cmd.config
	if start == "" && len(cmd.contract) > 0 {
		start = cmd.contract[0]
	}
	if start == "" {
		start = "."
	}
	root, err := gitRoot(filepath.Dir(start))
	if err != nil {
		return baseline{}, err
	}
	if cmd.config != "" {
		return baselineFromConfig(root, cmd.against, cmd.config, cmd.cases)
	}
	return baselineFromPaths(root, cmd.against, cmd.contract, cmd.relations, cmd.agent, cmd.cases)
}

func baselineFromConfig(root, ref, configPath string, casePaths []string) (base baseline, err error) {
	cfgRel, err := repoRel(root, configPath)
	if err != nil {
		return baseline{}, err
	}
	raw, err := gitShow(root, ref, cfgRel)
	if err != nil {
		return baseline{}, err
	}
	tmp, err := os.MkdirTemp("", "veto-against-")
	if err != nil {
		return baseline{}, err
	}
	defer func() {
		if rerr := os.RemoveAll(tmp); rerr != nil {
			err = errors.Join(err, fmt.Errorf("remove temp: %w", rerr))
		}
	}()
	if err := writeRepoFile(tmp, cfgRel, raw); err != nil {
		return baseline{}, err
	}
	var stub struct {
		Contracts []string `yaml:"contracts"`
		Relations string   `yaml:"relations_file"`
		Agent     string   `yaml:"agent_file"`
		Semantics string   `yaml:"semantics_file"`
		Flow      string   `yaml:"flow_file"`
	}
	if err := yaml.Unmarshal(raw, &stub); err != nil {
		return baseline{}, fmt.Errorf("parse config at %s: %w", ref, err)
	}
	names := append(append([]string{}, stub.Contracts...), stub.Relations, stub.Agent, stub.Semantics, stub.Flow)
	for _, name := range names {
		if name == "" || filepath.IsAbs(name) {
			continue
		}
		rel := path.Clean(path.Join(path.Dir(cfgRel), filepath.ToSlash(name)))
		data, err := gitShow(root, ref, rel)
		if err != nil {
			return baseline{}, err
		}
		if err := writeRepoFile(tmp, rel, data); err != nil {
			return baseline{}, err
		}
	}
	cfg, err := loadConfigAt(filepath.Join(tmp, filepath.FromSlash(cfgRel)))
	if err != nil {
		return baseline{}, err
	}
	cat, err := loadCatalog(cfg.Contracts, cfg.RelationsFile)
	if err != nil {
		return baseline{}, err
	}
	if err := applyAgent(cat, cfg.AgentFile); err != nil {
		return baseline{}, err
	}
	applyDeployment(cat, cfg)
	conf := map[string]*bool{}
	if cfg.AgentFile != "" {
		agentFile, err := agentmeta.Load(cfg.AgentFile)
		if err != nil {
			return baseline{}, err
		}
		conf = agentmeta.Confirmations(agentFile)
	}
	cases, err := casesAtRef(root, ref, casePaths)
	if err != nil {
		return baseline{}, err
	}
	tasks, err := taskLines(cfg, cat)
	if err != nil {
		return baseline{}, err
	}
	return baseline{Operations: catalog.Facts(cat), Cases: cases, Confirmations: conf, Tasks: tasks}, nil
}

func baselineFromPaths(root, ref string, contracts []string, relations, agentPath string, casePaths []string) (base baseline, err error) {
	tmp, err := os.MkdirTemp("", "veto-against-")
	if err != nil {
		return baseline{}, err
	}
	defer func() {
		if rerr := os.RemoveAll(tmp); rerr != nil {
			err = errors.Join(err, fmt.Errorf("remove temp: %w", rerr))
		}
	}()
	var contractPaths []string
	for i, abs := range contracts {
		data, err := showIfPresent(root, ref, abs)
		if errors.Is(err, errNotInRef) {
			continue
		}
		if err != nil {
			return baseline{}, err
		}
		name := fmt.Sprintf("contract-%d.yaml", i)
		if err := os.WriteFile(filepath.Join(tmp, name), data, 0o600); err != nil {
			return baseline{}, err
		}
		contractPaths = append(contractPaths, filepath.Join(tmp, name))
	}
	if len(contractPaths) == 0 {
		return baseline{}, fmt.Errorf("no contracts at %s", ref)
	}
	relPath := ""
	if relations != "" {
		data, err := showIfPresent(root, ref, relations)
		if errors.Is(err, errNotInRef) {
			data = nil
			err = nil
		}
		if err != nil {
			return baseline{}, err
		}
		if data != nil {
			relPath = filepath.Join(tmp, "relations.yaml")
			if err := os.WriteFile(relPath, data, 0o600); err != nil {
				return baseline{}, err
			}
		}
	}
	cat, err := loadCatalog(contractPaths, relPath)
	if err != nil {
		return baseline{}, err
	}
	conf := map[string]*bool{}
	if agentPath != "" {
		data, err := showIfPresent(root, ref, agentPath)
		if errors.Is(err, errNotInRef) {
			data = nil
			err = nil
		}
		if err != nil {
			return baseline{}, err
		}
		if data != nil {
			agentFilePath := filepath.Join(tmp, "agent.yaml")
			if err := os.WriteFile(agentFilePath, data, 0o600); err != nil {
				return baseline{}, err
			}
			if err := applyAgent(cat, agentFilePath); err != nil {
				return baseline{}, err
			}
			agentFile, err := agentmeta.Load(agentFilePath)
			if err != nil {
				return baseline{}, err
			}
			conf = agentmeta.Confirmations(agentFile)
		}
	}
	cases, err := casesAtRef(root, ref, casePaths)
	if err != nil {
		return baseline{}, err
	}
	return baseline{Operations: catalog.Facts(cat), Cases: cases, Confirmations: conf}, nil
}

func loadConfigAt(configPath string) (config.File, error) {
	src, err := resolve(configPath, nil, "", "")
	if err != nil {
		return config.File{}, err
	}
	return src.cfg, nil
}

func taskLines(cfg config.File, cat *catalog.Catalog) ([]string, error) {
	flows, err := loadFlows(cfg, cat)
	if err != nil {
		return nil, err
	}
	return flow.Lines(flows), nil
}

func casesAtRef(root, ref string, paths []string) (out []eval.CaseExpect, err error) {
	if len(paths) == 0 {
		return nil, nil
	}
	tmp, err := os.MkdirTemp("", "veto-against-cases-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if rerr := os.RemoveAll(tmp); rerr != nil {
			err = errors.Join(err, fmt.Errorf("remove temp: %w", rerr))
		}
	}()
	var files []string
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("read case: %w", err)
		}
		rel, err := repoRel(root, abs)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			names, err := gitList(root, ref, rel)
			if err != nil {
				return nil, err
			}
			for _, name := range names {
				if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
					continue
				}
				data, err := gitShow(root, ref, name)
				if err != nil {
					return nil, err
				}
				dest := filepath.Join(tmp, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
					return nil, err
				}
				if err := os.WriteFile(dest, data, 0o600); err != nil {
					return nil, err
				}
				files = append(files, dest)
			}
			continue
		}
		data, err := gitShow(root, ref, rel)
		if errors.Is(err, errNotInRef) {
			continue
		}
		if err != nil {
			return nil, err
		}
		dest := filepath.Join(tmp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, data, 0o600); err != nil {
			return nil, err
		}
		files = append(files, dest)
	}
	if len(files) == 0 {
		return nil, nil
	}
	cases, err := eval.LoadCases(files)
	if err != nil {
		return nil, err
	}
	return eval.Expects(cases), nil
}

func showIfPresent(root, ref, abs string) ([]byte, error) {
	rel, err := repoRel(root, abs)
	if err != nil {
		return nil, err
	}
	return gitShow(root, ref, rel)
}

func writeRepoFile(tmp, rel string, data []byte) error {
	dest := filepath.Join(tmp, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0o600)
}

func gitRoot(dir string) (string, error) {
	out, err := gitOutput(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func repoRel(root, abs string) (string, error) {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	abs, err = filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside %s", abs, root)
	}
	return filepath.ToSlash(rel), nil
}

func gitShow(root, ref, rel string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), "git", "-C", root, "show", ref+":"+rel)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if strings.Contains(msg, "does not exist") || strings.Contains(msg, "exists on disk, but not") {
			return nil, errNotInRef
		}
		text := strings.TrimSpace(msg)
		if text == "" {
			text = err.Error()
		}
		return nil, fmt.Errorf("git show %s:%s: %s", ref, rel, text)
	}
	return stdout.Bytes(), nil
}

func gitList(root, ref, rel string) ([]string, error) {
	cmd := exec.CommandContext(context.Background(), "git", "-C", root, "ls-tree", "-r", "--name-only", ref, "--", rel)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git ls-tree %s %s: %s", ref, rel, strings.TrimSpace(stderr.String()))
	}
	var out []string
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out, nil
}

func gitOutput(dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
