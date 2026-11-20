//go:build windows

package hostsedit

import "os"

// writeFile replaces the file in place on Windows. Rename-over is
// unreliable for system files that other processes may hold open.
func writeFile(path string, data []byte, mode os.FileMode) error {
	return os.WriteFile(path, data, mode)
}
