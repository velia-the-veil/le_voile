//go:build linux

// Package blocklist provides download, parsing, and in-memory management
// of a DNS blocklist in the StevenBlack/hosts format.
package blocklist

import (
	"bytes"
	"strings"
)

// parse reads a hosts-format blocklist and returns a set of blocked domains.
// Lines starting with '#' are ignored. Only lines beginning with "0.0.0.0 "
// are processed; the second field is the domain. Entries for "0.0.0.0",
// "localhost", and "broadcasthost" are excluded. Inline comments (after '#')
// are stripped. Both \n and \r\n line endings are handled.
// The function is tolerant of malformed lines — they are silently ignored.
func parse(data []byte) map[string]struct{} {
	domains := make(map[string]struct{})
	for _, rawLine := range bytes.Split(data, []byte("\n")) {
		line := strings.TrimRight(string(rawLine), "\r")
		line = strings.TrimSpace(line)

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip inline comments.
		if idx := strings.IndexByte(line, '#'); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		// Only process lines redirecting to 0.0.0.0.
		if fields[0] != "0.0.0.0" {
			continue
		}

		domain := fields[1]
		if domain == "" || domain == "0.0.0.0" || domain == "localhost" || domain == "broadcasthost" {
			continue
		}

		domains[domain] = struct{}{}
	}
	return domains
}
