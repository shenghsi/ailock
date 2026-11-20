//go:build !windows

package i18n

import (
	"os"
	"os/exec"
	"strings"
)

func detectChinese() bool {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		if strings.HasPrefix(strings.ToLower(os.Getenv(k)), "zh") {
			return true
		}
	}
	// OpenWrt: the busybox environment carries no locale variables, so
	// follow the LuCI web UI language instead (e.g. "zh_cn" from
	// `uci get luci.main.lang`).
	if _, err := os.Stat("/etc/openwrt_release"); err == nil {
		out, err := exec.Command("uci", "-q", "get", "luci.main.lang").Output()
		if err == nil && strings.HasPrefix(strings.ToLower(strings.TrimSpace(string(out))), "zh") {
			return true
		}
	}
	return false
}
