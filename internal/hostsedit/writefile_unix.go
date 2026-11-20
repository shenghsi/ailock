//go:build !windows

package hostsedit

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeFile replaces path atomically on Unix: write a temp file in the
// same directory, then rename over the target. A crash mid-write can
// never leave a half-edited hosts file.
func writeFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ailock-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename over %s: %w", path, err)
	}
	return nil
}
