//go:build darwin

package platform

import "os/exec"

// FlushDNS flushes the macOS resolver (DirectoryService cache + mDNSResponder).
func FlushDNS() error {
	_ = exec.Command("dscacheutil", "-flushcache").Run()
	_ = exec.Command("killall", "-HUP", "mDNSResponder").Run()
	return nil
}
