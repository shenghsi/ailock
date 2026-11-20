package i18n

import (
	"sync"
	"testing"
)

// resetForTest clears the per-process detection cache and forced language
// so each case starts clean.
func resetForTest() {
	detectOnce = sync.Once{}
	isChinese = false
	SetForced("auto")
}

func TestTFollowsLocale(t *testing.T) {
	resetForTest()
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LANGUAGE", "")
	if got := T("english", "中文"); got != "english" {
		t.Fatalf("en locale: T = %q", got)
	}

	resetForTest()
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	if got := T("english", "中文"); got != "中文" {
		t.Fatalf("zh_CN locale: T = %q", got)
	}

	resetForTest()
	t.Setenv("LC_ALL", "")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LANGUAGE", "zh_CN:en")
	if got := T("english", "中文"); got != "中文" {
		t.Fatalf("LANGUAGE=zh_CN:en: T = %q", got)
	}
}

func TestForcedOverridesLocale(t *testing.T) {
	resetForTest()
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	SetForced("en")
	if got := T("english", "中文"); got != "english" {
		t.Fatalf("forced en under zh locale: T = %q", got)
	}

	resetForTest()
	t.Setenv("LC_ALL", "en_US.UTF-8")
	for _, v := range []string{"zh", "zh-cn", "zh_cn", "Chinese"} {
		SetForced(v)
		if got := T("english", "中文"); got != "中文" {
			t.Fatalf("forced %q under en locale: T = %q", v, got)
		}
	}

	// Unknown values fall back to auto (en locale here).
	SetForced("klingon")
	if got := T("english", "中文"); got != "english" {
		t.Fatalf("forced garbage under en locale: T = %q", got)
	}
}

func TestSetForcedNormalizes(t *testing.T) {
	cases := map[string]int32{
		"":       0,
		"auto":   0,
		" AUTO ": 0,
		"junk":   0,
		"en":     1,
		" EN ":   1,
		"zh":     2,
		"zh_cn":  2,
	}
	for in, want := range cases {
		SetForced(in)
		if got := forced.Load(); got != want {
			t.Errorf("SetForced(%q): forced = %d, want %d", in, got, want)
		}
	}
}
