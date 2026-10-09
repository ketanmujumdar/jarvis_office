package domain

import "strings"

// reservedTLDs are top-level domains reserved by RFC 2606/6761. Reap rejects emails on them
// (POST /agentic/enrollments answers 400 AGENTIC_REQUEST_REJECTED), and quote shipping
// contacts are emails too, so they are refused before any Reap call.
var reservedTLDs = []string{"example", "test", "invalid", "localhost", "local"}

// ReservedEmailDomain reports whether email's domain ends in a reserved TLD (e.g.
// "a@corp.example"). It is a cheap offline check only: Reap quotes also fail (503
// QUOTE_TEMPORARILY_UNAVAILABLE) when the email's domain does not exist in DNS, which is why the
// seed data uses @example.com (registered) rather than an invented subdomain.
func ReservedEmailDomain(email string) bool {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(email[at+1:])), ".")
	tld := host
	if i := strings.LastIndex(host, "."); i >= 0 {
		tld = host[i+1:]
	}
	for _, r := range reservedTLDs {
		if tld == r {
			return true
		}
	}
	return false
}
