// Package main is the veto command.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/aiveto/veto/mcpserver"
	"github.com/spf13/cobra"
)

func main() {
	root, err := newRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "veto: %v\n", err)
		exitMain(1)
	}
	handled, err := jsonHelp(os.Stdout, root, os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "help: %v\n", err)
		exitMain(1)
	}
	if handled {
		exitMain(0)
	}
	if err := root.Execute(); err != nil {
		exitMain(1)
	}
	exitMain(0)
}

func useRootConfig(cmd *cobra.Command, local *string) {
	if local == nil || strings.TrimSpace(*local) != "" || cmd == nil || cmd.Root() == nil {
		return
	}
	flag := cmd.Root().PersistentFlags().Lookup("config")
	if flag == nil || !flag.Changed {
		return
	}
	*local = flag.Value.String()
}

func exitMain(code int) {
	releaseBundles()
	os.Exit(code)
}

func newRoot() (*cobra.Command, error) {
	evalCmd, err := newEvalCommand()
	if err != nil {
		return nil, err
	}
	generateCmd, err := newGenerateCommand()
	if err != nil {
		return nil, err
	}
	packCmd, err := newPackCommand()
	if err != nil {
		return nil, err
	}
	checkCmd := newCheckCommand()
	root := &cobra.Command{
		Use:          "veto",
		Short:        "Decide a call against an OpenAPI contract.",
		Long:         "Veto loads an OpenAPI contract, serves search, describe, and invoke, and refuses a destructive call until confirmation is stored.",
		SilenceUsage: true,
		Version:      mcpserver.Version,
	}
	root.SetVersionTemplate("veto {{.Version}}\n")
	root.PersistentFlags().String("config", "", "Path to veto.yaml. Works before the command.")
	root.AddCommand(
		newValidateCommand(),
		newInitCommand(),
		newProposeCommand(),
		newRunCommand(),
		newServeCommand(),
		evalCmd,
		generateCmd,
		newReplayCommand(),
		packCmd,
		newDoctorCommand(),
		newSearchCommand(),
		newDescribeCommand(),
		newInvokeCommand(),
		newPreviewCommand(),
		checkCmd,
		newAuthCommand(),
		newApproveCommand(),
		newVersionCommand(),
	)
	return root, nil
}
