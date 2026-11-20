// Package i18n picks between English and 简体中文 for user-facing
// strings based on the user's locale.
package i18n

// T returns zh when the user's locale is Chinese, otherwise en. Every
// user-facing string in ailock goes through T, so zh-CN users get native
// messages. Contributors: provide both languages inline, e.g.
//
//	fmt.Println(i18n.T("proxy check OK", "代理检测正常"))
func T(en, zh string) string {
	if Chinese() {
		return zh
	}
	return en
}
