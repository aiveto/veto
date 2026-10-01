package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newInitCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "init [contract...]",
		Short: "Write veto.yaml for the named contracts.",
		Args:  cobra.MinimumNArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			if err := writeStarter(".", args); err != nil {
				fmt.Fprintf(os.Stderr, "init: %v\n", err)
				exitMain(1)
			}
		},
	}
	return c
}

func writeStarter(dir string, contracts []string) error {
	if len(contracts) == 0 {
		return errors.New("contract required")
	}
	configPath := filepath.Join(dir, "veto.yaml")
	if _, err := os.Stat(configPath); err == nil {
		return errors.New("veto.yaml already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read veto.yaml: %w", err)
	}
	body, err := starterYAML(contracts)
	if err != nil {
		return err
	}
	if err := writeIfMissing(filepath.Join(dir, "relations.yaml"), []byte("relations: []\n")); err != nil {
		return err
	}
	if err := writeNew(configPath, body); err != nil {
		return err
	}
	return nil
}

func starterYAML(contracts []string) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	err := enc.Encode(struct {
		Contracts     []string `yaml:"contracts"`
		RelationsFile string   `yaml:"relations_file"`
	}{
		Contracts:     contracts,
		RelationsFile: "relations.yaml",
	})
	if err != nil {
		return nil, fmt.Errorf("write veto.yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("write veto.yaml: %w", err)
	}
	return buf.Bytes(), nil
}

func writeIfMissing(path string, body []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	_, werr := f.Write(body)
	cerr := f.Close()
	if werr != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), werr)
	}
	if cerr != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), cerr)
	}
	return nil
}

func writeNew(path string, body []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s already exists", filepath.Base(path))
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	_, werr := f.Write(body)
	cerr := f.Close()
	if werr != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), werr)
	}
	if cerr != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), cerr)
	}
	return nil
}
