//go:build windows

package selfupdate

import (
	"fmt"
	"os"
)

// ReplaceExecutable swaps the binary at path with data. A running
// executable cannot be overwritten on Windows, but it CAN be renamed:
// the running process keeps the old image under the new name, and the
// fresh file lands at the original path for the next start. A stale
// .old file from a previous update is removed first.
func ReplaceExecutable(path string, data []byte) error {
	old := path + ".old"
	_ = os.Remove(old) // stale leftover from a previous update
	if _, err := os.Stat(path); err == nil {
		if err := os.Rename(path, old); err != nil {
			return fmt.Errorf("rename running exe away from %s: %w", path, err)
		}
	}
	return os.WriteFile(path, data, 0o755)
}
