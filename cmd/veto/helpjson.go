package main

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"io"
	"slices"
	"strings"

	"github.com/aiveto/veto/mcpserver"
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

	capabilityHelp struct {
		mcpserver.Capability
		Flags []flagDoc `json:"flags"`
	}

	rootHelp struct {
		commandDoc
		Capabilities []mcpserver.Capability `json:"capabilities,omitempty"`
	}
)

func jsonHelp(w io.Writer, root *cobra.Command, args []string) (bool, error) {
	if !wantsHelpJSON(args) {
		return false, nil
	}
	cmd := targetCommand(root, args)
	doc := describeCommand(cmd)
	var err error
	if spec, ok := mcpserver.CapabilityByCommand(doc.Command); ok {
		err = writeIndentedJSON(w, capabilityHelp{Capability: spec, Flags: doc.Flags})
	} else if cmd == root {
		err = writeIndentedJSON(w, rootHelp{commandDoc: doc, Capabilities: mcpserver.Capabilities()})
	} else {
		err = writeIndentedJSON(w, doc)
	}
	if err != nil {
		return true, err
	}
	return true, nil
}

func writeIndentedJSON(w io.Writer, v any) error {
	if err := jsonv2.MarshalWrite(w, v, jsontext.Multiline(true), jsontext.WithIndent("  ")); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

func wantsHelpJSON(args []string) bool {
	return slices.Contains(args, "--help-json")
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
	return slices.Contains(f.Annotations[cobra.BashCompOneRequiredFlag], "true")
}
