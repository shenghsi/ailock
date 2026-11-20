//go:build windows

package i18n

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// detectChinese asks the OS for the preferred UI languages (BCP-47 tags
// like "zh-CN") — Windows rarely sets LANG/LC_* — then falls back to the
// environment.
func detectChinese() bool {
	if langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME); err == nil {
		for _, l := range langs {
			if strings.HasPrefix(strings.ToLower(l), "zh") {
				return true
			}
		}
	}
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		if strings.HasPrefix(strings.ToLower(os.Getenv(k)), "zh") {
			return true
		}
	}
	return false
}
