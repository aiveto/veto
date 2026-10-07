package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/flow"
	"github.com/aiveto/veto/runtime"
	"github.com/spf13/cobra"
)

func newRunCommand() *cobra.Command {
	var flags catalogFlags
	var params []string
	c := &cobra.Command{
		Use:   "run TASK",
		Short: "Run a read-only task and print what stayed true.",
		Args:  cobra.ExactArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			if err := runTask(flags, args[0], params); err != nil {
				fmt.Fprintf(os.Stderr, "run: %v\n", err)
				exitMain(1)
			}
		},
	}
	addCatalogFlags(c, &flags)
	c.Flags().StringArrayVar(&params, "param", nil, "First-step parameter, as name=value. Repeat for another.")
	c.Flags().StringVar(&flags.caller, "caller", "", "Caller or tenant passed to policy.")
	return c
}

func runTask(flags catalogFlags, name string, pairs []string) error {
	params, err := taskParams(pairs)
	if err != nil {
		return err
	}
	srv, cfg, err := buildServer(flags.contract, flags.config, flags.agent, flags.relations, flags.baseURL)
	if err != nil {
		return err
	}
	def := srv.Flows[name]
	if def == nil || strings.TrimSpace(def.Question) == "" {
		return fmt.Errorf("unknown task %s", name)
	}
	if srv.Calls == nil {
		return errors.New("runtime required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	ctx = auth.WithCaller(ctx, auth.OrLocal(flags.caller))
	var bodies []string
	runner := flow.Runner{Invoke: func(ctx context.Context, operationID string, stepParams map[string]string, approvalID string) (string, string, error) {
		res, err := srv.Calls.Invoke(ctx, runtime.Request{
			Operation: operationID,
			Arguments: runtime.FromStrings(stepParams),
			Approval:  approvalID,
		})
		if err != nil {
			return res.Status, "", err
		}
		if res.Status == runtime.StatusOK {
			bodies = append(bodies, res.Body)
		}
		return res.Status, res.Body, nil
	}}
	_, err = runner.Run(ctx, def, params)
	if err != nil {
		return flow.RunFailure(def, err)
	}
	lines, err := flow.Witness(def, srv.Catalog, bodies)
	if err != nil {
		return err
	}
	for _, line := range lines {
		fmt.Println(line)
	}
	return nil
}

func taskParams(pairs []string) (map[string]string, error) {
	out := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, _, ok := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, errors.New("param needs a name and a value")
		}
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("param %s is repeated", key)
		}
		_, value, _ := strings.Cut(pair, "=")
		out[key] = value
	}
	return out, nil
}
