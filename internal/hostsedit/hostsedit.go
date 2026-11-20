// Package hostsedit maintains a marked region of an OS hosts file that
// sinkholes a set of domains. Everything outside the markers is preserved
// byte-for-byte; the file's line ending style (LF or CRLF) is kept.
package hostsedit

import (
	"bytes"
	"os"
	"strings"
)

const (
	// BeginMarker starts the managed region. The long suffix makes it
	// unlikely anyone edits the block by hand without noticing.
	BeginMarker = "# BEGIN ailock (managed) - do not edit; ailock rewrites this block"
	EndMarker   = "# END ailock (managed)"

	ipv4Sink = "0.0.0.0"
	ipv6Sink = "::"
)

// SetBlocked (re)writes the managed region so every domain resolves to the
// sinkhole addresses. Idempotent; foreign lines are left untouched.
func SetBlocked(path string, domains []string) error {
	var b bytes.Buffer
	b.WriteString(BeginMarker)
	b.WriteString("\n")
	for _, d := range domains {
		b.WriteString(ipv4Sink + " " + d + "\n")
		b.WriteString(ipv6Sink + " " + d + "\n")
	}
	b.WriteString(EndMarker)
	return replaceRegion(path, b.String())
}

// Remove deletes the managed region if present.
func Remove(path string) error { return replaceRegion(path, "") }

// IsBlocked reports whether the managed region is currently present.
func IsBlocked(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	lines, _, _ := parse(data)
	bi, ei := findMarkers(lines)
	return bi >= 0 && ei >= bi
}

// BlockedDomains returns the domains currently sinkholed by the managed
// region (half the region's line count: each domain has v4+v6 lines).
func BlockedDomains(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	lines, _, _ := parse(data)
	bi, ei := findMarkers(lines)
	if bi < 0 || ei < bi {
		return 0
	}
	n := 0
	for _, ln := range lines[bi+1 : ei] {
		if strings.HasPrefix(ln, ipv4Sink+" ") {
			n++
		}
	}
	return n
}

// parse splits file data into lines (LF-normalized), detecting the EOL
// style and whether the file ended with a newline.
func parse(data []byte) (lines []string, eol string, finalNL bool) {
	s := string(data)
	eol = "\n"
	if strings.Contains(s, "\r\n") {
		eol = "\r\n"
		s = strings.ReplaceAll(s, "\r\n", "\n")
	}
	finalNL = s == "" || strings.HasSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil, eol, finalNL
	}
	return strings.Split(s, "\n"), eol, finalNL
}

func render(lines []string, eol string, finalNL bool) []byte {
	s := strings.Join(lines, "\n")
	if finalNL && s != "" {
		s += "\n"
	}
	if eol == "\r\n" {
		s = strings.ReplaceAll(s, "\n", "\r\n")
	}
	return []byte(s)
}

func findMarkers(lines []string) (begin, end int) {
	begin, end = -1, -1
	for i, ln := range lines {
		if ln == BeginMarker && begin == -1 {
			begin = i
		} else if ln == EndMarker && begin >= 0 {
			end = i
			return
		}
	}
	// A begin without an end means a corrupt region (should not happen:
	// writes are atomic). Treat everything from begin to EOF as the region.
	if begin >= 0 {
		end = len(lines) - 1
	}
	return
}

// replaceRegion swaps the managed region in path for region ("" removes it).
// The file is only rewritten when the resulting bytes differ.
func replaceRegion(path, region string) error {
	data, err := os.ReadFile(path)
	var lines []string
	eol := "\n"
	finalNL := true
	switch {
	case err == nil:
		lines, eol, finalNL = parse(data)
	case os.IsNotExist(err):
		// start fresh
	default:
		return err
	}

	regionLines := strings.Split(strings.TrimSuffix(region, "\n"), "\n")
	if region == "" {
		regionLines = nil
	}

	bi, ei := findMarkers(lines)
	var out []string
	switch {
	case bi >= 0 && len(regionLines) > 0: // replace the existing region
		out = append(out, lines[:bi]...)
		out = append(out, regionLines...)
		out = append(out, lines[ei+1:]...)
	case bi >= 0: // remove the region (+ the separator blank we add above it)
		start := bi
		if start > 0 && lines[start-1] == "" {
			start--
		}
		out = append(out, lines[:start]...)
		out = append(out, lines[ei+1:]...)
	case len(regionLines) == 0: // nothing to remove, nothing to write
		return nil
	default: // append, separated by a blank line
		out = append(out, lines...)
		if len(out) == 0 || out[len(out)-1] != "" {
			out = append(out, "")
		}
		out = append(out, regionLines...)
		finalNL = true // an appended region always ends the file with a newline
	}

	outBytes := render(out, eol, finalNL)
	if bytes.Equal(outBytes, data) {
		return nil
	}
	mode := os.FileMode(0o644)
	if fi, statErr := os.Stat(path); statErr == nil {
		mode = fi.Mode().Perm()
	}
	return writeFile(path, outBytes, mode)
}
