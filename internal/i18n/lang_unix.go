//go:build !windows

package i18n

import (
	"os"
	"strings"
)

// Chinese reports whether the user's locale is Chinese (zh*), checking
// the standard environment variables in precedence order.
func Chinese() bool {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		if strings.HasPrefix(strings.ToLower(os.Getenv(k)), "zh") {
			return true
		}
	}
	return false
}
