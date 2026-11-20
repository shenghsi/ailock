package blocklist

import (
	"strings"
	"testing"
)

func TestParseSectionsAndOrder(t *testing.T) {
	l, err := Parse([]byte(`
# leading comment
[openai]
OpenAI.com
chatgpt.com

[google]
google.com
# interlude
GOOGLE.com  # duplicate in different case — deduped
youtube.com
`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"openai.com", "chatgpt.com", "google.com", "youtube.com"}
	got := l.Domains()
	if len(got) != len(want) {
		t.Fatalf("domains = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("domains[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if len(l.Sections["openai"]) != 2 || len(l.Sections["google"]) != 2 {
		t.Fatalf("sections = %#v", l.Sections)
	}
}

func TestParseRejectsNonDomains(t *testing.T) {
	for _, bad := range []string{
		"https://openai.com",
		"*.openai.com",
		"openai com",
		"localhost",
		"address=/x/",
	} {
		if _, err := Parse([]byte("[s]\n" + bad + "\n")); err == nil {
			t.Errorf("Parse accepted %q", bad)
		}
	}
}

func TestParseEmpty(t *testing.T) {
	if _, err := Parse([]byte("# only comments\n\n")); err == nil {
		t.Error("Parse accepted an empty list")
	}
}

func TestParseCRLF(t *testing.T) {
	l, err := Parse([]byte("[s]\r\na.com\r\nb.com\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if l.Len() != 2 || len(l.Domains()[0]) != len("a.com") || strings.Contains(l.Domains()[0], "\r") {
		t.Fatalf("CRLF parse broken: %#v", l.Domains())
	}
}
