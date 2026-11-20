//go:build !windows

package selfupdate

import (
	"fmt"
	"os"
	"path/filepath"
)

// ReplaceExecutable atomically replaces the binary at path with data.
// Writing a running executable in place fails with ETXTBSY on Linux;
// renaming over it is fine, so: write a temp file next to it, then rename.
func ReplaceExecutable(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ailock-update-*")
	if err != nil {
		return fmt.Errorf("write next to %s (need root?): %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
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
