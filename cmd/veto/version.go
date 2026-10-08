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
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "veto "+mcpserver.Version)
			return err
		},
	}
}
