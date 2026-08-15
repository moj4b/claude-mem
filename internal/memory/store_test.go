package memory

import (
	"strings"
	"testing"
)

func TestFlatFrontmatterShape(t *testing.T) {
	// The flat frontmatter shape, as Claude Code writes it (§I.4).
	m := parseMemory("project_roadmap.md", []byte(`---
name: Roadmap for the next release
description: User considering renaming app from "Courier" to "Beacon" — may affect docs, CLI help, package name
type: project
---

As of 2026-01-15, user is exploring renaming the app.
`))
	if m.Name != "project_roadmap" {
		t.Errorf("Name = %q, want %q — the identifier is the filename (§I.5)", m.Name, "project_roadmap")
	}
	if m.Title != "Roadmap for the next release" {
		t.Errorf("Title = %q, want the frontmatter name", m.Title)
	}
	if m.Type != "project" {
		t.Errorf("Type = %q, want %q", m.Type, "project")
	}
	if !strings.HasPrefix(m.Description, `User considering renaming app from "Courier" to "Beacon"`) {
		t.Errorf("Description = %q", m.Description)
	}
	if !strings.HasPrefix(m.Body, "As of 2026-01-15") {
		t.Errorf("Body = %q, want frontmatter stripped", m.Body)
	}
}

func TestNestedFrontmatterShapeWithEveryRealHazard(t *testing.T) {
	// Every shape a real memory file has thrown at this parser, in one
	// fixture (§I.4): the trailing space after `metadata:`, a double-quoted
	// description containing \" escapes, a ':' inside the value, and
	// non-ASCII.
	m := parseMemory("feedback_verify_before_skipping.md", []byte("---\n"+
		"name: verify-a-skip-before-trusting-it\n"+
		`description: "Before recording \"same failure on both sides → pre-existing → SKIP\" in any comparison run, confirm: the suite actually got past setup."`+"\n"+
		"metadata: \n"+
		"  node_type: memory\n"+
		"  type: feedback\n"+
		"  originSessionId: 00000000-0000-4000-8000-000000000000\n"+
		"---\n\nbody here\n"))

	if m.Type != "feedback" {
		t.Errorf("Type = %q, want %q from metadata.type", m.Type, "feedback")
	}
	if strings.HasPrefix(m.Description, `"`) || strings.HasSuffix(m.Description, `"`) {
		t.Errorf("Description = %q, want the surrounding quotes stripped", m.Description)
	}
	if !strings.Contains(m.Description, `"same failure on both sides → pre-existing → SKIP"`) {
		t.Errorf("Description = %q, want \\\" unescaped to a bare quote", m.Description)
	}
	if !strings.Contains(m.Description, "confirm: the suite") {
		t.Errorf("Description = %q, want the ':' inside the value preserved", m.Description)
	}
	if !strings.Contains(m.Description, "→") {
		t.Errorf("Description = %q, want non-ASCII preserved", m.Description)
	}
	if m.Body != "body here\n" {
		t.Errorf("Body = %q, want %q", m.Body, "body here\n")
	}
}

func TestTypeAtTopLevelBeatsNothingAndNestedIsFallback(t *testing.T) {
	flat := parseMemory("a.md", []byte("---\ntype: user\n---\nx\n"))
	if flat.Type != "user" {
		t.Errorf("flat Type = %q, want %q", flat.Type, "user")
	}
	nested := parseMemory("a.md", []byte("---\nmetadata:\n  type: reference\n---\nx\n"))
	if nested.Type != "reference" {
		t.Errorf("nested Type = %q, want %q", nested.Type, "reference")
	}
}

func TestMissingFrontmatterDegradesInsteadOfCrashing(t *testing.T) {
	// The only real files without frontmatter are the MEMORY.md indexes (§I.4),
	// but the directory is written by an LLM and a human, so never crash (§II.2).
	m := parseMemory("MEMORY.md", []byte("- [User preferences](user_prefs.md) — terse\n"))
	if m.Name != "MEMORY" {
		t.Errorf("Name = %q, want %q", m.Name, "MEMORY")
	}
	if m.Title != "MEMORY" {
		t.Errorf("Title = %q, want the name as fallback", m.Title)
	}
	if m.Type != "" || m.Description != "" {
		t.Errorf("Type = %q, Description = %q, want both empty", m.Type, m.Description)
	}
	if !strings.HasPrefix(m.Body, "- [User preferences]") {
		t.Errorf("Body = %q, want the whole file", m.Body)
	}
}

