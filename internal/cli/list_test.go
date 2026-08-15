package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moj4b/claude-mem/internal/memory"
)

// memDir writes a memory directory from name→content and returns its path.
func memDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func fm(name, desc, typ string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\ntype: " + typ + "\n---\n\nbody\n"
}

func TestListPlainIsTabSeparatedWithNoHeaderOrGroups(t *testing.T) {
	dir := memDir(t, map[string]string{
		"project_roadmap.md": fm("Roadmap", "rename to Beacon", "project"),
		"user_prefs.md":      fm("User preferences", "terse dev", "user"),
		"MEMORY.md":          "- [User preferences](user_prefs.md) — terse dev\n",
	})
	out, errOut, code := exec("--dir", dir, "list")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut)
	}
	want := "project_roadmap\tproject\trename to Beacon\n" +
		"user_prefs\tuser\tterse dev\n"
	if out != want {
		t.Errorf("stdout =\n%q\nwant\n%q", out, want)
	}
}

func TestListExcludesTheIndexFromGroupsButCountsIt(t *testing.T) {
	dir := memDir(t, map[string]string{
		"a.md":      fm("A", "d", "user"),
		"MEMORY.md": "- [A](a.md) — d\n",
	})
	out, _, _ := exec("--dir", dir, "list")
	if strings.Contains(out, "MEMORY") {
		t.Errorf("stdout = %q, want the index excluded (§II.4)", out)
	}
	var buf bytes.Buffer
	s, err := memory.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	renderList(s, &buf, true)
	if !strings.Contains(buf.String(), "index: MEMORY.md") {
		t.Errorf("TTY header = %q, want it to count the index", buf.String())
	}
}

func TestListGroupOrderFollowsTheSpecifiedSequence(t *testing.T) {
	// §II.4: user, feedback, project, reference, then other types
	// alphabetically, then untyped last.
	dir := memDir(t, map[string]string{
		"z_untyped.md":   "---\nname: Untyped\ndescription: no type\n---\n\nbody\n",
		"a_zebra.md":     fm("Zebra", "d", "zebra"),
		"a_reference.md": fm("Ref", "d", "reference"),
		"a_project.md":   fm("Proj", "d", "project"),
		"a_feedback.md":  fm("Fb", "d", "feedback"),
		"a_user.md":      fm("Usr", "d", "user"),
		"a_alpaca.md":    fm("Alpaca", "d", "alpaca"),
	})
	s, err := memory.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	renderList(s, &buf, true)

	want := []string{"user", "feedback", "project", "reference", "alpaca", "zebra", "untyped"}
	var got []string
	for _, line := range strings.Split(buf.String(), "\n") {
		for _, w := range want {
			if line == w {
				got = append(got, line)
			}
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("group order = %v, want %v", got, want)
	}
}

func TestListSortsByFilenameWithinAGroup(t *testing.T) {
	dir := memDir(t, map[string]string{
		"project_milestones.md": fm("Status", "d", "project"),
		"project_targets.md":    fm("Targets", "d", "project"),
		"project_roadmap.md":    fm("Naming", "d", "project"),
	})
	out, _, _ := exec("--dir", dir, "list")
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		names = append(names, strings.Split(line, "\t")[0])
	}
	want := "project_milestones,project_roadmap,project_targets"
	if strings.Join(names, ",") != want {
		t.Errorf("order = %v, want %s", names, want)
	}
}

func TestListTTYHeaderNamesTheScopeAndCount(t *testing.T) {
	// The user must never have to guess which memory they are looking at (§II.0).
	dir := memDir(t, map[string]string{
		"a.md": fm("A", "d", "user"),
		"b.md": fm("B", "d", "user"),
	})
	s, err := memory.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	renderList(s, &buf, true)
	head := strings.Split(buf.String(), "\n")
	if head[0] != "memory: "+dir {
		t.Errorf("line 1 = %q, want %q", head[0], "memory: "+dir)
	}
	if head[1] != "2 memories · no index" {
		t.Errorf("line 2 = %q, want %q", head[1], "2 memories · no index")
	}
	if head[2] != "" {
		t.Errorf("line 3 = %q, want a blank line separating header from groups", head[2])
	}
	if head[3] != "user" {
		t.Errorf("line 4 = %q, want the first group heading", head[3])
	}
}

func TestListTTYPadsNamesToTheLongest(t *testing.T) {
	dir := memDir(t, map[string]string{
		"short.md":            fm("S", "desc one", "user"),
		"a_much_longer_id.md": fm("L", "desc two", "user"),
	})
	s, err := memory.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	renderList(s, &buf, true)
	var cols []int
	for _, line := range strings.Split(buf.String(), "\n") {
		if i := strings.Index(line, "desc "); i > 0 {
			cols = append(cols, i)
		}
	}
	if len(cols) != 2 {
		t.Fatalf("found %d description columns, want 2:\n%s", len(cols), buf.String())
	}
	if cols[0] != cols[1] {
		t.Errorf("descriptions start at columns %v, want them aligned", cols)
	}
}

func TestListEmptyDirectoryIsExitZeroWithANoteOnStderr(t *testing.T) {
	// Exit 0 — an existing-but-empty directory is success, and must never be
	// confused with "no memory directory" (3) (§II.9).
	dir := t.TempDir()
	out, errOut, code := exec("--dir", dir, "list")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "memory directory is empty: "+dir) {
		t.Errorf("stderr = %q, want the empty note naming the path", errOut)
	}
}

func TestListMissingDirectoryIsExit3NotExit0(t *testing.T) {
	_, _, code := exec("--dir", "/nope/nope", "list")
	if code != 3 {
		t.Errorf("exit code = %d, want 3 — missing ≠ empty (§II.9)", code)
	}
}

func TestListPlainDoesNotTruncateOrColour(t *testing.T) {
	long := strings.Repeat("x", 300)
	dir := memDir(t, map[string]string{"a.md": fm("A", long, "user")})
	t.Setenv("COLUMNS", "40")
	out, _, _ := exec("--dir", dir, "list")
	if !strings.Contains(out, long) {
		t.Error("plain output truncated the description; it must not (§II.4)")
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("plain output contains ANSI escapes")
	}
}

func TestListTTYTruncatesToTerminalWidth(t *testing.T) {
	long := strings.Repeat("x", 300)
	dir := memDir(t, map[string]string{"a.md": fm("A", long, "user")})
	t.Setenv("COLUMNS", "40")
	s, err := memory.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	renderList(s, &buf, true)
	// The header path is never truncated — it has to stay usable. Only the
	// description column is clipped to width (§II.4).
	for _, line := range strings.Split(buf.String(), "\n") {
		if !strings.HasPrefix(line, "  a ") && !strings.HasPrefix(line, "  a\t") {
			continue
		}
		if len([]rune(line)) > 40 {
			t.Errorf("memory line exceeds width 40 (%d runes): %q", len([]rune(line)), line)
		}
		if !strings.HasSuffix(line, "…") {
			t.Errorf("line = %q, want it clipped with an ellipsis", line)
		}
	}
}

func TestListUsesIndexHookWhenFrontmatterHasNoDescription(t *testing.T) {
	dir := memDir(t, map[string]string{
		"a.md":      "---\nname: A\ntype: user\n---\n\nbody\n",
		"MEMORY.md": "- [A](a.md) — hook from the index\n",
	})
	out, _, _ := exec("--dir", dir, "list")
	if !strings.Contains(out, "hook from the index") {
		t.Errorf("stdout = %q, want the index hook as description (§II.4)", out)
	}
}
