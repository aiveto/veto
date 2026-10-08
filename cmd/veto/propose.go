package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aiveto/veto/catalog"

	"github.com/aiveto/veto/flow"
	"github.com/spf13/cobra"
)

func newProposeCommand() *cobra.Command {
	var flags catalogFlags
	var out string
	c := &cobra.Command{
		Use:     "propose",
		Short:   "Print read-only tasks drafted from declared relations.",
		Example: "  veto propose --config veto.yaml --out draft.yaml",
		Run: func(*cobra.Command, []string) {
			if err := runPropose(flags, out); err != nil {
				fmt.Fprintf(os.Stderr, "propose: %v\n", err)
				exitMain(1)
			}
		},
	}
	addCatalogFlags(c, &flags)
	c.Flags().StringVar(&out, "out", "", "Write the tasks to this file. Replaces that file.")
	return c
}

func runPropose(flags catalogFlags, out string) error {
	src, err := resolve(flags.config, flags.contract, flags.relations, flags.agent)
	if err != nil {
		return err
	}
	cat, err := loadCatalog(src.contracts, src.relations)
	if err != nil {
		return err
	}
	tasks := flow.Propose(cat)
	if len(tasks) == 0 {
		return errors.New("no relation joins two reads that list response fields. Name the field and the operation in relations.yaml")
	}
	body, err := flow.Format(tasks)
	if err != nil {
		return err
	}
	if out == "" {
		_, err = os.Stdout.Write(body)
		return err
	}
	if err := os.WriteFile(out, body, 0o600); err != nil {
		return err
	}
	fmt.Println("wrote " + out)
	for _, line := range proposeCommands(src.cfg.FlowFile, out, cat, tasks) {
		fmt.Println(line)
	}
	return nil
}

func proposeCommands(flowFile, out string, cat *catalog.Catalog, tasks []*flow.Definition) []string {
	lines := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if task == nil || task.Name == "" {
			continue
		}
		cmd := "veto run"
		if !samePath(flowFile, out) {
			cmd += " --flow " + out
		}
		cmd += " " + task.Name
		if cat != nil && len(task.Steps) > 0 {
			if name := requiredParamName(cat.ByID(task.Steps[0].Operation)); name != "" {
				cmd += " --param " + name + "="
			}
		}
		lines = append(lines, cmd)
	}
	return lines
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return aa == bb
}
