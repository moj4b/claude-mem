package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathPrintsResolvedDirectory(t *testing.T) {
	dir := t.TempDir()
	out, errOut, code := exec("--dir", dir, "path")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if out != dir+"\n" {
		t.Errorf("stdout = %q, want %q", out, dir+"\n")
	}
	if errOut != "" {
		t.Errorf("stderr = %q, want empty", errOut)
	}
}

func TestPathOnMissingDirectoryIsExit3AndSilentOnStdout(t *testing.T) {
	// `cd $(mem path)` must never cd somewhere wrong (§II.7).
	out, errOut, code := exec("--dir", "/nope/nope", "path")
	if code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "memory directory does not exist: /nope/nope") ||
		!strings.Contains(errOut, "(from --dir)") {
		t.Errorf("stderr = %q, want it to name the path and the layer", errOut)
	}
}

func TestPathWithNoMemoryAnywhereExplainsWhatItLookedFor(t *testing.T) {
	// The most important message in the tool (§II.9).
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "cfg"))
	t.Setenv("CLAUDE_MEM_DIR", "")
	inDir(t, t.TempDir())

	out, errOut, code := exec("path")
	if code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	for _, want := range []string{
		"no memory directory for this project",
		"looked for:",
		"Claude Code has not saved memory for this project yet",
		"mem path --explain",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing %q; got:\n%s", want, errOut)
		}
	}
}

func TestPathWithMissingConfiguredDirectoryBlamesTheSettings(t *testing.T) {
	// "Claude Code has not saved memory for this project yet" is the wrong
	// explanation when settings name a directory that is not there — that is a
	// setting to fix, not memory waiting to be written. Naming the file turns an
	// exit 3 into something actionable.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("CLAUDE_MEM_DIR", "")
	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".claude", "settings.local.json"),
		[]byte(`{"autoMemoryDirectory":"/nope/nope"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	inDir(t, proj)

	out, errOut, code := exec("path")
	if code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	for _, want := range []string{"/nope/nope", "autoMemoryDirectory", "settings"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing %q; got:\n%s", want, errOut)
		}
	}
	if strings.Contains(errOut, "has not saved memory for this project yet") {
		t.Errorf("stderr blames Claude Code instead of the setting; got:\n%s", errOut)
	}
}

func TestPathExplainPutsPathOnStdoutAndTraceOnStderr(t *testing.T) {
	// $(mem path --explain) must still work (§II.3).
	dir := t.TempDir()
	out, errOut, code := exec("--dir", dir, "path", "--explain")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if out != dir+"\n" {
		t.Errorf("stdout = %q, want just the path", out)
	}
	if !strings.Contains(errOut, "resolving memory directory") {
		t.Errorf("stderr = %q, want the trace", errOut)
	}
}

// inDir chdirs for the duration of the test.
func inDir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })
}
