package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/agent/openai"
	"github.com/aiveto/veto/agentmeta"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/config"
	"github.com/aiveto/veto/eval"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/flow"
	"github.com/aiveto/veto/generate"
	"github.com/aiveto/veto/mcpserver"
	"github.com/aiveto/veto/memory"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/replay"
	"github.com/aiveto/veto/semantics"
	"github.com/aiveto/veto/telemetry"
	"github.com/spf13/cobra"
)

type (
	validateCmd struct {
		config    string
		contract  []string
		agent     string
		relations string
	}

	serveCmd struct {
		contract   []string
		config     string
		agent      string
		relations  string
		stdio      bool
		pin        []string
		directPins bool
		grouped    bool
		baseURL    string
	}

	generateCmd struct {
		config    string
		contract  []string
		out       string
		module    string
		agent     string
		relations string
	}

	evalCmd struct {
		contract  []string
		casePath  string
		config    string
		agent     string
		relations string
		baseURL   string
	}

	replayCmd struct {
		contract      []string
		message       string
		config        string
		agent         string
		relations     string
		baseURL       string
		keepSensitive bool
	}
)

func main() {
	root := &cobra.Command{
		Use:          "veto",
		SilenceUsage: true,
	}
	root.AddCommand(
		newValidateCommand(),
		newServeCommand(),
		newEvalCommand(),
		newGenerateCommand(),
		newReplayCommand(),
	)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newValidateCommand() *cobra.Command {
	cmd := &validateCmd{}
	c := &cobra.Command{
		Use:   "validate",
		Short: "Load and validate an OpenAPI contract.",
		Run: func(*cobra.Command, []string) {
			runValidate(*cmd)
		},
	}
	c.Flags().StringVar(&cmd.config, "config", "", "Path to veto.yaml. Contracts and relations live here.")
	c.Flags().StringArrayVar(&cmd.contract, "contract", nil, "OpenAPI file. Repeat to register another API. Overrides config.")
	c.Flags().StringVar(&cmd.agent, "agent", "", "Path to agent.yaml.")
	c.Flags().StringVar(&cmd.relations, "relations", "", "Relations file. Joins a schema field to an operation.")
	return c
}

func newServeCommand() *cobra.Command {
	cmd := &serveCmd{stdio: true}
	c := &cobra.Command{
		Use:   "serve",
		Short: "Serve MCP over stdio from the contract catalog.",
		Run: func(*cobra.Command, []string) {
			runServe(*cmd)
		},
	}
	c.Flags().StringArrayVar(&cmd.contract, "contract", nil, "OpenAPI file. Repeat to register another API. Overrides config.")
	c.Flags().StringVar(&cmd.config, "config", "", "Path to veto.yaml provider keys.")
	c.Flags().StringVar(&cmd.agent, "agent", "", "Path to agent.yaml. Overrides agent_file.")
	c.Flags().StringVar(&cmd.relations, "relations", "", "Relations file. Overrides relations_file.")
	c.Flags().BoolVar(&cmd.stdio, "stdio", true, "Listen on stdio for MCP.")
	c.Flags().StringArrayVar(&cmd.pin, "pin", nil, "Pin operation ids.")
	c.Flags().BoolVar(&cmd.directPins, "direct-pins", false, "Register direct MCP tools for pinned ids only.")
	c.Flags().BoolVar(&cmd.grouped, "grouped", false, "Register one MCP tool per resource.")
	c.Flags().StringVar(&cmd.baseURL, "base-url", "", "Override the server URL on every operation. Empty uses each contract server.")
	return c
}

func newGenerateCommand() *cobra.Command {
	cmd := &generateCmd{}
	c := &cobra.Command{
		Use:   "generate",
		Short: "Write a typed SDK, CLI, and MCP dispatch.",
		Run: func(*cobra.Command, []string) {
			runGenerate(*cmd)
		},
	}
	c.Flags().StringVar(&cmd.config, "config", "", "Path to veto.yaml. Contracts and relations live here.")
	c.Flags().StringArrayVar(&cmd.contract, "contract", nil, "OpenAPI file. Repeat to register another API. Overrides config.")
	c.Flags().StringVar(&cmd.out, "out", "", "Directory to write the generated module.")
	c.Flags().StringVar(&cmd.module, "module", "", "Go module path for the generated module.")
	c.Flags().StringVar(&cmd.agent, "agent", "", "Path to agent.yaml.")
	c.Flags().StringVar(&cmd.relations, "relations", "", "Relations file.")
	_ = c.MarkFlagRequired("out")
	_ = c.MarkFlagRequired("module")
	return c
}

func newEvalCommand() *cobra.Command {
	cmd := &evalCmd{}
	c := &cobra.Command{
		Use:   "eval",
		Short: "Run a deterministic eval case.",
		Run: func(*cobra.Command, []string) {
			runEval(*cmd)
		},
	}
	c.Flags().StringArrayVar(&cmd.contract, "contract", nil, "OpenAPI file. Repeat to register another API. Overrides config.")
	c.Flags().StringVar(&cmd.casePath, "case", "", "Path to eval case yaml.")
	c.Flags().StringVar(&cmd.config, "config", "", "Path to veto.yaml provider keys.")
	c.Flags().StringVar(&cmd.agent, "agent", "", "Path to agent.yaml. Overrides agent_file.")
	c.Flags().StringVar(&cmd.relations, "relations", "", "Relations file. Overrides relations_file.")
	c.Flags().StringVar(&cmd.baseURL, "base-url", "", "Override the server URL on every operation. Empty uses each contract server.")
	_ = c.MarkFlagRequired("case")
	return c
}

func newReplayCommand() *cobra.Command {
	cmd := &replayCmd{}
	c := &cobra.Command{
		Use:   "replay",
		Short: "Run one message and print the recorded trace.",
		Run: func(*cobra.Command, []string) {
			runReplay(*cmd)
		},
	}
	c.Flags().StringArrayVar(&cmd.contract, "contract", nil, "OpenAPI file. Repeat to register another API. Overrides config.")
	c.Flags().StringVar(&cmd.message, "message", "", "User message to run.")
	c.Flags().StringVar(&cmd.config, "config", "", "Path to veto.yaml provider keys.")
	c.Flags().StringVar(&cmd.agent, "agent", "", "Path to agent.yaml. Overrides agent_file.")
	c.Flags().StringVar(&cmd.relations, "relations", "", "Relations file. Overrides relations_file.")
	c.Flags().StringVar(&cmd.baseURL, "base-url", "", "Override the server URL on every operation. Empty uses each contract server.")
	c.Flags().BoolVar(&cmd.keepSensitive, "keep-sensitive", false, "Keep user messages and parameter values in the trace.")
	_ = c.MarkFlagRequired("message")
	return c
}

func runValidate(cmd validateCmd) {
	_, contracts, relations, agent, err := resolve(cmd.config, cmd.contract, cmd.relations, cmd.agent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "validate failed: %v\n", err)
		os.Exit(1)
	}
	cat, err := loadCatalog(contracts, relations)
	if err != nil {
		fmt.Fprintf(os.Stderr, "validate failed: %v\n", err)
		os.Exit(1)
	}
	if err := applyAgent(cat, agent); err != nil {
		fmt.Fprintf(os.Stderr, "validate failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("ok: %s (%d operations)\n", cat.Title, len(cat.Operations))
}

func runGenerate(cmd generateCmd) {
	_, contracts, relations, agent, err := resolve(cmd.config, cmd.contract, cmd.relations, cmd.agent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		os.Exit(1)
	}
	cat, err := loadCatalog(contracts, relations)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		os.Exit(1)
	}
	if err := applyAgent(cat, agent); err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		os.Exit(1)
	}
	if err := generate.Write(cmd.out, cmd.module, cat); err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("ok: %s\n", cmd.out)
}

