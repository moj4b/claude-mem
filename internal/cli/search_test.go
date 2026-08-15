package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/moj4b/claude-mem/internal/memory"
)

func TestSearchIsCaseInsensitiveLiteralSubstring(t *testing.T) {
	dir := memDir(t, map[string]string{
		"a.md": "---\nname: A\ntype: user\n---\n\nuses Docker daily\n",
	})
	for _, q := range []string{"docker", "DOCKER", "DoCkEr"} {
		out, _, code := exec("--dir", dir, "search", q)
		if code != 0 {
			t.Errorf("search %q: exit code = %d, want 0", q, code)
		}
		if !strings.Contains(out, "uses Docker daily") {
			t.Errorf("search %q: stdout = %q, want the matching line", q, out)
		}
	}
}

func TestSearchIsLiteralNotRegex(t *testing.T) {
	// §II.6: predictable literal substring; regex can come later behind a flag.
	dir := memDir(t, map[string]string{
		"a.md": "---\nname: A\n---\n\nliteral a.c here\nand abc too\n",
	})
	out, _, code := exec("--dir", dir, "search", "a.c")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if strings.Contains(out, "abc too") {
		t.Errorf("stdout = %q, want '.' treated literally, not as a regex wildcard", out)
	}
	if !strings.Contains(out, "literal a.c here") {
		t.Errorf("stdout = %q, want the literal match", out)
	}
}

func TestSearchPlainUsesGrepFormatWithOneBasedLineNumbers(t *testing.T) {
	dir := memDir(t, map[string]string{
		"a.md": "---\nname: A\n---\n\nline five has docker\n",
	})
	out, _, _ := exec("--dir", dir, "search", "docker")
	want := "a.md:5:line five has docker\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func TestSearchCoversFrontmatterIncludingType(t *testing.T) {
	// Searching the full raw file means `mem search reference` can match on
	// type (§II.6).
	dir := memDir(t, map[string]string{
		"a.md": fm("A", "d", "reference"),
		"b.md": fm("B", "d", "user"),
	})
	out, _, code := exec("--dir", dir, "search", "type: reference")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "a.md:") {
		t.Errorf("stdout = %q, want the frontmatter hit", out)
	}
	if strings.Contains(out, "b.md:") {
		t.Errorf("stdout = %q, want only the reference-typed file", out)
	}
}

func TestSearchIncludesTheIndex(t *testing.T) {
	dir := memDir(t, map[string]string{
		"a.md":      fm("A", "nothing relevant", "user"),
		"MEMORY.md": "- [A](a.md) — mentions docker here\n",
	})
	out, _, code := exec("--dir", dir, "search", "docker")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "MEMORY.md:1:") {
		t.Errorf("stdout = %q, want the index searched too (§II.6)", out)
	}
}

func TestSearchZeroMatchesIsExitOneAndSilentOnStdout(t *testing.T) {
	dir := memDir(t, map[string]string{"a.md": fm("A", "d", "user")})
	out, errOut, code := exec("--dir", dir, "search", "zzzznope")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, `no memory matches "zzzznope"`) {
		t.Errorf("stderr = %q, want the no-match message", errOut)
	}
}

func TestSearchEmptyDirectoryIsExitOne(t *testing.T) {
	_, _, code := exec("--dir", t.TempDir(), "search", "anything")
	if code != 1 {
		t.Errorf("exit code = %d, want 1 — zero results (§II.9)", code)
	}
}

func TestSearchTTYHeaderCountsMatchesAndMemories(t *testing.T) {
	dir := memDir(t, map[string]string{
		"a.md": "---\nname: A\n---\n\ndocker one\ndocker two\n",
		"b.md": "---\nname: B\n---\n\ndocker three\n",
		"c.md": "---\nname: C\n---\n\nnothing\n",
	})
	s, err := memory.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	renderSearch(s, memory.Search(s, "docker"), "docker", &buf, true)
	lines := strings.Split(buf.String(), "\n")
	if lines[0] != "memory: "+dir {
		t.Errorf("line 1 = %q, want the scope", lines[0])
	}
	if lines[1] != `3 matches in 2 memories for "docker"` {
		t.Errorf("line 2 = %q, want the match/memory counts", lines[1])
	}
}

func TestSearchTTYGroupsByFileWithRightAlignedLineNumbers(t *testing.T) {
	body := strings.Repeat("filler\n", 9) + "docker at ten\n"
	dir := memDir(t, map[string]string{
		"a.md": "---\nname: A\n---\n\ndocker at five\n" + body,
	})
	s, err := memory.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	renderSearch(s, memory.Search(s, "docker"), "docker", &buf, true)
	text := buf.String()
	if !strings.Contains(text, "\na.md\n") {
		t.Errorf("output = %q, want a file heading", text)
	}
	// 5 and 15 must be right-aligned to the same column.
	var numCols []int
	for _, l := range strings.Split(text, "\n") {
		if i := strings.Index(l, "docker at"); i > 0 {
			numCols = append(numCols, i)
		}
	}
	if len(numCols) != 2 {
		t.Fatalf("found %d hit lines, want 2:\n%s", len(numCols), text)
	}
	if numCols[0] != numCols[1] {
		t.Errorf("hit text starts at columns %v, want aligned", numCols)
	}
}

func TestSearchTTYHighlightsTheMatchWhenColoured(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	dir := memDir(t, map[string]string{"a.md": "---\nname: A\n---\n\nuses docker\n"})
	s, err := memory.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A plain buffer is not a TTY, so colour is off and the text stays clean.
	var buf bytes.Buffer
	renderSearch(s, memory.Search(s, "docker"), "docker", &buf, true)
	if strings.Contains(buf.String(), "\x1b[") {
		t.Error("colour emitted to a non-terminal writer")
	}
	if !strings.Contains(buf.String(), "uses docker") {
		t.Errorf("output = %q, want the matched line", buf.String())
	}
}

func TestSearchCountsEachMatchingLineOnce(t *testing.T) {
	dir := memDir(t, map[string]string{
		"a.md": "---\nname: A\n---\n\ndocker docker docker\n",
	})
	s, err := memory.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hits := memory.Search(s, "docker"); len(hits) != 1 {
		t.Errorf("got %d hits, want 1 — a match is a matching line", len(hits))
	}
}
