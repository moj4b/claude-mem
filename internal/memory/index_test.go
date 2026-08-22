package memory

import "testing"

func TestRemoveIndexEntryDropsOnlyTheMatchingLine(t *testing.T) {
	// The real index shape (§I.3): a heading, then one list line per memory.
	const before = `# Memory

- [Public repo](feedback_no_local_paths.md) — scrub local paths before any push.
- [Short commits](feedback_short_commit.md) — one-line commits, terse PR bodies.
`
	const want = `# Memory

- [Short commits](feedback_short_commit.md) — one-line commits, terse PR bodies.
`
	got, removed := RemoveIndexEntry([]byte(before), "feedback_no_local_paths.md")
	if !removed {
		t.Fatal("removed = false, want true — the entry was there")
	}
	if string(got) != want {
		t.Errorf("index =\n%q\nwant\n%q", got, want)
	}
}

func TestRemoveIndexEntryLeavesAnIndexWithoutTheEntryUntouched(t *testing.T) {
	// Index drift is normal (§II.4): a memory may have no line at all. The
	// file must come back byte-identical so nothing is silently reformatted.
	const before = "# Memory\n\n- [Short commits](feedback_short_commit.md) — terse.\n"
	got, removed := RemoveIndexEntry([]byte(before), "project_roadmap.md")
	if removed {
		t.Error("removed = true, want false — no entry pointed at that file")
	}
	if string(got) != before {
		t.Errorf("index =\n%q\nwant it unchanged\n%q", got, before)
	}
}

func TestRemoveIndexEntryDropsEveryDuplicateLine(t *testing.T) {
	// Leaving one of two dangling pointers behind would be the bug this
	// command exists to prevent.
	const before = `# Memory

- [Roadmap](project_roadmap.md) — first copy.
- [Short commits](feedback_short_commit.md) — terse.
- [Roadmap again](project_roadmap.md) — a stale duplicate.
`
	const want = `# Memory

- [Short commits](feedback_short_commit.md) — terse.
`
	got, removed := RemoveIndexEntry([]byte(before), "project_roadmap.md")
	if !removed {
		t.Fatal("removed = false, want true")
	}
	if string(got) != want {
		t.Errorf("index =\n%q\nwant\n%q", got, want)
	}
}

func TestRemoveIndexEntryOnlyTouchesListLines(t *testing.T) {
	// A link is not a pointer unless it is a bullet. Prose that mentions the
	// memory, and any fenced example, must survive intact — deleting a whole
	// sentence because it happened to start with a link is data loss.
	const before = "# Memory\n\n" +
		"[Roadmap](project_roadmap.md) is the planning doc, see it for context.\n" +
		"```\n" +
		"- [Roadmap](project_roadmap.md) — an example of the index shape.\n" +
		"```\n" +
		"- [Roadmap](project_roadmap.md) — rename to Beacon.\n" +
		"* [Roadmap](project_roadmap.md) — a star bullet counts too.\n" +
		"Trailing prose about [Roadmap](project_roadmap.md) and other things.\n"
	const want = "# Memory\n\n" +
		"[Roadmap](project_roadmap.md) is the planning doc, see it for context.\n" +
		"```\n" +
		"- [Roadmap](project_roadmap.md) — an example of the index shape.\n" +
		"```\n" +
		"Trailing prose about [Roadmap](project_roadmap.md) and other things.\n"
	got, removed := RemoveIndexEntry([]byte(before), "project_roadmap.md")
	if !removed {
		t.Fatal("removed = false, want true — there were two bullet entries")
	}
	if string(got) != want {
		t.Errorf("index =\n%q\nwant\n%q", got, want)
	}
}

func TestRemoveIndexEntryIgnoresAnUnclosedFence(t *testing.T) {
	// A stray unterminated fence — a pasted snippet, say — must not make every
	// entry below it invisible to the rewrite. Skipping a region only counts
	// when the region actually closes; otherwise the pointer this command
	// exists to remove would silently survive its memory.
	const before = "# Memory\n\n" +
		"Someone pasted a snippet:\n" +
		"```sh\n" +
		"mem list\n" +
		"- [Roadmap](project_roadmap.md) — rename to Beacon.\n" +
		"- [Prefs](user_prefs.md) — terse.\n"
	const want = "# Memory\n\n" +
		"Someone pasted a snippet:\n" +
		"```sh\n" +
		"mem list\n" +
		"- [Prefs](user_prefs.md) — terse.\n"
	got, removed := RemoveIndexEntry([]byte(before), "project_roadmap.md")
	if !removed {
		t.Fatal("removed = false, want true — the fence never closed")
	}
	if string(got) != want {
		t.Errorf("index =\n%q\nwant\n%q", got, want)
	}
}

func TestRemoveIndexEntryRespectsTildeFences(t *testing.T) {
	// CommonMark allows ~~~ as well as ```.
	const before = "# Memory\n\n~~~\n- [Roadmap](project_roadmap.md) — an example.\n~~~\n" +
		"- [Roadmap](project_roadmap.md) — the real pointer.\n"
	const want = "# Memory\n\n~~~\n- [Roadmap](project_roadmap.md) — an example.\n~~~\n"
	got, removed := RemoveIndexEntry([]byte(before), "project_roadmap.md")
	if !removed {
		t.Fatal("removed = false, want true")
	}
	if string(got) != want {
		t.Errorf("index =\n%q\nwant\n%q", got, want)
	}
}

func TestPointsAtSeesEntriesTheRewriteDeliberatelyLeaves(t *testing.T) {
	// The rewrite only removes bullet lines, but a bare link is still read as
	// an entry when the index is parsed. That gap must be detectable, so the
	// command can say the pointer survived rather than implying it went.
	bare := []byte("# Memory\n\n[Roadmap](project_roadmap.md) — a bare link, no bullet.\n")
	if !PointsAt(bare, "project_roadmap.md") {
		t.Error("PointsAt = false on a bare link, want true — parseIndex reads it as an entry")
	}
	// Inside a closed fence it is an example, not a pointer.
	fencedOnly := []byte("# Memory\n\n```\n- [Roadmap](project_roadmap.md) — example.\n```\n")
	if PointsAt(fencedOnly, "project_roadmap.md") {
		t.Error("PointsAt = true on a fenced example, want false")
	}
	if PointsAt([]byte("# Memory\n\n- [Prefs](user_prefs.md) — terse.\n"), "project_roadmap.md") {
		t.Error("PointsAt = true with no matching entry, want false")
	}
}
