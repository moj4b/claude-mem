package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// One rule, checked against every command that acts on a memory directory:
//
//	a well-formed autoMemoryDirectory is the answer, whether or not it exists,
//	and the computed default path is never consulted behind its back.
//
// #1 found the key, #2 established that a missing configured directory still
// wins, and neither pinned the rule anywhere but `path`. This file is the
// per-command coverage that keeps the three commands that read memory, the two
// that enumerate, and completion from drifting apart again.

// scopedCommands is every command whose behaviour depends on the layer walk.
// `stale` and `docker` exist only in the displaced default directory, so any
// command that surfaces either has read a directory it must not have.
var scopedCommands = []struct {
	name string
	args []string
}{
	{"list", []string{"list"}},
	{"bare", nil}, // `mem` is `mem list` (§II.1)
	{"read", []string{"read", "stale"}},
	{"show", []string{"show", "stale"}},
	{"search", []string{"search", "docker"}},
	{"path", []string{"path"}},
}

func TestMissingConfiguredDirectoryIsExitThreeForEveryCommand(t *testing.T) {
	// The regression #2 fixed, pinned per command rather than for `path` alone:
	// no command may quietly answer from the populated default directory the
	// setting displaced.
	for _, c := range scopedCommands {
		t.Run(c.name, func(t *testing.T) {
			r := newRedirected(t, false)
			out, errOut, code := exec(c.args...)
			if code != exitNoDir {
				t.Errorf("exit code = %d, want 3", code)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty — nothing was read", out)
			}
			if !strings.Contains(errOut, r.configured) {
				t.Errorf("stderr = %q, want it to name the configured directory %q", errOut, r.configured)
			}
			if strings.Contains(errOut, r.stale) {
				t.Errorf("stderr = %q, want no mention of the displaced default %q", errOut, r.stale)
			}
		})
	}
}

func TestConfiguredDirectoryIsTheOnlyOneReadForEveryCommand(t *testing.T) {
	// The configured directory exists and holds `current`; the displaced default
	// exists too and holds `stale`/`docker`. Only the first may be visible.
	for _, c := range scopedCommands {
		t.Run(c.name, func(t *testing.T) {
			r := newRedirected(t, true)
			out, errOut, code := exec(c.args...)
			switch c.name {
			case "read", "show", "search":
				// The name and the query live only in the displaced default.
				if code != exitNotFound {
					t.Errorf("exit code = %d, want 1 — the displaced default must not answer", code)
				}
			default:
				if code != exitOK {
					t.Errorf("exit code = %d, want 0 (stderr: %s)", code, errOut)
				}
			}
			if strings.Contains(out, "stale") || strings.Contains(out, "docker notes") {
				t.Errorf("stdout = %q, leaked the displaced default's contents", out)
			}
			if strings.Contains(out, r.stale) {
				t.Errorf("stdout = %q, named the displaced default %q", out, r.stale)
			}
		})
	}
}

func TestPathNamesTheConfiguredDirectoryWhetherOrNotItExists(t *testing.T) {
	for _, exists := range []bool{true, false} {
		r := newRedirected(t, exists)
		out, _, code := exec("path")
		if exists {
			if code != exitOK || strings.TrimSpace(out) != r.configured {
				t.Errorf("exists=%v: path = %q (exit %d), want %q", exists, out, code, r.configured)
			}
			continue
		}
		if code != exitNoDir {
			t.Errorf("exists=%v: exit code = %d, want 3", exists, code)
		}
		if out != "" {
			t.Errorf("exists=%v: stdout = %q, want empty so `cd $(mem path)` cannot go wrong", exists, out)
		}
	}
}

