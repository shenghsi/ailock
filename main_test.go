package main

import (
	"os"
	"path/filepath"
	"testing"
)

// copySelf must atomically replace the destination — including replacing
// a running binary, which is the update path (`ailock install`).
func TestCopySelfReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "ailock")

	if err := copySelf(dest); err != nil {
		t.Fatalf("copySelf: %v", err)
	}
	self, _ := os.Executable()
	want, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || len(got) != len(want) {
		t.Fatalf("copied %d bytes, running binary is %d", len(got), len(want))
	}
	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Fatalf("dest mode = %v, want 0755", fi.Mode().Perm())
	}

	// Overwriting an existing dest must work (update-in-place) and leave
	// no temp files behind.
	if err := copySelf(dest); err != nil {
		t.Fatalf("copySelf over existing: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "ailock" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("temp files left behind: %v", names)
	}
}
