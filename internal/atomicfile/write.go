// Package atomicfile writes a complete file by renaming a temporary sibling.
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write replaces path with data. A non-zero perm is applied to the temp file and the result.
func Write(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if perm != 0 {
		if err := tmp.Chmod(perm); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	if perm != 0 {
		return os.Chmod(path, perm)
	}
	return nil
}