func TestEmptyConfiguredDirectoryIsSuccessNotAFallback(t *testing.T) {
	// §II.9's distinction, under a settings key: an existing-but-empty directory
	// is exit 0, and emphatically not a reason to go looking at the default.
	r := newRedirected(t, true)
	if err := os.Remove(filepath.Join(r.configured, "current.md")); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := exec("list")
	if code != exitOK {
		t.Errorf("exit code = %d, want 0", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "memory directory is empty: "+r.configured) {
		t.Errorf("stderr = %q, want the empty-directory note naming %q", errOut, r.configured)
	}
}

func TestConfiguredPathThatIsAFileIsExitThreeNotAFallback(t *testing.T) {
	r := newRedirected(t, false)
	if err := os.MkdirAll(filepath.Dir(r.configured), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.configured, []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, code := exec("list")
	if code != exitNoDir {
		t.Errorf("exit code = %d, want 3", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
}

func TestEveryLayerThatSuppliesTheKeyBehavesTheSame(t *testing.T) {
	// Layers 4 and 5, the real key and the legacy ones, and the nested form: all
	// of them are "a configured directory", so all of them displace the default.
	for _, tc := range []struct {
		name string
		set  func(t *testing.T, r redirected)
	}{
		{"settings.local.json", func(t *testing.T, r redirected) {
			writeSettingsFile(t, r.settings, `{"autoMemoryDirectory":"`+r.configured+`"}`)
		}},
		{"settings.json", func(t *testing.T, r redirected) {
			r.clearProjectSettings(t)
			writeSettingsFile(t, filepath.Join(r.proj, ".claude", "settings.json"),
				`{"autoMemoryDirectory":"`+r.configured+`"}`)
		}},
		{"user settings.json", func(t *testing.T, r redirected) {
			r.clearProjectSettings(t)
			writeSettingsFile(t, r.userSettings, `{"autoMemoryDirectory":"`+r.configured+`"}`)
		}},
		{"legacy memoryDir", func(t *testing.T, r redirected) {
			writeSettingsFile(t, r.settings, `{"memoryDir":"`+r.configured+`"}`)
		}},
		{"nested memory.dir", func(t *testing.T, r redirected) {
			writeSettingsFile(t, r.settings, `{"memory":{"dir":"`+r.configured+`"}}`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRedirected(t, true)
			tc.set(t, r)
			out, _, code := exec("path")
			if code != exitOK || strings.TrimSpace(out) != r.configured {
				t.Errorf("path = %q (exit %d), want %q", out, code, r.configured)
			}
			// ...and the displaced default stops being another project.
			_, errOut, code := exec("search", "docker")
			if code != exitNotFound {
				t.Fatalf("search: exit code = %d, want 1", code)
			}
			if !strings.Contains(errOut, "1 memory across 1 other project") {
				t.Errorf("search stderr = %q, want courier counted alone", errOut)
			}
		})
	}
}

func TestAValueClaudeCodeRejectsFallsThroughToTheDefault(t *testing.T) {
	// The one fall-through, and it is about the value's form, never about
	// whether the directory is there. A relative path is not resolved against
	// anything — the key reads as unset, and layer 6 answers.
	for _, bad := range []string{
		`"relative/nope"`,
		`"~/.."`,
		`""`,
		`null`,
		`42`,
		`["/abs/but/an/array"]`,
	} {
		t.Run(bad, func(t *testing.T) {
			r := newRedirected(t, true)
			writeSettingsFile(t, r.settings, `{"autoMemoryDirectory":`+bad+`}`)
			out, _, code := exec("path")
			if code != exitOK || strings.TrimSpace(out) != r.stale {
				t.Errorf("path = %q (exit %d), want the default %q", out, code, r.stale)
			}
		})
	}
}

func TestUnreadableOrInvalidSettingsAreSkippedSilently(t *testing.T) {
	for _, body := range []string{`{ not json`, ``, `[]`} {
		t.Run(body, func(t *testing.T) {
			r := newRedirected(t, true)
			writeSettingsFile(t, r.settings, body)
			out, errOut, code := exec("path")
			if code != exitOK || strings.TrimSpace(out) != r.stale {
				t.Errorf("path = %q (exit %d), want the default %q", out, code, r.stale)
			}
			if errOut != "" {
				t.Errorf("stderr = %q, want silence — a broken settings file is skipped", errOut)
			}
		})
	}
}

func TestOneOffOverridesBeatSettingsAndKeepTheDefaultVisible(t *testing.T) {
	// --dir and $CLAUDE_MEM_DIR retarget one invocation. They outrank the
	// settings key, and — unlike it — they do not restate where this project
	// keeps its memory, so the default directory is still this project's and
	// must keep showing up in the cross-project hints.
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, r redirected) []string
	}{
		{"--dir", func(t *testing.T, r redirected) []string {
			return []string{"--dir", r.courier}
		}},
		{"$CLAUDE_MEM_DIR", func(t *testing.T, r redirected) []string {
			t.Setenv("CLAUDE_MEM_DIR", r.courier)
			return nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRedirected(t, true)
			flags := tc.setup(t, r)
			out, _, code := exec(append(append([]string{}, flags...), "path")...)
			if code != exitOK || strings.TrimSpace(out) != r.courier {
				t.Errorf("path = %q (exit %d), want %q", out, code, r.courier)
			}
			// `stale` is not courier's, so this misses — and the hint may name the
			// project's default directory, because nothing displaced it here.
			_, errOut, code := exec(append(append([]string{}, flags...), "read", "stale")...)
			if code != exitNotFound {
				t.Fatalf("read: exit code = %d, want 1", code)
			}
			if !strings.Contains(errOut, "mem read --project proj stale") {
				t.Errorf("read stderr = %q, want the default directory still offered", errOut)
			}
		})
	}
}

// --project, --all, `projects` and project-name completion are one view of the
// projects root, and the rows of each have to stay reachable by
// `mem read --project <name> <file>` — the escape hatch `mem path`'s exit-3
// message promises. A settings key belongs to the directory you are standing
// in, so it redirects the local view only. These four are a set: change one and
// you must change all four, which is what four separate failures will tell you.
func TestProjectStaysAProjectsRootView(t *testing.T) {
	newRedirected(t, false)
	out, _, code := exec("--project", "proj", "list")
	if code != exitOK {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "stale") {
		t.Errorf("stdout = %q, want the displaced default still reachable", out)
	}
}

func TestAllStaysAProjectsRootView(t *testing.T) {
	newRedirected(t, false)
	out, _, code := exec("--all", "search", "docker")
	if code != exitOK {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, want := range []string{"proj:stale.md:", "courier:courier.md:"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout = %q, want a %q row", out, want)
		}
	}
}

func TestProjectsStaysAProjectsRootView(t *testing.T) {
	r := newRedirected(t, false)
	out, _, code := exec("projects")
	if code != exitOK {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "proj\t1\t"+r.stale) {
		t.Errorf("stdout = %q, want proj listed at %q", out, r.stale)
	}
}

func TestProjectNameCompletionStaysAProjectsRootView(t *testing.T) {
	newRedirected(t, false)
	out, _, _ := exec("__complete", "--project", "")
	if !strings.Contains(out, "proj\n") {
		t.Errorf("completion = %q, want proj offered, matching what --project resolves", out)
	}
}

func TestCompletionOffersOnlyTheConfiguredDirectorysNames(t *testing.T) {
	// §II.10: __complete must never write to stderr or exit non-zero. It must
	// also never offer a name `mem read` would then fail to find.
	for _, exists := range []bool{true, false} {
		newRedirected(t, exists)
		out, errOut, code := exec("__complete", "read", "")
		if code != exitOK {
			t.Errorf("exists=%v: exit code = %d, want 0", exists, code)
		}
		if errOut != "" {
			t.Errorf("exists=%v: stderr = %q, want silence", exists, errOut)
		}
		if strings.Contains(out, "stale") {
			t.Errorf("exists=%v: completion = %q, offered a name from the displaced default", exists, out)
		}
		if exists && !strings.Contains(out, "current") {
			t.Errorf("completion = %q, want the configured directory's names", out)
		}
		if !exists && strings.TrimSpace(out) != "" {
			t.Errorf("completion = %q, want nothing — there is no memory directory", out)
		}
	}
}

func TestExplainTraceNamesTheSettingsFileThatDecided(t *testing.T) {
	r := newRedirected(t, true)
	out, errOut, code := exec("path", "--explain")
	if code != exitOK {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if strings.TrimSpace(out) != r.configured {
		t.Errorf("stdout = %q, want just the path", out)
	}
	for _, want := range []string{"autoMemoryDirectory", r.settings, "→ " + r.configured} {
		if !strings.Contains(errOut, want) {
			t.Errorf("trace missing %q; got:\n%s", want, errOut)
		}
	}
}

func TestSettingsAreFoundFromASubdirectory(t *testing.T) {
	// The walk starts at cwd and climbs (§II.3), so standing in a subdirectory
	// must not fall back to the default either.
	r := newRedirected(t, true)
	sub := filepath.Join(r.proj, "src", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	inDir(t, sub)
	out, _, code := exec("path")
	if code != exitOK || strings.TrimSpace(out) != r.configured {
		t.Errorf("path = %q (exit %d), want %q", out, code, r.configured)
	}
	_, errOut, code := exec("search", "docker")
	if code != exitNotFound {
		t.Fatalf("search: exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "1 memory across 1 other project") {
		t.Errorf("search stderr = %q, want courier counted alone from a subdirectory too", errOut)
	}
}
