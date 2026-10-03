package main

import (
	"fmt"
	"os"

	"github.com/aiveto/veto/generate"
	"github.com/spf13/cobra"
)

type generateCmd struct {
	config    string
	contract  []string
	out       string
	module    string
	agent     string
	relations string
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

func runGenerate(cmd generateCmd) {
	src, err := resolve(cmd.config, cmd.contract, cmd.relations, cmd.agent)
	contracts, relations, agent := src.contracts, src.relations, src.agent
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		exitMain(1)
	}
	cat, err := loadCatalog(contracts, relations)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		exitMain(1)
	}
	if err := applyAgent(cat, agent); err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		exitMain(1)
	}
	applyDeployment(cat, src.cfg)
	if err := generate.Write(cmd.out, cmd.module, cat); err != nil {
		fmt.Fprintf(os.Stderr, "generate: %v\n", err)
		exitMain(1)
	}
	fmt.Printf("ok: %s\n", cmd.out)
}
