package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aiveto/veto/runtime"
	"github.com/spf13/cobra"
)

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
		exitMain(1)
	}
	loop, _, err := buildLoop(cmd.contract, cmd.config, cmd.agent, cmd.relations, cmd.baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "preview: %v\n", err)
		exitMain(1)
	}
	args, err := paramArgs(cmd.param)
	if err != nil {
		fmt.Fprintf(os.Stderr, "preview: %v\n", err)
		exitMain(1)
	}
	rt := loop.Runtime()
	out, err := rt.Preview(context.Background(), runtime.Request{
		Operation: cmd.operation,
		Arguments: args,
		Caller:    cmd.caller,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "preview: %v\n", err)
		exitMain(1)
	}
	if err := writeIndentedJSON(os.Stdout, out); err != nil {
		fmt.Fprintf(os.Stderr, "preview: %v\n", err)
		exitMain(1)
	}
	if len(out.Errors) > 0 {
		exitMain(1)
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
