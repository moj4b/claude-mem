package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsMemoryFileAcceptsOnlyDirectMarkdownChildren(t *testing.T) {
	dir := filepath.FromSlash("/home/you/.claude/projects/-w-courier/memory")
	j := func(parts ...string) string {
		return filepath.Join(append([]string{dir}, parts...)...)
	}
	cases := []struct {
		path string
		want bool
		why  string
	}{
		{j("project_roadmap.md"), true, "a direct .md child is a memory"},
		{j("MEMORY.md"), false, "the index is not a memory"},
		{j("notes.txt"), false, "a non-.md file is not a memory"},
		{j(".consolidate-lock"), false, "courier's lock file is not a memory"},
		{j("sub", "nested.md"), false, "memory directories are flat (§I.2)"},
		{j("..", "outside", "target.md"), false, "escaping the directory is never a memory"},
		{filepath.FromSlash("/etc/hosts"), false, "an absolute path elsewhere is not a memory"},
		{filepath.FromSlash("/etc/passwd.md"), false, "nor is one that merely ends in .md"},
		{j(".md"), false, "a bare extension names nothing"},
		{dir, false, "the directory itself is not a memory"},
	}
	for _, c := range cases {
		if got := IsMemoryFile(dir, c.path); got != c.want {
			t.Errorf("IsMemoryFile(%q) = %v, want %v — %s", c.path, got, c.want, c.why)
		}
	}
}

func TestRemovableRejectsAnythingThatIsNotAMemoryFileOnDisk(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()

	target := filepath.Join(outside, "target.md")
	mustWrite(t, target, "OUTSIDE\n")
	mustWrite(t, filepath.Join(dir, "real_memory.md"), "---\nname: real\n---\n\nbody\n")
	mustWrite(t, filepath.Join(dir, "notes.txt"), "not a memory\n")
	mustWrite(t, filepath.Join(dir, IndexFile), "- [x](real_memory.md) — hook\n")
	// A *directory* whose name ends in .md: os.Remove deletes an empty
	// directory, so this must be refused on the file type, not just the name.
	if err := os.Mkdir(filepath.Join(dir, "looks_like.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Recorded rather than skipped on: a platform without symlinks must still
	// exercise the directory and non-.md cases below.
	symlinked := os.Symlink(target, filepath.Join(dir, "linked.md")) == nil

	for _, c := range []struct {
		name string
		ok   bool
		why  string
	}{
		{"real_memory.md", true, "a regular .md child is removable"},
		{"linked.md", true, "a symlinked memory is removable — as the link, not the target"},
		{"looks_like.md", false, "a directory is never removable, whatever it is named"},
		{"notes.txt", false, "a non-.md file is not a memory"},
		{IndexFile, false, "the index is not a memory"},
		{"absent.md", false, "a path that is not there is not removable"},
	} {
		if c.name == "linked.md" && !symlinked {
			t.Log("symlinks unavailable; skipping just that case")
			continue
		}
		err := Removable(dir, filepath.Join(dir, c.name))
		if c.ok && err != nil {
			t.Errorf("Removable(%s) = %v, want nil — %s", c.name, err, c.why)
		}
		if !c.ok && err == nil {
			t.Errorf("Removable(%s) = nil, want an error — %s", c.name, c.why)
		}
	}

	// Escaping the directory is refused even though the target really exists.
	if err := Removable(dir, target); err == nil {
		t.Error("Removable escaped the memory directory, want an error")
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
