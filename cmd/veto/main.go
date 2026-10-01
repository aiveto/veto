package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/agent/openai"
	"github.com/aiveto/veto/agentmeta"
	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/bundle"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/config"
	"github.com/aiveto/veto/eval"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/flow"
	"github.com/aiveto/veto/generate"
	"github.com/aiveto/veto/mcpserver"
	"github.com/aiveto/veto/memory"
	"github.com/aiveto/veto/opa"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/replay"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/runtime"
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
		http       bool
		addr       string
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
		cases     []string
		config    string
		agent     string
		relations string
		baseURL   string
	}

	packCmd struct {
		contract  []string
		config    string
		agent     string
		relations string
		message   string
		asJSON    bool
	}

	replayCmd struct {
		contract      []string
		message       string
		config        string
		agent         string
		relations     string
		baseURL       string
		keepSensitive bool
		from          string
	}
)

func main() {
	root, err := newRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "veto: %v\n", err)
		exitMain(1)
	}
	handled, err := jsonHelp(os.Stdout, root, os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "help: %v\n", err)
		exitMain(1)
	}
	if handled {
		exitMain(0)
	}
	if err := root.Execute(); err != nil {
		exitMain(1)
	}
	exitMain(0)
}

func exitMain(code int) {
	if err := bundle.Release(); err != nil {
		fmt.Fprintf(os.Stderr, "bundle: %v\n", err)
	}
	os.Exit(code)
}

func newRoot() (*cobra.Command, error) {
	evalCmd, err := newEvalCommand()
	if err != nil {
		return nil, err
	}
	generateCmd, err := newGenerateCommand()
	if err != nil {
		return nil, err
	}
	packCmd, err := newPackCommand()
	if err != nil {
		return nil, err
	}
	checkCmd := newCheckCommand()
	root := &cobra.Command{
		Use:          "veto",
		SilenceUsage: true,
	}
	root.AddCommand(
		newValidateCommand(),
		newInitCommand(),
		newServeCommand(),
		evalCmd,
		generateCmd,
		newReplayCommand(),
		packCmd,
		newDoctorCommand(),
		newPreviewCommand(),
		checkCmd,
		newAuthCommand(),
		newApproveCommand(),
	)
	return root, nil
}

type previewCmd struct {
	contract  []string
	config    string
	agent     string
	relations string
	baseURL   string
	operation string
	param     []string
	caller    string
}

func newPreviewCommand() *cobra.Command {
	cmd := &previewCmd{}
	c := &cobra.Command{
		Use:   "preview",
		Short: "Resolve, validate, and check policy without upstream HTTP.",
		Run: func(*cobra.Command, []string) {
			runPreview(*cmd)
		},
	}
	c.Flags().StringArrayVar(&cmd.contract, "contract", nil, "OpenAPI file. Repeat to register another API. Overrides config.")
	c.Flags().StringVar(&cmd.config, "config", "", "Path to veto.yaml provider keys.")
	c.Flags().StringVar(&cmd.agent, "agent", "", "Path to agent.yaml. Overrides agent_file.")
	c.Flags().StringVar(&cmd.relations, "relations", "", "Relations file. Overrides relations_file.")
	c.Flags().StringVar(&cmd.baseURL, "base-url", "", "Override the server URL on every operation.")
	c.Flags().StringVar(&cmd.operation, "operation", "", "Operation id to preview.")
	c.Flags().StringArrayVar(&cmd.param, "param", nil, "Parameter as key=value. Repeat for another parameter.")
	c.Flags().StringVar(&cmd.caller, "caller", "", "Caller or tenant passed to policy.")
	return c
}

