//go:build windows

package platform

import "os/exec"

// FlushDNS clears the Windows DNS Client cache.
func FlushDNS() error {
	return exec.Command("ipconfig", "/flushdns").Run()
}
