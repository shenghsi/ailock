// Package blocklist parses the plain-text domain blocklist used by ailock.
//
// Format: one domain per line; blank lines and #-comments are ignored;
// optional [section] headers group domains for readability. Domains are
// bare hosts (no scheme, no wildcard) because hosts files support neither.
package blocklist

import (
	"fmt"
	"strings"
)

// List is a parsed blocklist.
type List struct {
	Sections map[string][]string
	domains  []string
	index    map[string]bool
}

// Parse parses blocklist data.
func Parse(data []byte) (*List, error) {
	l := &List{Sections: map[string][]string{}, index: map[string]bool{}}
	section := ""
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		// Strip trailing comments ("domain  # note"). Domains can never
		// contain '#', so cutting at the first one is safe.
		if j := strings.IndexByte(line, '#'); j >= 0 {
			line = strings.TrimSpace(line[:j])
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			if section == "" {
				return nil, fmt.Errorf("blocklist line %d: empty section header", i+1)
			}
			if _, ok := l.Sections[section]; !ok {
				l.Sections[section] = nil
			}
			continue
		}
		d := strings.ToLower(line)
		if err := validDomain(d); err != nil {
			return nil, fmt.Errorf("blocklist line %d: %w", i+1, err)
		}
		if !l.index[d] {
			l.index[d] = true
			l.domains = append(l.domains, d)
			l.Sections[section] = append(l.Sections[section], d)
		}
	}
	if len(l.domains) == 0 {
		return nil, fmt.Errorf("blocklist contains no domains")
	}
	return l, nil
}

// Domains returns the deduplicated domains in file order.
func (l *List) Domains() []string { return append([]string(nil), l.domains...) }

// Len returns the number of unique domains.
func (l *List) Len() int { return len(l.domains) }

func validDomain(d string) error {
	if !strings.Contains(d, ".") {
		return fmt.Errorf("%q has no dot", d)
	}
	for _, r := range d {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-':
		default:
			return fmt.Errorf("%q is not a bare domain", d)
		}
	}
	return nil
}
