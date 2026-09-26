package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/agentmeta"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/config"
	"github.com/aiveto/veto/eval"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/flow"
	"github.com/aiveto/veto/generate"
	"github.com/aiveto/veto/mcpserver"
	"github.com/aiveto/veto/model"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/replay"
	"github.com/aiveto/veto/semantics"
	"github.com/aiveto/veto/telemetry"
	"github.com/alecthomas/kong"
)

type CLI struct {
	Validate ValidateCmd `cmd:"validate" help:"Load and validate an OpenAPI contract."`
	Serve    ServeCmd    `cmd:"serve" help:"Serve MCP over stdio from the contract catalog."`
	Eval     EvalCmd     `cmd:"eval" help:"Run a deterministic eval case."`
	Generate GenerateCmd `cmd:"generate" help:"Write a typed SDK, CLI, and MCP dispatch."`
	Replay   ReplayCmd   `cmd:"replay" help:"Run one message and print the recorded trace."`
}

type ValidateCmd struct {
	Config    string   `help:"Path to veto.yaml. Contracts and relations live here." name:"config"`
	Contract  []string `help:"OpenAPI file. Repeat to register another API. Overrides config." name:"contract"`
	Agent     string   `help:"Path to agent.yaml." name:"agent"`
	Relations string   `help:"Relations file. Joins a schema field to an operation." name:"relations"`
}

type ServeCmd struct {
	Contract   []string `help:"OpenAPI file. Repeat to register another API. Overrides config." name:"contract"`
	Config     string   `help:"Path to veto.yaml provider keys." name:"config"`
	Agent      string   `help:"Path to agent.yaml. Overrides agent_file." name:"agent"`
	Relations  string   `help:"Relations file. Overrides relations_file." name:"relations"`
	Stdio      bool     `help:"Listen on stdio for MCP." default:"true"`
	Pin        []string `help:"Pin operation ids."`
	DirectPins bool     `help:"Register direct MCP tools for pinned ids only."`
	Grouped    bool     `help:"Register one MCP tool per resource."`
	BaseURL    string   `help:"Override the server URL on every operation. Empty uses each contract server."`
}

type GenerateCmd struct {
	Config    string   `help:"Path to veto.yaml. Contracts and relations live here." name:"config"`
	Contract  []string `help:"OpenAPI file. Repeat to register another API. Overrides config." name:"contract"`
	Out       string   `required:"" help:"Directory to write the generated module." name:"out"`
	Module    string   `required:"" help:"Go module path for the generated module." name:"module"`
	Agent     string   `help:"Path to agent.yaml." name:"agent"`
	Relations string   `help:"Relations file." name:"relations"`
}

type EvalCmd struct {
	Contract  []string `help:"OpenAPI file. Repeat to register another API. Overrides config." name:"contract"`
	Case      string   `required:"" help:"Path to eval case yaml." name:"case"`
	Config    string   `help:"Path to veto.yaml provider keys." name:"config"`
	Agent     string   `help:"Path to agent.yaml. Overrides agent_file." name:"agent"`
	Relations string   `help:"Relations file. Overrides relations_file." name:"relations"`
	BaseURL   string   `help:"Override the server URL on every operation. Empty uses each contract server."`
}

type ReplayCmd struct {
	Contract      []string `help:"OpenAPI file. Repeat to register another API. Overrides config." name:"contract"`
	Message       string   `required:"" help:"User message to run." name:"message"`
	Config        string   `help:"Path to veto.yaml provider keys." name:"config"`
	Agent         string   `help:"Path to agent.yaml. Overrides agent_file." name:"agent"`
	Relations     string   `help:"Relations file. Overrides relations_file." name:"relations"`
	BaseURL       string   `help:"Override the server URL on every operation. Empty uses each contract server."`
	KeepSensitive bool     `help:"Keep user messages and parameter values in the trace." name:"keep-sensitive"`
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli, kong.Name("veto"))
	switch ctx.Command() {
	case "validate":
		runValidate(cli.Validate)
	case "serve":
		runServe(cli.Serve)
	case "eval":
		runEval(cli.Eval)
	case "generate":
		runGenerate(cli.Generate)
	case "replay":
		runReplay(cli.Replay)
	default:
		ctx.FatalIfErrorf(fmt.Errorf("unknown command"))
	}
}

func runValidate(cmd ValidateCmd) {
	_, contracts, relations, agent, err := resolve(cmd.Config, cmd.Contract, cmd.Relations, cmd.Agent)
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

func runGenerate(cmd GenerateCmd) {
	_, contracts, relations, agent, err := resolve(cmd.Config, cmd.Contract, cmd.Relations, cmd.Agent)
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
	if err := generate.Write(cmd.Out, cmd.Module, cat); err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("ok: %s\n", cmd.Out)
}

func runServe(cmd ServeCmd) {
	loop, cfg, err := buildLoop(cmd.Contract, cmd.Config, cmd.Agent, cmd.Relations, cmd.BaseURL)
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
	if err := mcpserver.ValidatePins(loop.Catalog, cmd.Pin); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		os.Exit(1)
	}
	if !cmd.Stdio {
		fmt.Fprintf(os.Stderr, "only --stdio is supported\n")
		os.Exit(1)
	}
	srv := &mcpserver.Server{
		Catalog:   loop.Catalog,
		Semantics: loop.Semantics,
		Agent:     loop,
	}
	opt := mcpserver.Options{Pins: cmd.Pin, DirectPins: cmd.DirectPins, Grouped: cmd.Grouped}
	if err := mcpserver.RunStdio(context.Background(), srv, opt); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		os.Exit(1)
	}
}

func runEval(cmd EvalCmd) {
	loop, cfg, err := buildLoop(cmd.Contract, cmd.Config, cmd.Agent, cmd.Relations, cmd.BaseURL)
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
	c, err := eval.LoadCase(cmd.Case)
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

func runReplay(cmd ReplayCmd) {
	rec, err := telemetry.Record()
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay: %v\n", err)
		os.Exit(1)
	}
	defer rec.Stop(context.Background())
	loop, cfg, err := buildLoop(cmd.Contract, cmd.Config, cmd.Agent, cmd.Relations, cmd.BaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay: %v\n", err)
		os.Exit(1)
	}
	if _, err := loop.Run(context.Background(), cmd.Message); err != nil {
		fmt.Fprintf(os.Stderr, "replay: %v\n", err)
		os.Exit(1)
	}
	redact := cfg.Redact()
	if cmd.KeepSensitive {
		redact = false
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
	loop := agent.New(cat, sem, execute.Client{BaseURL: baseURL})
	loop.Flows = flows
	if cfg.Model == "openai" {
		live, err := model.NewOpenAI("", os.Getenv("OPENAI_API_KEY"), cfg.ModelName)
		if err != nil {
			return nil, cfg, err
		}
		loop.Model = live
	}
	return loop, cfg, nil
}
