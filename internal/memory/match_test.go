package memory

import (
	"strings"
	"testing"
)

func names(ms ...string) []Memory {
	out := make([]Memory, len(ms))
	for i, n := range ms {
		out[i] = Memory{Name: n, File: n + ".md"}
	}
	return out
}

func TestExactBeatsPrefixBeatsSubstringBeatsFuzzy(t *testing.T) {
	// §II.8: an exact filename always beats a coincidental fuzzy hit.
	pool := names("naming", "naming_extra", "project_roadmap", "n_a_m_i_n_g")
	m, err := Match(pool, "naming")
	if err != nil {
		t.Fatalf("matchName: %v", err)
	}
	if m.Name != "naming" {
		t.Errorf("matched %q, want the exact hit %q", m.Name, "naming")
	}
}

func TestPrefixTierUsedWhenNoExactMatch(t *testing.T) {
	m, err := Match(names("project_roadmap", "unrelated"), "project_r")
	if err != nil {
		t.Fatalf("matchName: %v", err)
	}
	if m.Name != "project_roadmap" {
		t.Errorf("matched %q, want %q", m.Name, "project_roadmap")
	}
}

func TestSubstringTierUsedWhenNoPrefixMatch(t *testing.T) {
	// `mem read cache` finds feedback_build_cache at tier 3 (§II.8).
	m, err := Match(names("feedback_build_cache", "user_prefs"), "cache")
	if err != nil {
		t.Fatalf("matchName: %v", err)
	}
	if m.Name != "feedback_build_cache" {
		t.Errorf("matched %q, want %q", m.Name, "feedback_build_cache")
	}
}

func TestSubsequenceFuzzyTierIsLast(t *testing.T) {
	m, err := Match(names("feedback_build_cache", "user_prefs"), "fdd")
	if err != nil {
		t.Fatalf("matchName: %v", err)
	}
	if m.Name != "feedback_build_cache" {
		t.Errorf("matched %q, want the subsequence hit", m.Name)
	}
}

func TestCaseInsensitiveAndMdSuffixAccepted(t *testing.T) {
	for _, q := range []string{"PROJECT_ROADMAP", "project_roadmap.md", "Project_Roadmap.MD"} {
		m, err := Match(names("project_roadmap"), q)
		if err != nil {
			t.Errorf("Match(%q): %v", q, err)
			continue
		}
		if m.Name != "project_roadmap" {
			t.Errorf("Match(%q) = %q, want %q", q, m.Name, "project_roadmap")
		}
	}
}

func TestAmbiguityIsReportedNeverGuessed(t *testing.T) {
	pool := names("project_targets", "project_roadmap", "project_milestones")
	_, err := Match(pool, "project_")
	if err == nil {
		t.Fatal("matchName succeeded on an ambiguous query; it must never guess")
	}
	msg := err.Error()
	if !strings.Contains(msg, "is ambiguous — 3 memories match") {
		t.Errorf("error = %q, want it to state the count", msg)
	}
	for _, n := range []string{"project_targets", "project_roadmap", "project_milestones"} {
		if !strings.Contains(msg, n) {
			t.Errorf("error = %q, want it to list %q", msg, n)
		}
	}
}

func TestAmbiguityInAnEarlyTierDoesNotFallThroughToALaterOne(t *testing.T) {
	// Two prefix hits is ambiguous — it must not silently narrow via fuzzy.
	_, err := Match(names("abc_one", "abc_two", "xyz"), "abc")
	if err == nil {
		t.Fatal("expected ambiguity, got a match")
	}
}

func TestFuzzyTierResolvesNearMissesBeforeGivingUp(t *testing.T) {
	// A near-miss like 'cche' is a not-found by the earlier tiers, but §II.8's
	// tier-4 subsequence rule matches it against feedback_build_cache (c-c-h-e
	// all appear in order). §II.8 is the normative rule, so it resolves.
	m, err := Match(names("feedback_build_cache", "project_targets", "user_prefs"), "cche")
	if err != nil {
		t.Fatalf("Match(cche): %v", err)
	}
	if m.Name != "feedback_build_cache" {
		t.Errorf("matched %q, want %q", m.Name, "feedback_build_cache")
	}
}

func TestNotFoundSuggestsNearestByLevenshtein(t *testing.T) {
	pool := names("feedback_build_cache", "project_targets", "user_prefs")
	_, err := Match(pool, "zzzznope")
	if err == nil {
		t.Fatal("expected not-found")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no memory matches 'zzzznope'") {
		t.Errorf("error = %q, want it to name the query", msg)
	}
	if !strings.Contains(msg, "did you mean:") {
		t.Errorf("error = %q, want suggestions", msg)
	}
	if !strings.Contains(msg, "user_prefs") {
		t.Errorf("error = %q, want the nearest name suggested", msg)
	}
}

func TestSuggestionsCapAtThree(t *testing.T) {
	_, err := Match(names("aaaa", "bbbb", "cccc", "dddd", "eeee"), "zzzz")
	if err == nil {
		t.Fatal("expected not-found")
	}
	line := ""
	for _, l := range strings.Split(err.Error(), "\n") {
		if strings.Contains(l, "did you mean:") {
			line = l
		}
	}
	if n := len(strings.Split(strings.SplitN(line, ":", 2)[1], ",")); n > 3 {
		t.Errorf("suggested %d names, want at most 3", n)
	}
}

func TestLevenshtein(t *testing.T) {
	// Worked examples, independent of the implementation.
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"kitten", "sitting", 3},
		{"flaw", "lawn", 2},
		{"", "abc", 3},
	} {
		if got := levenshtein(tc.a, tc.b); got != tc.want {
			t.Errorf("levenshtein(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