func runPreview(cmd previewCmd) {
	if cmd.operation == "" {
		fmt.Fprintf(os.Stderr, "preview: operation required\n")
		os.Exit(1)
	}
	loop, _, err := buildLoop(cmd.contract, cmd.config, cmd.agent, cmd.relations, cmd.baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "preview: %v\n", err)
		os.Exit(1)
	}
	args, err := paramArgs(cmd.param)
	if err != nil {
		fmt.Fprintf(os.Stderr, "preview: %v\n", err)
		os.Exit(1)
	}
	rt := loop.Runtime()
	out, err := rt.Preview(context.Background(), runtime.Request{
		Operation: cmd.operation,
		Arguments: args,
		Caller:    cmd.caller,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "preview: %v\n", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "preview: %v\n", err)
		os.Exit(1)
	}
	if len(out.Errors) > 0 {
		os.Exit(1)
	}
}

func paramArgs(pairs []string) (map[string]any, error) {
	if len(pairs) == 0 {
		return map[string]any{}, nil
	}
	out := make(map[string]any, len(pairs))
	for _, pair := range pairs {
		key, val, ok := strings.Cut(pair, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("param %q is not key=value", pair)
		}
		out[key] = val
	}
	return out, nil
}

func newApproveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "approve <id>",
		Short: "Record approval for a pending confirmation.",
		Args:  cobra.ExactArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			approved, err := approveID(args[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "approve: %v\n", err)
				os.Exit(1)
			}
			fmt.Println(approved)
		},
	}
}

func approveID(id string) (string, error) {
	state := policy.NewState()
	if err := policy.ApplyEnv(state); err != nil {
		return "", err
	}
	return state.Approve(id)
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
		Short: "Serve MCP from the contract catalog.",
		Run: func(c *cobra.Command, _ []string) {
			runServe(*cmd, c)
		},
	}
	c.Flags().StringArrayVar(&cmd.contract, "contract", nil, "OpenAPI file. Repeat to register another API. Overrides config.")
	c.Flags().StringVar(&cmd.config, "config", "", "Path to veto.yaml provider keys.")
	c.Flags().StringVar(&cmd.agent, "agent", "", "Path to agent.yaml. Overrides agent_file.")
	c.Flags().StringVar(&cmd.relations, "relations", "", "Relations file. Overrides relations_file.")
	c.Flags().BoolVar(&cmd.stdio, "stdio", true, "Listen on stdio for MCP.")
	c.Flags().BoolVar(&cmd.http, "http", false, "Listen for MCP on Streamable HTTP. Requires the Veto-Caller header.")
	c.Flags().StringVar(&cmd.addr, "addr", mcpserver.DefaultAddr, "Listen address for --http.")
	c.Flags().StringArrayVar(&cmd.pin, "pin", nil, "Pin operation ids.")
	c.Flags().BoolVar(&cmd.directPins, "direct-pins", false, "Register direct MCP tools for pinned ids only.")
	c.Flags().BoolVar(&cmd.grouped, "grouped", false, "Register one MCP tool per resource.")
	c.Flags().StringVar(&cmd.baseURL, "base-url", "", "Override the server URL on every operation. Empty uses each contract server.")
	return c
}

