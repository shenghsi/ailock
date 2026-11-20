//go:build windows

package platform

import (
	"os"
	"path/filepath"
)

// HostsPath returns the Windows hosts file path.
func HostsPath() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, `System32\drivers\etc\hosts`)
}

// ConfigDir returns the default directory for ailock's config files.
func ConfigDir() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, "ailock")
}
