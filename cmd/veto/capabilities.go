package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	jsonv2 "encoding/json/v2"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/mcpserver"
	"github.com/spf13/cobra"
)

type catalogFlags struct {
	contract  []string
	config    string
	agent     string
	relations string
	baseURL   string
}

func newSearchCommand() *cobra.Command {
	var flags catalogFlags
	c := &cobra.Command{
		Use:   "search [query JSON or words]",
		Short: mcpserver.SearchDescription,
		Args:  cobra.MinimumNArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			runSearch(flags, args)
		},
	}
	addCatalogFlags(c, &flags)
	return c
}

func newDescribeCommand() *cobra.Command {
	var flags catalogFlags
	c := &cobra.Command{
		Use:   "describe [operation JSON or id]",
		Short: mcpserver.DescribeDescription,
		Args:  cobra.MinimumNArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			runDescribe(flags, args)
		},
	}
	addCatalogFlags(c, &flags)
	return c
}

func newInvokeCommand() *cobra.Command {
	var flags catalogFlags
	c := &cobra.Command{
		Use:   "invoke [operation JSON or id]",
		Short: "Invoke an operation through policy and HTTP.",
		Args:  cobra.MinimumNArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			runInvoke(flags, args)
		},
	}
	addCatalogFlags(c, &flags)
	return c
}

func addCatalogFlags(c *cobra.Command, flags *catalogFlags) {
	c.Flags().StringArrayVar(&flags.contract, "contract", nil, "OpenAPI file. Repeat to register another API. Overrides config.")
	c.Flags().StringVar(&flags.config, "config", "", "Path to veto.yaml provider keys.")
	c.Flags().StringVar(&flags.agent, "agent", "", "Path to agent.yaml. Overrides agent_file.")
	c.Flags().StringVar(&flags.relations, "relations", "", "Relations file. Overrides relations_file.")
	c.Flags().StringVar(&flags.baseURL, "base-url", "", "Override the server URL on every operation.")
}

func capabilityServer(loop *agent.Loop) *mcpserver.Server {
	if loop == nil {
		return &mcpserver.Server{}
	}
	calls := loop.Runtime()
	return &mcpserver.Server{
		Catalog:   loop.Catalog,
		Semantics: loop.Semantics,
		Calls:     &calls,
	}
}

func openCapabilityServer(flags catalogFlags) (*mcpserver.Server, error) {
	loop, _, err := buildLoop(flags.contract, flags.config, flags.agent, flags.relations, flags.baseURL)
	if err != nil {
		return nil, err
	}
	return capabilityServer(loop), nil
}

func runSearch(flags catalogFlags, args []string) {
	srv, err := openCapabilityServer(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "search: %v\n", err)
		exitMain(1)
	}
	in, err := decodeSearch(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "search: %v\n", err)
		exitMain(1)
	}
	b, err := srv.RunSearch(in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "search: %v\n", err)
		exitMain(1)
	}
	fmt.Printf("%s\n", b)
}

func runDescribe(flags catalogFlags, args []string) {
	srv, err := openCapabilityServer(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "describe: %v\n", err)
		exitMain(1)
	}
	in, err := decodeDescribe(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "describe: %v\n", err)
		exitMain(1)
	}
	b, err := srv.RunDescribe(in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "describe: %v\n", err)
		exitMain(1)
	}
	fmt.Printf("%s\n", b)
}

func runInvoke(flags catalogFlags, args []string) {
	srv, err := openCapabilityServer(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invoke: %v\n", err)
		exitMain(1)
	}
	in, err := decodeInvoke(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invoke: %v\n", err)
		exitMain(1)
	}
	ctx := auth.WithCaller(context.Background(), callerName(""))
	b, err := srv.RunInvoke(ctx, in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invoke: %v\n", err)
		if len(b) > 0 {
			fmt.Printf("%s\n", b)
		}
		exitMain(1)
	}
	fmt.Printf("%s\n", b)
}

func decodeSearch(args []string) (mcpserver.SearchArgs, error) {
	var in mcpserver.SearchArgs
	ok, err := unmarshalObject(args, &in)
	if err != nil || ok {
		return in, err
	}
	in.Query = strings.Join(args, " ")
	return in, nil
}

func decodeDescribe(args []string) (mcpserver.DescribeArgs, error) {
	var in mcpserver.DescribeArgs
	ok, err := unmarshalObject(args, &in)
	if err != nil || ok {
		return in, err
	}
	in.OperationID = args[0]
	return in, nil
}

func decodeInvoke(args []string) (mcpserver.InvokeArgs, error) {
	var in mcpserver.InvokeArgs
	ok, err := unmarshalObject(args, &in)
	if err != nil || ok {
		return in, err
	}
	in.OperationID = args[0]
	if len(args) > 1 {
		if err := jsonv2.Unmarshal([]byte(args[1]), &in.Params); err != nil {
			return in, fmt.Errorf("params: %w", err)
		}
	}
	return in, nil
}

func unmarshalObject[T any](args []string, dst *T) (bool, error) {
	if len(args) != 1 {
		return false, nil
	}
	raw := strings.TrimSpace(args[0])
	if !strings.HasPrefix(raw, "{") {
		return false, nil
	}
	return true, jsonv2.Unmarshal([]byte(raw), dst)
}