func newGenerateCommand() (*cobra.Command, error) {
	cmd := &generateCmd{}
	c := &cobra.Command{
		Use:   "generate",
		Short: "Write a Go client, CLI, and MCP dispatch.",
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
	if err := c.MarkFlagRequired("out"); err != nil {
		return nil, err
	}
	if err := c.MarkFlagRequired("module"); err != nil {
		return nil, err
	}
	return c, nil
}

func newEvalCommand() (*cobra.Command, error) {
	cmd := &evalCmd{}
	c := &cobra.Command{
		Use:   "eval",
		Short: "Run a deterministic eval case.",
		Run: func(*cobra.Command, []string) {
			runEval(*cmd)
		},
	}
	c.Flags().StringArrayVar(&cmd.contract, "contract", nil, "OpenAPI file. Repeat to register another API. Overrides config.")
	c.Flags().StringArrayVar(&cmd.cases, "case", nil, "Eval case file or directory. Repeat to add another.")
	c.Flags().StringVar(&cmd.config, "config", "", "Path to veto.yaml provider keys.")
	c.Flags().StringVar(&cmd.agent, "agent", "", "Path to agent.yaml. Overrides agent_file.")
	c.Flags().StringVar(&cmd.relations, "relations", "", "Relations file. Overrides relations_file.")
	c.Flags().StringVar(&cmd.baseURL, "base-url", "", "Override the server URL on every operation. Empty uses each contract server.")
	if err := c.MarkFlagRequired("case"); err != nil {
		return nil, err
	}
	return c, nil
}

func newPackCommand() (*cobra.Command, error) {
	cmd := &packCmd{}
	c := &cobra.Command{
		Use:   "pack",
		Short: "Print the context pack for a message.",
		Run: func(*cobra.Command, []string) {
			runPack(*cmd)
		},
	}
	c.Flags().StringArrayVar(&cmd.contract, "contract", nil, "OpenAPI file. Repeat to register another API. Overrides config.")
	c.Flags().StringVar(&cmd.config, "config", "", "Path to veto.yaml provider keys.")
	c.Flags().StringVar(&cmd.agent, "agent", "", "Path to agent.yaml. Overrides agent_file.")
	c.Flags().StringVar(&cmd.relations, "relations", "", "Relations file. Overrides relations_file.")
	c.Flags().StringVar(&cmd.message, "message", "", "User message.")
	c.Flags().BoolVar(&cmd.asJSON, "json", false, "Print the pack as JSON.")
	if err := c.MarkFlagRequired("message"); err != nil {
		return nil, err
	}
	return c, nil
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
	c.Flags().BoolVar(&cmd.keepSensitive, "keep-sensitive", false, "Record response bodies in the trace.")
	c.Flags().StringVar(&cmd.from, "from", "", "Read a trace file instead of running the message.")
	return c
}

func runValidate(cmd validateCmd) {
	src, err := resolve(cmd.config, cmd.contract, cmd.relations, cmd.agent)
	contracts, relations, agent := src.contracts, src.relations, src.agent
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
	for _, line := range cat.Joins() {
		fmt.Println(line)
	}
}

func runGenerate(cmd generateCmd) {
	src, err := resolve(cmd.config, cmd.contract, cmd.relations, cmd.agent)
	contracts, relations, agent := src.contracts, src.relations, src.agent
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

func runServe(cmd serveCmd, c *cobra.Command) {
	stdio := cmd.stdio
	if cmd.http && c != nil && !c.Flags().Changed("stdio") {
		stdio = false
	}
	if !cmd.http && !stdio {
		fmt.Fprintf(os.Stderr, "serve: stdio or http required\n")
		os.Exit(1)
	}
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
	finish := func() {
		if err := stop(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		}
	}
	fail := func() {
		finish()
		os.Exit(1)
	}
	if err := mcpserver.ValidatePins(loop.Catalog, cmd.pin); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		fail()
	}
	calls := loop.Runtime()
	srv := &mcpserver.Server{
		Catalog:   loop.Catalog,
		Semantics: loop.Semantics,
		Calls:     &calls,
	}
	opt := mcpserver.Options{Pins: cmd.pin, DirectPins: cmd.directPins, Grouped: cmd.grouped}
	if cmd.http {
		ids, err := mcpserver.Identities(cfg.Callers, os.Getenv)
		if err != nil {
			fmt.Fprintf(os.Stderr, "serve: %v\n", err)
			fail()
		}
		handler, err := mcpserver.Handler(srv, opt, ids)
		if err != nil {
			fmt.Fprintf(os.Stderr, "serve: %v\n", err)
			fail()
		}
		addr := cmd.addr
		if addr == "" {
			addr = mcpserver.DefaultAddr
		}
		fmt.Fprintf(os.Stderr, "mcp http://%s\n", addr)
		if !stdio {
			if err := mcpserver.Serve(context.Background(), addr, handler); err != nil {
				fmt.Fprintf(os.Stderr, "serve: %v\n", err)
				fail()
			}
			finish()
			return
		}
		go func() {
			if err := mcpserver.Serve(context.Background(), addr, handler); err != nil {
				fmt.Fprintf(os.Stderr, "serve: %v\n", err)
				fail()
			}
		}()
	}
	if err := mcpserver.RunStdio(context.Background(), srv, opt); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		fail()
	}
	finish()
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
	finish := func() {
		if err := stop(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "eval: %v\n", err)
		}
	}
	if err := runCases(loop, cmd.cases); err != nil {
		fmt.Fprintf(os.Stderr, "eval failed: %v\n", err)
		finish()
		os.Exit(1)
	}
	finish()
}

