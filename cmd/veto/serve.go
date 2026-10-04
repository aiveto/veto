package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/aiveto/veto/capability"
	"github.com/aiveto/veto/mcpserver"
	"github.com/aiveto/veto/telemetry"
	"github.com/spf13/cobra"
)

type serveCmd struct {
	contract   []string
	config     string
	agent      string
	relations  string
	stdio      bool
	http       bool
	jsonLines  bool
	addr       string
	pin        []string
	directPins bool
	grouped    bool
	baseURL    string
}

func newServeCommand() *cobra.Command {
	cmd := &serveCmd{stdio: true}
	c := &cobra.Command{
		Use:   "serve",
		Short: "Serve the three capabilities over MCP or JSON.",
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
	c.Flags().BoolVar(&cmd.jsonLines, "json", false, "Serve JSON lines for skills and scripts.")
	c.Flags().StringVar(&cmd.addr, "addr", mcpserver.DefaultAddr, "Listen address for --http.")
	c.Flags().StringArrayVar(&cmd.pin, "pin", nil, "Register a direct MCP tool for this operation id.")
	c.Flags().BoolVar(&cmd.directPins, "direct-pins", false, "Register pinned ids as tools. Implied by --pin.")
	c.Flags().BoolVar(&cmd.grouped, "grouped", false, "Register one MCP tool per resource.")
	c.Flags().StringVar(&cmd.baseURL, "base-url", "", "Override the server URL on every operation. Empty uses each contract server.")
	return c
}

func runServe(cmd serveCmd, c *cobra.Command) {
	stdio := cmd.stdio
	if (cmd.http || cmd.jsonLines) && c != nil && !c.Flags().Changed("stdio") {
		stdio = false
	}
	if cmd.jsonLines && stdio {
		fmt.Fprintf(os.Stderr, "serve: --json and --stdio both use stdin\n")
		exitMain(1)
	}
	if !cmd.http && !stdio && !cmd.jsonLines {
		fmt.Fprintf(os.Stderr, "serve: stdio, http, or json required\n")
		exitMain(1)
	}
	srv, cfg, err := buildServer(cmd.contract, cmd.config, cmd.agent, cmd.relations, cmd.baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		exitMain(1)
	}
	if err := stdioTraceConflict(stdio || cmd.jsonLines, cfg.TraceExport); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		exitMain(1)
	}
	stop, err := telemetry.Install(cfg.TraceExport)
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		exitMain(1)
	}
	finish := func() {
		if err := stop(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		}
	}
	fail := func() {
		finish()
		exitMain(1)
	}
	if err := mcpserver.ValidatePins(srv.Catalog, cmd.pin); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		fail()
	}
	ctx, stopSig := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSig()
	opt := serveOptions(cmd)
	if cmd.jsonLines {
		stopRead := closeOnDone(ctx, os.Stdin)
		defer stopRead()
		if err := capability.RunJSON(ctx, srv, os.Stdin, os.Stdout); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, os.ErrClosed) {
			fmt.Fprintf(os.Stderr, "serve: %v\n", err)
			fail()
		}
		finish()
		return
	}
	if cmd.http {
		ids, err := mcpserver.Identities(cfg.Callers, os.Getenv)
		if err != nil {
			fmt.Fprintf(os.Stderr, "serve: %v\n", err)
			fail()
		}
		httpOpt := opt
		httpOpt.ChatApproval = cfg.ChatApproval
		handler, err := mcpserver.Handler(srv, httpOpt, ids)
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
			if err := mcpserver.Serve(ctx, addr, handler); err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintf(os.Stderr, "serve: %v\n", err)
				fail()
			}
			finish()
			return
		}
		go func() {
			if err := mcpserver.Serve(ctx, addr, handler); err != nil && !errors.Is(err, context.Canceled) {
				fmt.Fprintf(os.Stderr, "serve: %v\n", err)
				fail()
			}
		}()
	}
	if err := mcpserver.RunStdio(ctx, srv, opt); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		fail()
	}
	finish()
}

func serveOptions(cmd serveCmd) mcpserver.Options {
	return mcpserver.Options{
		Pins:       cmd.pin,
		DirectPins: cmd.directPins || len(cmd.pin) > 0,
		Grouped:    cmd.grouped,
	}
}

func stdioTraceConflict(stdio bool, export string) error {
	if stdio && export == "stdout" {
		return errors.New("trace_export stdout cannot share stdio")
	}
	return nil
}

func closeOnDone(ctx context.Context, c io.Closer) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = c.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}
