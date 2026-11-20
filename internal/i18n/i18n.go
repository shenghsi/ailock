// Package i18n picks between English and 简体中文 for user-facing
// strings based on the user's locale.
package i18n

import (
	"strings"
	"sync"
	"sync/atomic"
)

// forced pins the output language: 0 = auto (locale detection),
// 1 = English, 2 = 简体中文. Set once from the config at startup.
var forced atomic.Int32

// SetForced pins the output language. Accepted values: "auto" (default,
// locale detection), "en"/"english", "zh"/"zh-cn"/"zh_cn"/"chinese".
// Anything else falls back to auto.
func SetForced(lang string) {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "en", "english":
		forced.Store(1)
	case "zh", "zh-cn", "zh_cn", "chinese":
		forced.Store(2)
	default: // "", "auto", anything unknown
		forced.Store(0)
	}
}

var (
	detectOnce sync.Once
	isChinese  bool
)

// Chinese reports whether the user's locale is Chinese. Detected once
// per process (the OpenWrt path shells out to uci).
func Chinese() bool {
	detectOnce.Do(func() { isChinese = detectChinese() })
	return isChinese
}

// T returns zh when the user's locale is Chinese, otherwise en. Every
// user-facing string in ailock goes through T, so zh-CN users get native
// messages. Contributors: provide both languages inline, e.g.
//
//	fmt.Println(i18n.T("proxy check OK", "代理检测正常"))
func T(en, zh string) string {
	switch forced.Load() {
	case 1:
		return en
	case 2:
		return zh
	}
	if Chinese() {
		return zh
	}
	return en
}
