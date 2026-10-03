package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/runctx"
	"github.com/spf13/cobra"
)

type packCmd struct {
	contract  []string
	config    string
	agent     string
	relations string
	message   string
	asJSON    bool
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

func runPack(cmd packCmd) {
	loop, _, err := buildLoop(cmd.contract, cmd.config, cmd.agent, cmd.relations, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "pack: %v\n", err)
		exitMain(1)
	}
	out, err := packOutput(loop, cmd.message, cmd.asJSON)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pack: %v\n", err)
		exitMain(1)
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