func runCases(loop *agent.Loop, paths []string) error {
	cases, err := eval.LoadCases(paths)
	if err != nil {
		return err
	}
	r := &eval.Runner{Catalog: loop.Catalog, Semantics: loop.Semantics, Model: loop.Model, Loop: loop}
	for _, c := range cases {
		if err := r.Run(context.Background(), c); err != nil {
			return fmt.Errorf("%s: %w", c.Name, err)
		}
		fmt.Printf("ok: %s\n", c.Name)
	}
	return nil
}

func runPack(cmd packCmd) {
	loop, _, err := buildLoop(cmd.contract, cmd.config, cmd.agent, cmd.relations, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "pack: %v\n", err)
		os.Exit(1)
	}
	out, err := packOutput(loop, cmd.message, cmd.asJSON)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pack: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(out)
}

func packOutput(loop *agent.Loop, message string, asJSON bool) (string, error) {
	if loop.Packs == nil {
		loop.Packs = runctx.NewBuilder(0)
	}
	pack := loop.Packs.Build(loop.Catalog, []runctx.Turn{{Role: "user", Content: message}}, nil, loop.Semantics, nil)
	if !asJSON {
		return pack.Serialize() + "\n", nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(pack); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func runReplay(cmd replayCmd) {
	if cmd.from != "" {
		view, err := replay.Load(cmd.from)
		if err != nil {
			fmt.Fprintf(os.Stderr, "replay: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(view.String())
		return
	}
	if cmd.message == "" {
		fmt.Fprintf(os.Stderr, "replay: message required\n")
		os.Exit(1)
	}
	rec, err := telemetry.Record()
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay: %v\n", err)
		os.Exit(1)
	}
	finish := func() {
		if stopErr := rec.Stop(context.Background()); stopErr != nil {
			fmt.Fprintf(os.Stderr, "replay: %v\n", stopErr)
		}
	}
	fail := func() {
		finish()
		os.Exit(1)
	}
	loop, cfg, err := buildLoop(cmd.contract, cmd.config, cmd.agent, cmd.relations, cmd.baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay: %v\n", err)
		fail()
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
		fail()
	}
	text, err := finishReplay(rec.Spans(), redact, cfg.TraceFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay: %v\n", err)
		fail()
	}
	fmt.Print(text)
	finish()
}

func finishReplay(spans []telemetry.Span, redact bool, traceFile string) (string, error) {
	view := replay.FromSpans(spans, redact)
	if traceFile != "" {
		if err := replay.Save(traceFile, view); err != nil {
			return "", err
		}
	}
	return view.String(), nil
}

func callerName(name string) string {
	if name == "" {
		return "local"
	}
	return name
}

func allowSet(list []string) map[string]bool {
	if list == nil {
		return nil
	}
	out := make(map[string]bool, len(list))
	for _, p := range list {
		if p != "" {
			out[p] = true
		}
	}
	return out
}

func authSecrets(names config.Sources) map[string]string {
	if len(names) == 0 {
		return nil
	}
	out := make(map[string]string, len(names))
	for scheme, src := range names {
		if src.Kind() != "env" || src.Env == "" {
			continue
		}
		if val := os.Getenv(src.Env); val != "" {
			out[scheme] = val
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func tokenDir(cfg config.File) string {
	if d := os.Getenv("VETO_TOKEN_DIR"); d != "" {
		return d
	}
	if cfg.TokenDir != "" {
		return cfg.TokenDir
	}
	return auth.DefaultTokenDir()
}

func authResolver(cfg config.File) *auth.Resolver {
	schemes := make([]auth.Scheme, 0, len(cfg.Auth))
	for name, src := range cfg.Auth {
		schemes = append(schemes, auth.Scheme{
			Name:                   name,
			Source:                 src.Kind(),
			Env:                    src.Env,
			ClientID:               src.ClientID,
			ClientSecretEnv:        src.ClientSecretEnv,
			AuthorizationURL:       src.AuthorizationURL,
			TokenURL:               src.TokenURL,
			Issuer:                 src.Issuer,
			DeviceAuthorizationURL: src.DeviceAuthorizationURL,
			RedirectURL:            src.RedirectURL,
			Scopes:                 append([]string(nil), src.Scopes...),
			Audience:               src.Audience,
			Header:                 src.Header,
			Command:                append([]string(nil), src.Command...),
			Timeout:                time.Duration(src.Timeout),
			AuthToken:              src.AuthToken,
			UserToken:              src.UserToken,
			UserHeader:             src.UserHeader,
			Subject:                src.Subject,
			SubjectTokenType:       src.SubjectTokenType,
		})
	}
	return auth.New(auth.Options{
		Schemes:        schemes,
		Dir:            tokenDir(cfg),
		HTTP:           &http.Client{Timeout: cfg.Timeout},
		CommandTimeout: cfg.Timeout,
	})
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
		return nil, errors.New("contract required")
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
	data, err := os.ReadFile(relationsPath)
	if err != nil {
		return nil, fmt.Errorf("read relations: %w", err)
	}
	rels, err := catalog.ParseRelations(data)
	if err != nil {
		return nil, err
	}
	if err := catalog.ApplyRelations(cat, rels); err != nil {
		return nil, err
	}
	return cat, nil
}

type sources struct {
	cfg       config.File
	contracts []string
	relations string
	agent     string
}

func resolve(configPath string, contracts []string, relations, agent string) (sources, error) {
	return resolveBundle(configPath, "", contracts, relations, agent)
}

func resolveBundle(configPath, bundlePath string, contracts []string, relations, agent string) (sources, error) {
	cfg := config.Defaults()
	loadedConfig := false
	if configPath != "" {
		loaded, err := config.Load(configPath)
		if err != nil {
			return sources{}, err
		}
		cfg = loaded
		loadedConfig = true
	}
	if bundlePath == "" {
		bundlePath = cfg.Bundle
	}
	if bundlePath != "" {
		shared, err := bundle.Load(bundlePath)
		if err != nil {
			return sources{}, err
		}
		if loadedConfig {
			cfg = overlayBundle(cfg, shared.Config)
		} else {
			cfg = shared.Config
		}
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
	return sources{cfg: cfg, contracts: contracts, relations: relations, agent: agent}, nil
}

func overlayBundle(deploy, shared config.File) config.File {
	out := deploy
	if len(shared.Contracts) > 0 {
		out.Contracts = shared.Contracts
	}
	if shared.RelationsFile != "" {
		out.RelationsFile = shared.RelationsFile
	}
	if len(shared.Cases) > 0 {
		out.Cases = shared.Cases
	}
	if shared.SemanticsFile != "" {
		out.SemanticsFile = shared.SemanticsFile
		out.Semantics = shared.Semantics
	}
	if shared.AgentFile != "" {
		out.AgentFile = shared.AgentFile
	}
	if shared.FlowFile != "" {
		out.FlowFile = shared.FlowFile
	}
	return out
}

func releaseBundles() {
	if err := bundle.Release(); err != nil {
		fmt.Fprintf(os.Stderr, "bundle: %v\n", err)
	}
}

func buildLoop(contracts []string, configPath, agentPath, relationsPath, baseURL string) (*agent.Loop, config.File, error) {
	return buildLoopBundle(contracts, configPath, "", agentPath, relationsPath, baseURL)
}

func buildLoopBundle(contracts []string, configPath, bundlePath, agentPath, relationsPath, baseURL string) (*agent.Loop, config.File, error) {
	src, err := resolveBundle(configPath, bundlePath, contracts, relationsPath, agentPath)
	if err != nil {
		return nil, config.File{}, err
	}
	cfg := src.cfg
	contracts = src.contracts
	relationsPath = src.relations
	agentPath = src.agent
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
	if err := cat.SelectServer(cfg.Server); err != nil {
		return nil, config.File{}, err
	}
	var sem agent.Notes = semantics.NewDerived(cat)
	if cfg.SemanticsFile != "" {
		data, err := os.ReadFile(cfg.SemanticsFile)
		if err != nil {
			return nil, config.File{}, fmt.Errorf("read semantics: %w", err)
		}
		over, err := semantics.ParseOverlay(data, sem)
		if err != nil {
			return nil, config.File{}, err
		}
		sem = over
	}
	flows := map[string]*flow.Definition{}
	if cfg.FlowFile != "" {
		data, err := os.ReadFile(cfg.FlowFile)
		if err != nil {
			return nil, config.File{}, fmt.Errorf("read flow: %w", err)
		}
		def, err := flow.Parse(data)
		if err != nil {
			return nil, config.File{}, err
		}
		flows[def.Name] = def
	}
	pages := 0
	if cfg.Page == "follow" {
		pages = 5
	}
	loop, err := agent.New(cat, sem, execute.Client{
		BaseURL:     baseURL,
		HTTP:        auth.WithEnvProxy(&http.Client{Timeout: cfg.Timeout}),
		Auth:        authSecrets(cfg.Auth),
		Creds:       authResolver(cfg),
		FollowPages: pages,
		Fields:      cfg.ResponseFields,
		Limit:       cfg.ResponseLimit,
	})
	if err != nil {
		return nil, config.File{}, err
	}
	if err := policy.ApplyEnv(loop.State); err != nil {
		return nil, config.File{}, err
	}
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
	case "file":
		log, err := memory.NewLog(cfg.MemoryFile)
		if err != nil {
			return err
		}
		loop.Memory = log
	default:
		return fmt.Errorf("memory provider %q is not in this slice", cfg.Memory)
	}
	base := policy.Builtin{Caller: callerName(cfg.Caller), Allow: allowSet(cfg.Permissions)}
	switch cfg.Policy {
	case "builtin":
		loop.SetPolicy(base)
	case "opa":
		eng, err := opa.New(context.Background(), cfg.PolicyFile, cfg.PolicyBundle, base)
		if err != nil {
			return err
		}
		eng.Environment = cfg.Environment
		if cfg.Caller != "" {
			eng.Principal = cfg.Caller
		}
		loop.SetPolicy(eng)
		loop.SetFloor(base)
	default:
		return fmt.Errorf("unsupported policy provider %q", cfg.Policy)
	}
	if cfg.ApprovalWebhook.URL != "" || len(cfg.ApprovalWebhook.Command) > 0 {
		hook, err := policy.NewWebhook(cfg.ApprovalWebhook.URL, append([]string(nil), cfg.ApprovalWebhook.Command...))
		if err != nil {
			return err
		}
		loop.Notify = hook
	}
	if cfg.Execution != "in-process" {
		return fmt.Errorf("execution provider %q is not in this slice", cfg.Execution)
	}
	if cfg.Decision != "default" {
		return fmt.Errorf("decision provider %q is not in this slice", cfg.Decision)
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
