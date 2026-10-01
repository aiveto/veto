package generate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/aiveto/veto/catalog"
)

func Write(dir, module string, cat *catalog.Catalog) error {
	files, err := Render(module, cat)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "sdk"), 0o750); err != nil {
		return fmt.Errorf("mkdir sdk: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "cli"), 0o750); err != nil {
		return fmt.Errorf("mkdir cli: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "dispatch"), 0o750); err != nil {
		return fmt.Errorf("mkdir dispatch: %w", err)
	}
	mod := "module " + module + "\n\ngo 1.26.0\n\nrequire github.com/aiveto/veto v0.0.0\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o600); err != nil {
		return fmt.Errorf("write go.mod: %w", err)
	}
	writes := []struct {
		path string
		body []byte
	}{
		{filepath.Join(dir, "sdk", "client.go"), files.SDK},
		{filepath.Join(dir, "cli", "main.go"), files.CLI},
		{filepath.Join(dir, "dispatch", "call.go"), files.MCP},
	}
	for _, w := range writes {
		if err := os.WriteFile(w.path, w.body, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", w.path, err)
		}
	}
	return nil
}
