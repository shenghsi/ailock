//go:build windows

package i18n

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// Chinese reports whether the user's UI language is Chinese. Windows
// rarely sets LANG/LC_*, so ask the OS for the preferred UI languages
// first (BCP-47 tags like "zh-CN"), then fall back to the environment.
func Chinese() bool {
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
