package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/eval"
	"github.com/aiveto/veto/telemetry"
	"github.com/spf13/cobra"
)

type evalCmd struct {
	contract  []string
	cases     []string
	config    string
	agent     string
	relations string
	baseURL   string
}

func newEvalCommand() (*cobra.Command, error) {
	cmd := &evalCmd{}
	c := &cobra.Command{
		Use:     "eval",
		Short:   "Run a deterministic eval case.",
		Example: "  veto eval --case testdata/delete.yaml",
		PreRunE: func(*cobra.Command, []string) error {
			if blank(cmd.cases) {
				return errors.New("--case required, for example: veto eval --case testdata/delete.yaml")
			}
			return nil
		},
		Run: func(c *cobra.Command, _ []string) {
			useRootConfig(c, &cmd.config)
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

func blank(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func runEval(cmd evalCmd) {
	loop, cfg, err := buildEvalLoop(cmd.contract, cmd.config, cmd.agent, cmd.relations, cmd.baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval: %v\n", err)
		exitMain(1)
	}
	stop, err := telemetry.Install(cfg.TraceExport)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval: %v\n", err)
		exitMain(1)
	}
	finish := func() {
		if err := stop(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "eval: %v\n", err)
		}
	}
	if err := runCases(loop, cmd.cases); err != nil {
		fmt.Fprintf(os.Stderr, "eval failed: %v\n", err)
		finish()
		exitMain(1)
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