func runServe(cmd serveCmd) {
	loop, cfg, err := buildLoop(cmd.contract, cmd.config, cmd.agent, cmd.relations, cmd.baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		os.Exit(1)
	}
	stop, err := telemetry.Install(cfg.TraceExport)
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		os.Exit(1)
	}
	defer stop(context.Background())
	if err := mcpserver.ValidatePins(loop.Catalog, cmd.pin); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		os.Exit(1)
	}
	if !cmd.stdio {
		fmt.Fprintf(os.Stderr, "only --stdio is supported\n")
		os.Exit(1)
	}
	srv := &mcpserver.Server{
		Catalog:   loop.Catalog,
		Semantics: loop.Semantics,
		Agent:     loop,
	}
	opt := mcpserver.Options{Pins: cmd.pin, DirectPins: cmd.directPins, Grouped: cmd.grouped}
	if err := mcpserver.RunStdio(context.Background(), srv, opt); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		os.Exit(1)
	}
}

func runEval(cmd evalCmd) {
	loop, cfg, err := buildLoop(cmd.contract, cmd.config, cmd.agent, cmd.relations, cmd.baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval: %v\n", err)
		os.Exit(1)
	}
	stop, err := telemetry.Install(cfg.TraceExport)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval: %v\n", err)
		os.Exit(1)
	}
	defer stop(context.Background())
	c, err := eval.LoadCase(cmd.casePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval: %v\n", err)
		os.Exit(1)
	}
	r := &eval.Runner{Catalog: loop.Catalog, Semantics: loop.Semantics, Model: loop.Model, Loop: loop}
	if err := r.Run(context.Background(), c); err != nil {
		fmt.Fprintf(os.Stderr, "eval failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("ok: %s\n", c.Name)
}

func runReplay(cmd replayCmd) {
	rec, err := telemetry.Record()
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay: %v\n", err)
		os.Exit(1)
	}
	defer rec.Stop(context.Background())
	loop, cfg, err := buildLoop(cmd.contract, cmd.config, cmd.agent, cmd.relations, cmd.baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay: %v\n", err)
		os.Exit(1)
	}
	redact := cfg.Redact()
	if cmd.keepSensitive {
		redact = false
	}
	if !redact {
		if c, ok := loop.Exec.(execute.Client); ok {
			c.RecordBody = true
			loop.Exec = c
		}
	}
	if _, err := loop.Run(context.Background(), cmd.message); err != nil {
		fmt.Fprintf(os.Stderr, "replay: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(replay.FromSpans(rec.Spans(), redact).String())
}

func applyAgent(cat *catalog.Catalog, path string) error {
	if path == "" {
		return nil
	}
	f, err := agentmeta.Load(path)
	if err != nil {
		return err
	}
	return agentmeta.Apply(cat, f)
}

func loadCatalog(contracts []string, relationsPath string) (*catalog.Catalog, error) {
	if len(contracts) == 0 {
		return nil, fmt.Errorf("contract required")
	}
	parts := make([]*catalog.Catalog, 0, len(contracts))
	for _, path := range contracts {
		cat, err := openapi.Load(context.Background(), path)
		if err != nil {
			return nil, err
		}
		parts = append(parts, cat)
	}
	cat, err := catalog.Merge(parts...)
	if err != nil {
		return nil, err
	}
	if relationsPath == "" {
		return cat, nil
	}
	rels, err := catalog.LoadRelations(relationsPath)
	if err != nil {
		return nil, err
	}
	if err := catalog.ApplyRelations(cat, rels); err != nil {
		return nil, err
	}
	return cat, nil
}

func resolve(configPath string, contracts []string, relations, agent string) (config.File, []string, string, string, error) {
	cfg := config.Defaults()
	if configPath != "" {
		loaded, err := config.Load(configPath)
		if err != nil {
			return config.File{}, nil, "", "", err
		}
		cfg = loaded
	}
	if len(contracts) == 0 {
		contracts = cfg.Contracts
	}
	if relations == "" {
		relations = cfg.RelationsFile
	}
	if agent == "" {
		agent = cfg.AgentFile
	}
	return cfg, contracts, relations, agent, nil
}

func buildLoop(contracts []string, configPath, agentPath, relationsPath, baseURL string) (*agent.Loop, config.File, error) {
	cfg, contracts, relationsPath, agentPath, err := resolve(configPath, contracts, relationsPath, agentPath)
	if err != nil {
		return nil, config.File{}, err
	}
	cat, err := loadCatalog(contracts, relationsPath)
	if err != nil {
		return nil, config.File{}, err
	}
	if agentPath == "" {
		agentPath = cfg.AgentFile
	}
	if err := applyAgent(cat, agentPath); err != nil {
		return nil, config.File{}, err
	}
	var sem semantics.Provider = semantics.NewDerived(cat)
	if cfg.SemanticsFile != "" {
		over, err := semantics.LoadOverlay(cfg.SemanticsFile, sem)
		if err != nil {
			return nil, config.File{}, err
		}
		sem = over
	}
	flows := map[string]*flow.Definition{}
	if cfg.FlowFile != "" {
		def, err := flow.Load(cfg.FlowFile)
		if err != nil {
			return nil, config.File{}, err
		}
		flows[def.Name] = def
	}
	loop := agent.New(cat, sem, execute.Client{
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: cfg.Timeout},
	})
	loop.Flows = flows
	if err := applyProviders(loop, cfg); err != nil {
		return nil, cfg, err
	}
	return loop, cfg, nil
}

// applyProviders constructs the providers named in the config. The accepted values are the defaults.
func applyProviders(loop *agent.Loop, cfg config.File) error {
	switch cfg.Memory {
	case "local":
		loop.Memory = memory.NewLocalMap()
	default:
		return fmt.Errorf("memory provider %q is not in this slice", cfg.Memory)
	}
	switch cfg.Policy {
	case "builtin":
		loop.Policy = policy.Builtin{}
	default:
		return fmt.Errorf("policy provider %q is not in this slice", cfg.Policy)
	}
	switch cfg.Model {
	case "scripted":
		loop.Model = agent.NewScripted()
	case "openai":
		live, err := openai.New(cfg.ModelBaseURL, os.Getenv("OPENAI_API_KEY"), cfg.ModelName)
		if err != nil {
			return err
		}
		loop.Model = live
	default:
		return fmt.Errorf("model provider %q is not in this slice", cfg.Model)
	}
	return nil
}
