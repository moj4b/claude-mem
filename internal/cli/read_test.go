package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moj4b/claude-mem/internal/memory"
)

func TestReadPipedIsByteIdenticalToTheFile(t *testing.T) {
	// `mem read naming > copy.md` must yield a valid memory file (§II.5).
	raw := fm("Roadmap for the next release", "rename to Beacon", "project")
	dir := memDir(t, map[string]string{"project_roadmap.md": raw})
	out, _, code := exec("--dir", dir, "read", "project_roadmap")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if out != raw {
		t.Errorf("stdout =\n%q\nwant byte-identical\n%q", out, raw)
	}
}

func TestReadTTYPrintsHeaderThenBodyWithFrontmatterStripped(t *testing.T) {
	dir := memDir(t, map[string]string{
		"project_roadmap.md": fm("Roadmap for the next release", "rename to Beacon", "project"),
	})
	s, err := memory.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	renderRead(s.Memories[0], &buf, true)
	lines := strings.Split(buf.String(), "\n")
	if lines[0] != "Roadmap for the next release" {
		t.Errorf("line 1 = %q, want the title", lines[0])
	}
	if lines[1] != "project · project_roadmap.md" {
		t.Errorf("line 2 = %q, want %q", lines[1], "project · project_roadmap.md")
	}
	if lines[2] != "" {
		t.Errorf("line 3 = %q, want a blank line", lines[2])
	}
	if strings.Contains(buf.String(), "description:") {
		t.Error("TTY output leaked frontmatter")
	}
	if !strings.Contains(buf.String(), "body") {
		t.Error("TTY output is missing the body")
	}
}

func TestReadTheIndexEvenThoughListHidesIt(t *testing.T) {
	dir := memDir(t, map[string]string{
		"a.md":      fm("A", "d", "user"),
		"MEMORY.md": "- [A](a.md) — d\n",
	})
	out, errOut, code := exec("--dir", dir, "read", "MEMORY")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut)
	}
	if !strings.Contains(out, "- [A](a.md)") {
		t.Errorf("stdout = %q, want the index contents", out)
	}
}

func TestReadAmbiguousIsExitOneAndSilentOnStdout(t *testing.T) {
	dir := memDir(t, map[string]string{
		"project_targets.md":    fm("D", "d", "project"),
		"project_roadmap.md":    fm("N", "d", "project"),
		"project_milestones.md": fm("S", "d", "project"),
	})
	out, errOut, code := exec("--dir", dir, "read", "project_")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty — nothing was read", out)
	}
	if !strings.Contains(errOut, "project_targets") {
		t.Errorf("stderr = %q, want the candidates listed", errOut)
	}
}

func TestReadNotFoundIsExitOneAndSilentOnStdout(t *testing.T) {
	dir := memDir(t, map[string]string{"a.md": fm("A", "d", "user")})
	out, errOut, code := exec("--dir", dir, "read", "zzzznope")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "no memory matches") {
		t.Errorf("stderr = %q, want a not-found message", errOut)
	}
}

func TestReadOnEmptyDirectoryIsExitOneNotZero(t *testing.T) {
	// The directory exists (so not 3) but the name is not there (so 1) — §II.9.
	out, _, code := exec("--dir", t.TempDir(), "read", "anything")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
}

func TestShowIsAnAliasForRead(t *testing.T) {
	dir := memDir(t, map[string]string{"a.md": fm("A", "d", "user")})
	readOut, _, _ := exec("--dir", dir, "read", "a")
	showOut, _, _ := exec("--dir", dir, "show", "a")
	if readOut != showOut {
		t.Errorf("show output = %q, want it identical to read = %q", showOut, readOut)
	}
}

func TestReadUntypedMemoryOmitsTheTypeFromTheHeader(t *testing.T) {
	dir := memDir(t, map[string]string{"a.md": "---\nname: A\n---\n\nbody\n"})
	s, err := memory.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	renderRead(s.Memories[0], &buf, true)
	if line := strings.Split(buf.String(), "\n")[1]; line != "a.md" {
		t.Errorf("line 2 = %q, want just the filename when there is no type", line)
	}
}

func TestReadUnreadableFileIsAnErrorNotACrash(t *testing.T) {
	dir := memDir(t, map[string]string{"a.md": fm("A", "d", "user")})
	// Remove read permission after the store has been built.
	path := filepath.Join(dir, "a.md")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Skip("cannot chmod in this environment")
	}
	t.Cleanup(func() { os.Chmod(path, 0o644) })
	if os.Geteuid() == 0 {
		t.Skip("running as root; permissions are not enforced")
	}
	_, _, code := exec("--dir", dir, "read", "a")
	if code == 0 {
		t.Error("exit code = 0 for an unreadable file, want non-zero")
	}
}
