package i18n

import "testing"

func TestTFollowsLocale(t *testing.T) {
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LANGUAGE", "")
	if got := T("english", "中文"); got != "english" {
		t.Fatalf("en locale: T = %q", got)
	}

	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	if got := T("english", "中文"); got != "中文" {
		t.Fatalf("zh_CN locale: T = %q", got)
	}

	t.Setenv("LC_ALL", "")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LANGUAGE", "zh_CN:en")
	if got := T("english", "中文"); got != "中文" {
		t.Fatalf("LANGUAGE=zh_CN:en: T = %q", got)
	}
}
