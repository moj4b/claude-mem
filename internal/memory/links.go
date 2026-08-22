package memory

import "strings"

// Backlinks are the memories whose bodies link to `name` with [[name]] (§I.5).
// Reported, never rewritten: an unresolved link marks intent to write that
// memory rather than an error, so stripping one would destroy information.
func Backlinks(pool []Memory, name string) []Memory {
	target := MatchKey(name)
	var out []Memory
	for _, m := range pool {
		if MatchKey(m.Name) == target {
			continue // a memory's own self-reference is not inbound
		}
		if linksTo(m.Body, target) {
			out = append(out, m)
		}
	}
	return out
}

// linksTo reports whether body carries a [[link]] resolving to target. Matching
// runs through MatchKey so `[[Foo.md]]` and `[[foo]]` agree, the same
// normalization `read` resolves names with (§II.8).
func linksTo(body, target string) bool {
	rest := body
	for {
		open := strings.Index(rest, "[[")
		if open < 0 {
			return false
		}
		rest = rest[open+2:]
		end := strings.Index(rest, "]]")
		if end < 0 {
			return false
		}
		if MatchKey(strings.TrimSpace(rest[:end])) == target {
			return true
		}
		rest = rest[end+2:]
	}
}
