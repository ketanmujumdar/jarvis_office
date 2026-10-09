// Package allowlist is the runtime merchant allow-list (backend/seed/allowed_merchants.tsv). Only
// merchants on it may be bought from: admin vendor writes, policy and checkout all check it, so a
// vendor row alone (editable by anyone who can reach the fake-login admin page) is not enough.
package allowlist

import (
	"bufio"
	"strings"
	"sync"

	seedfiles "github.com/ketanmujumdar/jarvis_office/backend/seed"
)

// Set is a set of normalised merchant domains. The zero value allows nothing.
type Set struct{ domains map[string]bool }

// Parse reads a TSV with a "domain" first column (a header row is skipped; blank lines and
// lines starting with # are ignored).
func Parse(tsv string) Set {
	s := Set{domains: map[string]bool{}}
	sc := bufio.NewScanner(strings.NewReader(tsv))
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		d := Normalize(strings.SplitN(line, "\t", 2)[0])
		if first {
			first = false
			if d == "domain" {
				continue
			}
		}
		if d != "" {
			s.domains[d] = true
		}
	}
	return s
}

// Of builds a set from domains (tests).
func Of(domains ...string) Set {
	s := Set{domains: map[string]bool{}}
	for _, d := range domains {
		if d = Normalize(d); d != "" {
			s.domains[d] = true
		}
	}
	return s
}

var (
	defOnce sync.Once
	def     Set
)

// Default is the embedded backend/seed/allowed_merchants.tsv.
func Default() Set {
	defOnce.Do(func() { def = Parse(seedfiles.AllowedMerchantsTSV) })
	return def
}

// Normalize lower-cases a domain and strips scheme, "www." and a trailing slash.
func Normalize(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	d = strings.TrimPrefix(strings.TrimPrefix(d, "https://"), "http://")
	d = strings.TrimSuffix(d, "/")
	return strings.TrimPrefix(d, "www.")
}

// Contains reports whether domain is on the list.
func (s Set) Contains(domain string) bool { return s.domains[Normalize(domain)] }

// Len is the number of domains.
func (s Set) Len() int { return len(s.domains) }
