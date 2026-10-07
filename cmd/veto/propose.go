package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/aiveto/veto/flow"
	"github.com/spf13/cobra"
)

func newProposeCommand() *cobra.Command {
	var flags catalogFlags
	c := &cobra.Command{
		Use:   "propose",
		Short: "Print read-only tasks drafted from declared relations.",
		Run: func(*cobra.Command, []string) {
			if err := runPropose(flags); err != nil {
				fmt.Fprintf(os.Stderr, "propose: %v\n", err)
				exitMain(1)
			}
		},
	}
	addCatalogFlags(c, &flags)
	return c
}

func runPropose(flags catalogFlags) error {
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
		return errors.New("no relation joins two reads that list response fields")
	}
	body, err := flow.Format(tasks)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(body)
	return err
}
