package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
)

func TestAssetName(t *testing.T) {
	cases := map[[2]string]string{
		{"linux", "amd64"}:   "ailock-linux-amd64",
		{"linux", "mipsle"}:  "ailock-linux-mipsle",
		{"windows", "amd64"}: "ailock-windows-amd64.exe",
		{"darwin", "arm64"}:  "ailock-darwin-arm64",
	}
	for in, want := range cases {
		if got := AssetName(in[0], in[1]); got != want {
			t.Errorf("AssetName(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		tag, current string
		want         bool
	}{
		{"v0.3.0", "0.2.0", true},
		{"v0.2.1", "0.2.0", true},
		{"v0.10.0", "0.9.3", true}, // numeric, not lexicographic
		{"v1.0.0", "0.9.9", true},
		{"v0.2.0", "0.2.0", false},
		{"0.2.0", "0.2.0", false}, // bare, no v
		{"v0.1.9", "0.2.0", false},
		{"v0.2.0-rc1", "0.2.0", false},
		{"v0.2", "0.2.0", false}, // short tags are not newer
		{"v0.3", "0.2.9", true},
	}
	for _, c := range cases {
		if got := Newer(c.tag, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.tag, c.current, got, c.want)
		}
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("fake binary")
	sum := sha256.Sum256(data)
	good := hex.EncodeToString(sum[:])
	sums := []byte(fmt.Sprintf("1111  other-file\n%s  ailock-linux-amd64\n%s *ailock-linux-arm64\n", good, good))

	if err := VerifyChecksum(sums, "ailock-linux-amd64", data); err != nil {
		t.Errorf("text-mode entry: %v", err)
	}
	if err := VerifyChecksum(sums, "ailock-linux-arm64", data); err != nil {
		t.Errorf("binary-mode entry: %v", err)
	}
	if err := VerifyChecksum(sums, "ailock-darwin-arm64", data); err == nil {
		t.Error("unlisted asset passed")
	}
	if err := VerifyChecksum(sums, "ailock-linux-amd64", []byte("tampered")); err == nil {
		t.Error("mismatched data passed")
	}
}
