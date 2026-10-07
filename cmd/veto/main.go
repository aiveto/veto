// Package main is the veto command.
package main

import (
	"fmt"
	"os"

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
		SilenceUsage: true,
	}
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
	)
	return root, nil
}
