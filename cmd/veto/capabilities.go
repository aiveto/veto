package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	jsonv2 "encoding/json/v2"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/capability"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runtime"
	"github.com/spf13/cobra"
)

type catalogFlags struct {
	contract  []string
	config    string
	agent     string
	relations string
	baseURL   string
	caller    string
}

func newSearchCommand() *cobra.Command {
	var flags catalogFlags
	c := &cobra.Command{
		Use:   "search [query JSON or words]",
		Short: capability.SearchDescription,
		Args:  cobra.MinimumNArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			runCap("search", flags, func(srv *capability.Server) ([]byte, error) {
				in, err := decodeSearch(args)
				if err != nil {
					return nil, err
				}
				return srv.RunSearch(in)
			})
		},
	}
	addCatalogFlags(c, &flags)
	return c
}

func newDescribeCommand() *cobra.Command {
	var flags catalogFlags
	c := &cobra.Command{
		Use:   "describe [operation JSON or id]",
		Short: capability.DescribeDescription,
		Args:  cobra.MinimumNArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			runCap("describe", flags, func(srv *capability.Server) ([]byte, error) {
				in, err := decodeDescribe(args)
				if err != nil {
					return nil, err
				}
				return srv.RunDescribe(in)
			})
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
			runCap("invoke", flags, func(srv *capability.Server) ([]byte, error) {
				in, err := decodeInvoke(args)
				if err != nil {
					return nil, err
				}
				ctx := auth.WithCaller(context.Background(), auth.OrLocal(flags.caller))
				return runInvoke(ctx, srv, in)
			})
		},
	}
	addCatalogFlags(c, &flags)
	c.Flags().StringVar(&flags.caller, "caller", "", "Caller or tenant passed to policy.")
	return c
}

func addCatalogFlags(c *cobra.Command, flags *catalogFlags) {
	c.Flags().StringArrayVar(&flags.contract, "contract", nil, "OpenAPI file. Repeat to register another API. Overrides config.")
	c.Flags().StringVar(&flags.config, "config", "", "Path to veto.yaml provider keys.")
	c.Flags().StringVar(&flags.agent, "agent", "", "Path to agent.yaml. Overrides agent_file.")
	c.Flags().StringVar(&flags.relations, "relations", "", "Relations file. Overrides relations_file.")
	c.Flags().StringVar(&flags.baseURL, "base-url", "", "Override the server URL on every operation.")
}

func newServer(loop *agent.Loop) *capability.Server {
	if loop == nil {
		return &capability.Server{}
	}
	calls := loop.Runtime()
	return &capability.Server{
		Catalog:   loop.Catalog,
		Semantics: loop.Semantics,
		Calls:     &calls,
	}
}

func openCapabilityServer(flags catalogFlags) (*capability.Server, error) {
	srv, _, err := buildServer(flags.contract, flags.config, flags.agent, flags.relations, flags.baseURL)
	if err != nil {
		return nil, err
	}
	return srv, nil
}

func runInvoke(ctx context.Context, srv *capability.Server, in capability.InvokeArgs) ([]byte, error) {
	b, err := srv.RunInvoke(ctx, in)
	if err != nil || in.ApprovalID != "" || in.Preview {
		return b, err
	}
	var res capability.InvokeResult
	if decErr := jsonv2.Unmarshal(b, &res); decErr != nil || res.Status != runtime.StatusConfirmationRequired || res.ApprovalID == "" {
		return b, err
	}
	sentence := heldSentence(ctx, srv, res)
	accepted, asked := confirmInvoke(sentence)
	if !asked || !accepted {
		return b, err
	}
	if srv == nil || srv.Calls == nil || srv.Calls.State == nil {
		return b, err
	}
	approved, approveErr := srv.Calls.State.Approve(ctx, res.ApprovalID)
	if approveErr != nil {
		return b, approveErr
	}
	in.ApprovalID = approved
	return srv.RunInvoke(ctx, in)
}

func heldSentence(ctx context.Context, srv *capability.Server, res capability.InvokeResult) string {
	op := res.OperationID
	var params map[string]string
	var caller string
	if srv != nil && srv.Calls != nil && srv.Calls.State != nil {
		if pending := srv.Calls.State.Pending(ctx, res.ApprovalID); pending != nil {
			op = pending.OperationID
			params = pending.Params
			caller = pending.Caller
		}
	}
	return policy.ConfirmAsk(caller, op, params)
}

var confirmInvoke = confirmInvokeTTY

func confirmInvokeTTY(sentence string) (accepted bool, asked bool) {
	if !invokeStdinTTY() {
		return false, false
	}
	fmt.Fprintf(os.Stderr, "%s [y/N] ", sentence)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return false, true
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, true
	default:
		return false, true
	}
}

func invokeStdinTTY() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func runCap(name string, flags catalogFlags, fn func(*capability.Server) ([]byte, error)) {
	srv, err := openCapabilityServer(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		exitMain(1)
	}
	b, err := fn(srv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		if len(b) > 0 {
			fmt.Printf("%s\n", b)
		}
		exitMain(1)
	}
	fmt.Printf("%s\n", b)
}

func decodeSearch(args []string) (capability.SearchArgs, error) {
	var in capability.SearchArgs
	ok, err := unmarshalObject(args, &in)
	if err != nil || ok {
		return in, err
	}
	in.Query = strings.Join(args, " ")
	return in, nil
}

func decodeDescribe(args []string) (capability.DescribeArgs, error) {
	var in capability.DescribeArgs
	ok, err := unmarshalObject(args, &in)
	if err != nil || ok {
		return in, err
	}
	in.OperationID = args[0]
	return in, nil
}

func decodeInvoke(args []string) (capability.InvokeArgs, error) {
	var in capability.InvokeArgs
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
