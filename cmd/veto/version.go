package main

import (
	"fmt"

	"github.com/aiveto/veto/mcpserver"
	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the veto version.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "veto "+mcpserver.Version)
		},
	}
}
