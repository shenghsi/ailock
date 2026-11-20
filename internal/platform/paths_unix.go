//go:build !windows

package platform

// HostsPath returns the OS hosts file path.
func HostsPath() string { return "/etc/hosts" }

// ConfigDir returns the default directory for ailock's config files.
func ConfigDir() string { return "/etc/ailock" }
