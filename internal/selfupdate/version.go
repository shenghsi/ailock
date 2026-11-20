package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Newer reports whether release tag (e.g. "v0.10.0") is strictly newer
// than the running version (e.g. "0.9.3"). Only numeric major.minor.patch
// parts are compared; extra segments are ignored.
func Newer(tag, current string) bool {
	a, b := parseVersion(NormalizeTag(tag)), parseVersion(current)
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func parseVersion(v string) [3]int {
	var out [3]int
	for i, part := range strings.SplitN(v, ".", 4)[:3] {
		n, _ := strconv.Atoi(strings.SplitN(part, "-", 2)[0])
		out[i] = n
	}
	return out
}

// VerifyChecksum checks data against the entry for name in sha256sums
// output ("<hash>  <name>", possibly with a binary-mode "*name").
func VerifyChecksum(sums []byte, name string, data []byte) error {
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			want = fields[0]
			break
		}
	}
	if want == "" {
		return fmt.Errorf("%s not listed in SHA256SUMS", name)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("checksum mismatch for %s (got %s, want %s)", name, got, want)
	}
	return nil
}
