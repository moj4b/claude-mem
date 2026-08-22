package memory

import "strings"

// RemoveIndexEntry drops the MEMORY.md list line pointing at `file`, returning
// the rewritten index and whether anything was removed. Every other byte
// survives verbatim: the index is a file a human and an LLM both edit, so this
// deletes a line and never reformats one (§I.3).
func RemoveIndexEntry(data []byte, file string) ([]byte, bool) {
	lines := strings.Split(string(data), "\n")
	fenced := fencedLines(lines)
	out := make([]string, 0, len(lines))
	removed := false
	for i, line := range lines {
		if !fenced[i] && indexLineFile(line) == file {
			removed = true
			continue
		}
		out = append(out, line)
	}
	if !removed {
		return data, false
	}
	return []byte(strings.Join(out, "\n")), true
}

// PointsAt reports whether the index still carries a line pointing at file,
// outside any fenced block — with or without a bullet, which is how parseIndex
// reads it (§I.3).
//
// RemoveIndexEntry is deliberately stricter than that: it will not delete a
// line that is not a bullet, because such a line is usually prose. The gap
// between the two is exactly the set of pointers that survive a removal, and
// this is what lets the caller say so instead of implying the pointer went.
func PointsAt(data []byte, file string) bool {
	lines := strings.Split(string(data), "\n")
	fenced := fencedLines(lines)
	for i, line := range lines {
		if fenced[i] {
			continue
		}
		if linkTarget(strings.TrimSpace(line)) == file {
			return true
		}
	}
	return false
}

// fencedLines marks the lines sitting inside a *closed* fenced block. A fenced
// block is an example of the index rather than the index, so entries inside
// one are left alone.
//
// Only closed fences count. A stray opener — a snippet someone pasted and
// never terminated — would otherwise hide every entry below it from the
// rewrite, and hiding an entry means leaving a pointer to a memory that has
// just been deleted: silently, and beyond the reach of running the command
// again.
func fencedLines(lines []string) []bool {
	mask := make([]bool, len(lines))
	for i := 0; i < len(lines); i++ {
		open := fenceMarker(lines[i])
		if open == "" {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if fenceMarker(lines[j]) != open {
				continue
			}
			for k := i; k <= j; k++ {
				mask[k] = true
			}
			i = j
			break
		}
		// Unclosed: nothing is masked, and scanning simply carries on.
	}
	return mask
}

// fenceMarker is the fence a line opens or closes with, or "" if it is not a
// fence line. CommonMark allows both backtick and tilde fences.
func fenceMarker(line string) string {
	t := strings.TrimSpace(line)
	for _, m := range []string{"```", "~~~"} {
		if strings.HasPrefix(t, m) {
			return m
		}
	}
	return ""
}

// indexLineFile is the file an index list line points at, or "" if the line is
// not one. Shape is `- [<Title>](<file.md>) — <hook>` (§I.3).
//
// The bullet is required, unlike in parseIndex, which tolerates a bare link.
// The asymmetry is deliberate and only ever errs toward keeping bytes: reading
// a hook from a prose line costs nothing, whereas deleting a sentence because
// it opened with a link destroys it.
func indexLineFile(raw string) string {
	line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
	if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "*") {
		return ""
	}
	return linkTarget(strings.TrimSpace(line[1:]))
}

// linkTarget is the target of a markdown link opening the given text, or "".
func linkTarget(line string) string {
	if !strings.HasPrefix(line, "[") {
		return ""
	}
	close := strings.Index(line, "](")
	if close < 0 {
		return ""
	}
	rest := line[close+2:]
	end := strings.Index(rest, ")")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}
