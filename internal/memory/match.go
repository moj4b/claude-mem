package memory

import (
	"fmt"
	"sort"
	"strings"
)

// matchKey normalizes both sides of a comparison: lowercase, trailing .md
// stripped (§II.8).
func MatchKey(s string) string {
	return strings.ToLower(strings.TrimSuffix(strings.ToLower(s), ".md"))
}

// matchName resolves a query to exactly one memory using the four tiers of
// §II.8. Within a tier: one hit wins, several is ambiguous, none falls to the
// next tier. Ambiguity is always reported, never guessed, so an exact filename
// can never lose to a coincidental fuzzy hit.
func Match(pool []Memory, query string) (Memory, error) {
	q := MatchKey(query)
	tiers := []struct {
		name  string
		match func(name string) bool
	}{
		{"exact", func(n string) bool { return n == q }},
		{"prefix", func(n string) bool { return strings.HasPrefix(n, q) }},
		{"substring", func(n string) bool { return strings.Contains(n, q) }},
		{"fuzzy", func(n string) bool { return isSubsequence(q, n) }},
	}
	for _, tier := range tiers {
		var hits []Memory
		for _, m := range pool {
			if tier.match(MatchKey(m.Name)) {
				hits = append(hits, m)
			}
		}
		switch len(hits) {
		case 0:
			continue
		case 1:
			return hits[0], nil
		default:
			names := make([]string, len(hits))
			for i, h := range hits {
				names[i] = h.Name
			}
			sort.Strings(names)
			return Memory{}, fmt.Errorf("'%s' is ambiguous — %d memories match\n  %s",
				query, len(hits), strings.Join(names, ", "))
		}
	}
	return Memory{}, fmt.Errorf("no memory matches '%s'%s", query, suggestionLine(pool, q))
}

// suggestionLine offers the three nearest names by edit distance (§II.8).
func suggestionLine(pool []Memory, q string) string {
	if len(pool) == 0 {
		return ""
	}
	type scored struct {
		name string
		d    int
	}
	all := make([]scored, 0, len(pool))
	for _, m := range pool {
		all = append(all, scored{m.Name, levenshtein(q, MatchKey(m.Name))})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].d != all[j].d {
			return all[i].d < all[j].d
		}
		return all[i].name < all[j].name
	})
	n := min(3, len(all))
	names := make([]string, n)
	for i := 0; i < n; i++ {
		names[i] = all[i].name
	}
	return "\n  did you mean: " + strings.Join(names, ", ")
}

// isSubsequence reports whether every rune of q appears in s in order, gaps
// allowed — tier 4 of §II.8.
func isSubsequence(q, s string) bool {
	if q == "" {
		return true
	}
	qr := []rune(q)
	i := 0
	for _, r := range s {
		if r == qr[i] {
			if i++; i == len(qr) {
				return true
			}
		}
	}
	return false
}

// levenshtein is the standard edit distance, used only to rank suggestions.
func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min(min(curr[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}
