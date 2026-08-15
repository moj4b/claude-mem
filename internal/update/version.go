package update

import (
	"strconv"
	"strings"
)

// version is the subset of SemVer the release tags use: `vMAJOR.MINOR.PATCH`
// with an optional `-prerelease` tail. Build metadata after `+` is accepted and
// discarded, because SemVer does not order it.
type version struct {
	num [3]int
	pre string
}

// parseVersion is deliberately strict. Anything that is not a release version —
// "(devel)", "(devel abc123 -dirty)", a distro's own string — must fail here,
// because the whole nag rests on the answer: a build we cannot place in the
// release order is one we must never claim is out of date (§II.12).
func parseVersion(s string) (version, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	var v version
	if i := strings.IndexByte(s, '-'); i >= 0 {
		v.pre, s = s[i+1:], s[:i]
		if v.pre == "" {
			return version{}, false
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		// The round-trip rejects what Atoi is happy to take — "+1", "01" — so
		// two spellings of one number cannot compare unequal.
		if err != nil || n < 0 || p != strconv.Itoa(n) {
			return version{}, false
		}
		v.num[i] = n
	}
	return v, true
}

// IsRelease reports whether v is a comparable release version, which is what
// distinguishes an installed release from a local `go build` (§II.12).
func IsRelease(v string) bool {
	_, ok := parseVersion(v)
	return ok
}

// Newer reports whether latest is strictly newer than current. It is
// conservative on purpose: if either side is not a release version the answer
// is false, so an unstamped build is never told to upgrade and a garbled
// release feed can never trigger one.
func Newer(current, latest string) bool {
	c, okc := parseVersion(current)
	l, okl := parseVersion(latest)
	if !okc || !okl {
		return false
	}
	return compare(c, l) < 0
}

func compare(a, b version) int {
	for i := range a.num {
		if a.num[i] != b.num[i] {
			if a.num[i] < b.num[i] {
				return -1
			}
			return 1
		}
	}
	return comparePre(a.pre, b.pre)
}

// comparePre orders prerelease tails by SemVer §11: a release outranks any
// prerelease of the same number, identifiers compare numerically when both are
// numeric, and a shorter identifier list loses when it is a prefix of a longer
// one. This exists so `v0.2.0-rc.1` never displaces `v0.2.0`.
func comparePre(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		an, aerr := strconv.Atoi(as[i])
		bn, berr := strconv.Atoi(bs[i])
		switch {
		case aerr == nil && berr == nil:
			if an < bn {
				return -1
			}
			return 1
		case aerr == nil:
			return -1 // numeric identifiers rank below alphanumeric ones
		case berr == nil:
			return 1
		case as[i] < bs[i]:
			return -1
		default:
			return 1
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	}
	return 0
}
