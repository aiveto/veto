package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/replay"
	"github.com/aiveto/veto/telemetry"
	"github.com/spf13/cobra"
)

type replayCmd struct {
	contract      []string
	message       string
	config        string
	agent         string
	relations     string
	baseURL       string
	keepSensitive bool
	from          string
}

func newReplayCommand() *cobra.Command {
	cmd := &replayCmd{}
	c := &cobra.Command{
		Use:     "replay",
		Short:   "Run one message and print the recorded trace.",
		Example: "  veto replay --message \"delete order 123\"",
		PreRunE: func(*cobra.Command, []string) error {
			if strings.TrimSpace(cmd.from) == "" && strings.TrimSpace(cmd.message) == "" {
				return errors.New(`message required, for example: veto replay --message "delete order 123"`)
			}
			return nil
		},
		Run: func(c *cobra.Command, _ []string) {
			useRootConfig(c, &cmd.config)
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

func runReplay(cmd replayCmd) {
	if cmd.from != "" {
		view, err := replay.Load(cmd.from)
		if err != nil {
			fmt.Fprintf(os.Stderr, "replay: %v\n", err)
			exitMain(1)
		}
		fmt.Print(view.String())
		return
	}
	rec, err := telemetry.Record()
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay: %v\n", err)
		exitMain(1)
	}
	finish := func() {
		if stopErr := rec.Stop(context.Background()); stopErr != nil {
			fmt.Fprintf(os.Stderr, "replay: %v\n", stopErr)
		}
	}
	fail := func() {
		finish()
		exitMain(1)
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
		if c, ok := loop.RuntimePtr().Exec.(execute.Client); ok {
			c.RecordBody = true
			loop.RuntimePtr().Exec = c
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
