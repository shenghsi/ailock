//go:build !windows && !darwin

package platform

import "os/exec"

// FlushDNS best-effort flushes every resolver cache we know about:
// systemd-resolved on desktop distros, dnsmasq on OpenWrt/routers.
// Failures are swallowed: a failed cache flush only delays the toggle.
func FlushDNS() error {
	_ = exec.Command("resolvectl", "flush-caches").Run()
	// SIGHUP makes dnsmasq re-read /etc/hosts and its conf-dir.
	_ = exec.Command("sh", "-c",
		"pidof dnsmasq >/dev/null 2>&1 && kill -HUP $(pidof dnsmasq) 2>/dev/null").Run()
	return nil
}
