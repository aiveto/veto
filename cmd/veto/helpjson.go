package main

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type (
	commandDoc struct {
		Command  string    `json:"command"`
		Short    string    `json:"short"`
		Flags    []flagDoc `json:"flags"`
		Commands []string  `json:"commands,omitempty"`
	}

	flagDoc struct {
		Name     string `json:"name"`
		Usage    string `json:"usage"`
		Required bool   `json:"required,omitempty"`
	}
)

func jsonHelp(w io.Writer, root *cobra.Command, args []string) (bool, error) {
	if !wantsHelpJSON(args) {
		return false, nil
	}
	doc := describeCommand(targetCommand(root, args))
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return true, err
	}
	return true, nil
}

func wantsHelpJSON(args []string) bool {
	for _, a := range args {
		if a == "--help-json" {
			return true
		}
	}
	return false
}

func targetCommand(root *cobra.Command, args []string) *cobra.Command {
	for _, a := range args {
		if a == "--help-json" || strings.HasPrefix(a, "-") {
			continue
		}
		for _, c := range root.Commands() {
			if c.Name() == a {
				return c
			}
		}
		break
	}
	return root
}

func describeCommand(cmd *cobra.Command) commandDoc {
	doc := commandDoc{Command: cmd.Name(), Short: cmd.Short}
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" {
			return
		}
		doc.Flags = append(doc.Flags, flagDoc{
			Name:     f.Name,
			Usage:    f.Usage,
			Required: annotationRequired(f),
		})
	})
	for _, c := range cmd.Commands() {
		doc.Commands = append(doc.Commands, c.Name())
	}
	if doc.Flags == nil {
		doc.Flags = []flagDoc{}
	}
	return doc
}

func annotationRequired(f *pflag.Flag) bool {
	for _, v := range f.Annotations[cobra.BashCompOneRequiredFlag] {
		if v == "true" {
			return true
		}
	}
	return false
}
