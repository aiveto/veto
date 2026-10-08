package main

import (
	"fmt"
	"os"

	"github.com/aiveto/veto/catalog"
	"github.com/spf13/cobra"
)

type validateCmd struct {
	config    string
	contract  []string
	agent     string
	relations string
}

func newValidateCommand() *cobra.Command {
	cmd := &validateCmd{}
	c := &cobra.Command{
		Use:   "validate",
		Short: "Load and validate an OpenAPI contract.",
		Run: func(c *cobra.Command, _ []string) {
			useRootConfig(c, &cmd.config)
			runValidate(*cmd)
		},
	}
	c.Flags().StringVar(&cmd.config, "config", "", "Path to veto.yaml. Contracts and relations live here.")
	c.Flags().StringArrayVar(&cmd.contract, "contract", nil, "OpenAPI file. Repeat to register another API. Overrides config.")
	c.Flags().StringVar(&cmd.agent, "agent", "", "Path to agent.yaml.")
	c.Flags().StringVar(&cmd.relations, "relations", "", "Relations file. Joins a schema field to an operation.")
	return c
}

func runValidate(cmd validateCmd) {
	src, err := resolve(cmd.config, cmd.contract, cmd.relations, cmd.agent)
	contracts, relations, agent := src.contracts, src.relations, src.agent
	if err != nil {
		fmt.Fprintf(os.Stderr, "validate failed: %v\n", err)
		exitMain(1)
	}
	cat, err := loadCatalog(contracts, relations)
	if err != nil {
		fmt.Fprintf(os.Stderr, "validate failed: %v\n", err)
		exitMain(1)
	}
	if err := applyAgent(cat, agent); err != nil {
		fmt.Fprintf(os.Stderr, "validate failed: %v\n", err)
		exitMain(1)
	}
	applyDeployment(cat, src.cfg)
	fmt.Printf("ok: %s (%d operations)\n", cat.Title, len(cat.Operations))
	if line := confirmationNotice(src.cfg); line != "" {
		fmt.Println(line)
	}
	for _, line := range cat.Joins() {
		fmt.Println(line)
	}
	if line := relationGap(cat); line != "" {
		fmt.Println(line)
	}
}

func relationGap(cat *catalog.Catalog) string {
	if cat == nil {
		return ""
	}
	for _, e := range cat.Graph.Edges {
		if e.Kind == catalog.EdgeRelates {
			return ""
		}
	}
	return "no relations. A response field is not a call until relations.yaml names the operation."
}
