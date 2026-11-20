package hostsedit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const existing = `127.0.0.1 localhost
::1 localhost
192.168.1.10 nas.lan
`

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSetBlockedThenRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	write(t, path, existing)

	if IsBlocked(path) {
		t.Fatal("fresh file must not be blocked")
	}
	if err := SetBlocked(path, []string{"a.com", "b.com"}); err != nil {
		t.Fatal(err)
	}
	if !IsBlocked(path) {
		t.Fatal("must be blocked after SetBlocked")
	}
	if n := BlockedDomains(path); n != 2 {
		t.Fatalf("BlockedDomains = %d, want 2", n)
	}
	got := read(t, path)
	for _, want := range []string{
		"127.0.0.1 localhost\n",
		"192.168.1.10 nas.lan\n",
		BeginMarker,
		EndMarker,
		"0.0.0.0 a.com",
		":: a.com",
		"0.0.0.0 b.com",
		":: b.com",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("hosts after block missing %q:\n%s", want, got)
		}
	}
	if !(strings.Index(got, BeginMarker) < strings.Index(got, "0.0.0.0 a.com") &&
		strings.Index(got, "0.0.0.0 b.com") < strings.Index(got, EndMarker)) {
		t.Fatalf("marker ordering wrong:\n%s", got)
	}

	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	if IsBlocked(path) {
		t.Fatal("must not be blocked after Remove")
	}
	if got := read(t, path); got != existing {
		t.Fatalf("after Remove hosts = %q, want original %q", got, existing)
	}
}

func TestSetBlockedReplacesRegionKeepsForeignLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	write(t, path, existing)
	if err := SetBlocked(path, []string{"a.com"}); err != nil {
		t.Fatal(err)
	}
	if err := SetBlocked(path, []string{"x.com", "y.com", "z.com"}); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if strings.Contains(got, "a.com") {
		t.Fatalf("old domain still present:\n%s", got)
	}
	if n := BlockedDomains(path); n != 3 {
		t.Fatalf("BlockedDomains = %d, want 3", n)
	}
	if !strings.Contains(got, "192.168.1.10 nas.lan\n") {
		t.Fatalf("foreign line lost:\n%s", got)
	}
	if strings.Count(got, BeginMarker) != 1 || strings.Count(got, EndMarker) != 1 {
		t.Fatalf("expected exactly one region:\n%s", got)
	}
}

func TestSetBlockedMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	if err := SetBlocked(path, []string{"a.com"}); err != nil {
		t.Fatal(err)
	}
	if !IsBlocked(path) {
		t.Fatal("must be blocked")
	}
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should still exist (or not, but stat err %v)", err)
	}
}

func TestCRLFPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	crlf := strings.ReplaceAll(existing, "\n", "\r\n")
	write(t, path, crlf)

	if err := SetBlocked(path, []string{"a.com"}); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if !strings.Contains(got, "127.0.0.1 localhost\r\n") {
		t.Fatalf("CRLF style not preserved:\n%q", got)
	}
	if !strings.Contains(got, "0.0.0.0 a.com\r\n") {
		t.Fatalf("region not CRLF:\n%q", got)
	}
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != crlf {
		t.Fatalf("after Remove = %q, want %q", got, crlf)
	}
}

func TestNoFinalNewlineHandled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	write(t, path, strings.TrimSuffix(existing, "\n"))

	if err := SetBlocked(path, []string{"a.com"}); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if !strings.HasSuffix(got, EndMarker+"\n") {
		t.Fatalf("region should end with newline:\n%q", got)
	}
	if !strings.HasPrefix(got, "127.0.0.1 localhost\n") {
		t.Fatalf("prefix mangled:\n%q", got)
	}
}

func TestRemoveWithoutMarkersIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	write(t, path, existing)
	before, _ := os.Stat(path)
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("file was rewritten although no markers present")
	}
}

func TestCorruptRegionRecovered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	write(t, path, existing+BeginMarker+"\n0.0.0.0 old.com\n") // end marker lost
	if !IsBlocked(path) {
		t.Fatal("corrupt region should still count as blocked")
	}
	if err := SetBlocked(path, []string{"new.com"}); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if strings.Contains(got, "old.com") {
		t.Fatalf("stale corrupt region not replaced:\n%s", got)
	}
	if n := BlockedDomains(path); n != 1 {
		t.Fatalf("BlockedDomains = %d, want 1", n)
	}
}