func TestUnterminatedFenceIsNotTreatedAsFrontmatter(t *testing.T) {
	m := parseMemory("a.md", []byte("---\nname: Dangling\ntype: user\nno closing fence here\n"))
	if m.Title != "Dangling" {
		t.Errorf("Title = %q — parse what is there rather than crashing", m.Title)
	}
	if m.Body == "" {
		t.Error("Body is empty; an unterminated fence must not swallow the file")
	}
}

func TestCRLFInput(t *testing.T) {
	// Not on disk today (§I.4) but cheap to guard.
	m := parseMemory("a.md", []byte("---\r\nname: Windows\r\ntype: user\r\n---\r\n\r\nbody\r\n"))
	if m.Title != "Windows" {
		t.Errorf("Title = %q, want %q — CR must not leak into values", m.Title, "Windows")
	}
	if m.Type != "user" {
		t.Errorf("Type = %q, want %q", m.Type, "user")
	}
}

func TestEmptyFile(t *testing.T) {
	m := parseMemory("a.md", nil)
	if m.Name != "a" || m.Title != "a" {
		t.Errorf("got Name=%q Title=%q, want both %q", m.Name, m.Title, "a")
	}
}

func TestIndexParsingAcceptsEverySeparatorAndAMissingHook(t *testing.T) {
	// §I.3: every observed line uses an em-dash, but accept the variants anyway.
	idx := parseIndex([]byte(strings.Join([]string{
		"- [User preferences](user_prefs.md) — em dash hook",
		"- [Target devices](project_targets.md) – en dash hook",
		"- [Roadmap](project_roadmap.md) - hyphen hook",
		"- [Project status](project_milestones.md) -- double hyphen hook",
		"- [No hook](feedback_build_cache.md)",
		"not a list line at all",
	}, "\n")))

	for file, want := range map[string]string{
		"user_prefs.md":           "em dash hook",
		"project_targets.md":      "en dash hook",
		"project_roadmap.md":      "hyphen hook",
		"project_milestones.md":   "double hyphen hook",
		"feedback_build_cache.md": "",
	} {
		e, ok := idx[file]
		if !ok {
			t.Errorf("%s missing from index", file)
			continue
		}
		if e.hook != want {
			t.Errorf("%s hook = %q, want %q", file, e.hook, want)
		}
	}
	if idx["user_prefs.md"].title != "User preferences" {
		t.Errorf("title = %q, want %q", idx["user_prefs.md"].title, "User preferences")
	}
	if len(idx) != 5 {
		t.Errorf("parsed %d entries, want 5 — the non-list line must be ignored", len(idx))
	}
}

func TestDescriptionPrecedenceFrontmatterThenIndexThenEmpty(t *testing.T) {
	// §II.4: frontmatter description → MEMORY.md hook → "".
	idx := map[string]indexEntry{
		"a.md": {title: "Indexed A", hook: "hook from index"},
		"b.md": {title: "Indexed B", hook: "hook from index"},
	}
	withFM := parseMemory("a.md", []byte("---\ndescription: from frontmatter\n---\nx\n"))
	withFM.applyIndex(idx)
	if withFM.Description != "from frontmatter" {
		t.Errorf("Description = %q, want frontmatter to win", withFM.Description)
	}

	noFM := parseMemory("b.md", []byte("---\nname: B\n---\nx\n"))
	noFM.applyIndex(idx)
	if noFM.Description != "hook from index" {
		t.Errorf("Description = %q, want the index hook", noFM.Description)
	}

	// A file absent from the index keeps an empty description, not a crash.
	orphan := parseMemory("c.md", []byte("---\nname: C\n---\nx\n"))
	orphan.applyIndex(idx)
	if orphan.Description != "" {
		t.Errorf("Description = %q, want empty", orphan.Description)
	}
}

func TestTitleFallsBackToIndexTitleThenName(t *testing.T) {
	idx := map[string]indexEntry{"a.md": {title: "Indexed A", hook: "h"}}
	m := parseMemory("a.md", []byte("x\n")) // no frontmatter at all
	m.applyIndex(idx)
	if m.Title != "Indexed A" {
		t.Errorf("Title = %q, want the index title", m.Title)
	}
	orphan := parseMemory("z.md", []byte("x\n"))
	orphan.applyIndex(idx)
	if orphan.Title != "z" {
		t.Errorf("Title = %q, want the name", orphan.Title)
	}
}
