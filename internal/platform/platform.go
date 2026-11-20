// Package platform holds per-OS paths and helpers. The Go files in this
// directory carry build tags for the OS-specific parts; this file only
// carries the OS-neutral dnsmasq address-file management, which is plain
// file I/O wherever dnsmasq happens to run.
package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const dnsmasqFileName = "ailock.conf"

// SetDnsmasqBlocked writes address=/domain/ wildcard lines for every
// domain into dir/ailock.conf (removing any previous file first), giving
// dnsmasq true wildcard-subdomain coverage that hosts files cannot offer.
func SetDnsmasqBlocked(dir string, domains []string) error {
	if dir == "" {
		return nil
	}
	if err := checkDir(dir); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# BEGIN ailock (managed) - do not edit; ailock rewrites this file\n")
	for _, d := range domains {
		fmt.Fprintf(&b, "address=/%s/\n", d)
	}
	b.WriteString("# END ailock (managed)\n")
	return os.WriteFile(filepath.Join(dir, dnsmasqFileName), []byte(b.String()), 0o644)
}

// RemoveDnsmasqBlocked deletes the ailock dnsmasq address file if present.
func RemoveDnsmasqBlocked(dir string) error {
	if dir == "" {
		return nil
	}
	err := os.Remove(filepath.Join(dir, dnsmasqFileName))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// DnsmasqBlocked reports whether the ailock address file exists.
func DnsmasqBlocked(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, dnsmasqFileName))
	return err == nil
}

func checkDir(dir string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("dnsmasq conf dir %s unusable: %w", dir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("dnsmasq conf dir %s is not a directory", dir)
	}
	return nil
}
