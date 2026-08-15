package memory

import (
	"sort"
	"strings"
)

// Hit is one matching line. A match is a line, not an occurrence, so the
// header counts and the grep-format output agree.
type Hit struct {
	File string
	Line int // 1-based
	Text string
	Col  int // byte offset of the match within Text, for highlighting
	Len  int
}

// Search scans the full raw file including frontmatter, so `mem search
// reference` can match on type, and including MEMORY.md (§II.6). The match is
// a case-insensitive literal substring — predictable, not regex.
func Search(s Store, query string) []Hit {
	q := strings.ToLower(query)
	if q == "" {
		return nil
	}
	var hits []Hit
	for _, m := range s.Readable() {
		for i, line := range strings.Split(strings.ReplaceAll(string(m.Raw), "\r\n", "\n"), "\n") {
			if col := strings.Index(strings.ToLower(line), q); col >= 0 {
				hits = append(hits, Hit{
					File: m.File, Line: i + 1, Text: line, Col: col, Len: len(query),
				})
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].File != hits[j].File {
			return hits[i].File < hits[j].File
		}
		return hits[i].Line < hits[j].Line
	})
	return hits
}

// CountFiles reports how many distinct memories a hit list touches.
func CountFiles(hits []Hit) int {
	seen := map[string]bool{}
	for _, h := range hits {
		seen[h.File] = true
	}
	return len(seen)
}
