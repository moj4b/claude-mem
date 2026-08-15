package resolve

import "testing"

func TestSlugifyMatchesClaudeCodesTransform(t *testing.T) {
	// Both cases are measured ground truth from §I.1, not guesses.
	for _, tc := range []struct{ in, want string }{
		{"/home/you/projects/courier", "-home-you-projects-courier"},
		{"/home/you/projects/photo_lib", "-home-you-projects-photo-lib"},
		{"/home/you/projects/report-gen", "-home-you-projects-report-gen"},
	} {
		if got := Slugify(tc.in); got != tc.want {
			t.Errorf("Slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestProjectNamesKeepCourieresInTheDirectoryName(t *testing.T) {
	// The slug is lossy — '/' and '-' both became '-' — so the human name
	// cannot be "everything after the last dash": that turns claude-mem into
	// "mem" and notes-sync into "server". Strip the prefix the sibling
	// slugs share instead (§II.0, §II.7a).
	got := ProjectNames([]string{
		"-home-you-projects-claude-mem",
		"-home-you-projects-notes-sync",
		"-home-you-projects-report-gen",
		"-home-you-projects-courier",
		"-home-you-projects-sandbox",
	})
	for slug, want := range map[string]string{
		"-home-you-projects-claude-mem": "claude-mem",
		"-home-you-projects-notes-sync": "notes-sync",
		"-home-you-projects-report-gen": "report-gen",
		"-home-you-projects-courier":    "courier",
		"-home-you-projects-sandbox":    "sandbox",
	} {
		if got[slug] != want {
			t.Errorf("projectNames[%q] = %q, want %q", slug, got[slug], want)
		}
	}
}

func TestProjectNamesWithASingleSlugStillYieldsAName(t *testing.T) {
	got := ProjectNames([]string{"-home-you-projects-courier"})
	if got["-home-you-projects-courier"] == "" {
		t.Error("a lone slug produced an empty name")
	}
}

func TestNormCollapsesCourierRunsAndCase(t *testing.T) {
	// norm powers the fallback scan (§II.3 step 3b): it compares our guessed
	// slug against a real on-disk entry, so a directory whose true transform
	// differs on punctuation still matches. Both sides are slugs, never paths.
	guess := Slugify("/home/you/projects/my.app")
	for _, onDisk := range []string{
		"-home-you-projects-my-app",  // '.' became '-', same as our guess
		"-home-you-projects-my--app", // '.' became '--'; runs collapse
		"-home-you-projects-My-App",  // case differs
	} {
		if norm(onDisk) != norm(guess) {
			t.Errorf("norm(%q) = %q, want it to match norm(%q) = %q",
				onDisk, norm(onDisk), guess, norm(guess))
		}
	}
	// Distinct projects must not collide.
	if norm(Slugify("/p/alpha")) == norm(Slugify("/p/beta")) {
		t.Error("norm collapsed two distinct projects together")
	}
}
