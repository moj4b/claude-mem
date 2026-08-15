package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moj4b/claude-mem/internal/resolve"
)

// redirected is the shape of every real project that keeps memory beside its
// code: `.claude/settings.local.json` names a directory, and the computed
// default directory Claude Code used to write to is still sitting under the
// projects root with the memories it wrote before the setting landed.
//
//	<tmp>/w/proj/.claude/settings.local.json   autoMemoryDirectory -> configured
//	<tmp>/w/proj/.claude/memory/current.md     the memory Claude Code reads today
//	<tmp>/config/projects/<slug of w/proj>/memory/stale.md  what it read before
//	<tmp>/config/projects/<slug of w/courier>/memory/courier.md     an unrelated project
//
// proj and courier are siblings so that the shared-prefix stripping of §II.0 yields
// the names a real projects root yields: "proj" and "courier".
type redirected struct {
	tmp, root, proj, configured, stale, courier string
	settings, userSettings                      string
}

// clearProjectSettings removes layer 4, so layers 5 and 6 decide.
func (r redirected) clearProjectSettings(t *testing.T) {
	t.Helper()
	if err := os.Remove(r.settings); err != nil {
		t.Fatal(err)
	}
}

// newRedirected plants that fixture and points the process env at it. When
// exists is false the configured directory is never created, which is the case
// #2 settled: the setting still wins, and the stale default is still not this
// project's memory.
func newRedirected(t *testing.T, exists bool) redirected {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := redirected{
		tmp:  tmp,
		root: filepath.Join(tmp, "config"),
		proj: filepath.Join(tmp, "w", "proj"),
	}
	r.configured = filepath.Join(r.proj, ".claude", "memory")
	r.stale = filepath.Join(r.root, "projects", resolve.Slugify(r.proj), "memory")
	r.courier = filepath.Join(r.root, "projects",
		resolve.Slugify(filepath.Join(tmp, "w", "courier")), "memory")
	r.settings = filepath.Join(r.proj, ".claude", "settings.local.json")
	r.userSettings = filepath.Join(r.root, "settings.json")

	if exists {
		writeMemory(t, r.configured, "current.md", fm("Current", "what Claude Code reads today", "project"))
	}
	// Both the stale default and the unrelated project hold "docker", so a search
	// that finds it in one but not the other proves which directories were read.
	writeMemory(t, r.stale, "stale.md", fm("Stale", "docker notes from before", "project"))
	writeMemory(t, r.courier, "courier.md", fm("Courier", "docker notes too", "project"))
	writeSettingsFile(t, r.settings, `{"autoMemoryDirectory":"`+r.configured+`"}`)

	t.Setenv("HOME", tmp)
	t.Setenv("CLAUDE_CONFIG_DIR", r.root)
	t.Setenv("CLAUDE_MEM_DIR", "")
	inDir(t, r.proj)
	return r
}

func writeMemory(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The reported bug. A local search that misses must not report the project's
// own displaced default directory as a hit "in another project" — it is the
// same project, and it is memory Claude Code no longer reads.

func TestSearchMissDoesNotCountTheDisplacedDefaultAsAnotherProject(t *testing.T) {
	newRedirected(t, true)
	out, errOut, code := exec("search", "docker")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stdout: %q)", code, out)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	// courier has it; the displaced default has it too but is not another project.
	if !strings.Contains(errOut, "1 memory across 1 other project") {
		t.Errorf("stderr = %q, want the hint to count courier alone", errOut)
	}
}

// Not another project — but not nothing either. §II.0: any cross-project result
// must print the exact command that reaches it, so output is always actionable.
// Dropping the row would trade one wrong answer for no answer.

func TestSearchMissNamesTheDisplacedDirectoryAsThisProjectsOwn(t *testing.T) {
	r := newRedirected(t, true)
	if err := os.Remove(filepath.Join(r.courier, "courier.md")); err != nil {
		t.Fatal(err)
	}
	_, errOut, code := exec("search", "docker")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if strings.Contains(errOut, "other project") {
		t.Errorf("stderr = %q, want it not called another project", errOut)
	}
	if want := "mem search --dir " + r.stale + " docker"; !strings.Contains(errOut, want) {
		t.Errorf("stderr = %q, want the runnable %q", errOut, want)
	}
}

func TestSearchMissNamesBothWhenTheQueryIsInEach(t *testing.T) {
	r := newRedirected(t, true)
	_, errOut, code := exec("search", "docker")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "1 memory across 1 other project") {
		t.Errorf("stderr = %q, want courier counted alone as another project", errOut)
	}
	if !strings.Contains(errOut, "mem search --dir "+r.stale+" docker") {
		t.Errorf("stderr = %q, want the displaced directory offered too", errOut)
	}
}

func TestReadMissOffersTheDisplacedDirectoryByPathNotAsAnotherProject(t *testing.T) {
	r := newRedirected(t, true)
	_, errOut, code := exec("read", "stale")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if strings.Contains(errOut, "--project") {
		t.Errorf("stderr = %q, want the displaced directory not offered as a project", errOut)
	}
	if want := "mem read --dir " + r.stale + " stale"; !strings.Contains(errOut, want) {
		t.Errorf("stderr = %q, want the runnable %q", errOut, want)
	}
}

func TestSearchAndReadMissesStayQuietWhenTheDisplacedDirectoryHasNothing(t *testing.T) {
	r := newRedirected(t, true)
	if err := os.Remove(filepath.Join(r.stale, "stale.md")); err != nil {
		t.Fatal(err)
	}
	// A positive assertion beside each absence, or these pass on a command that
	// printed nothing at all.
	out, errOut, code := exec("search", "zzznope")
	if code != exitNotFound || out != "" {
		t.Errorf("search: exit %d, stdout %q; want 1 and empty", code, out)
	}
	if !strings.Contains(errOut, `no memory matches "zzznope"`) {
		t.Errorf("search stderr = %q, want the plain miss", errOut)
	}
	if strings.Contains(errOut, "--dir") {
		t.Errorf("search stderr = %q, want no offer — the displaced directory has no hit", errOut)
	}
	out, errOut, code = exec("read", "zzznope")
	if code != exitNotFound || out != "" {
		t.Errorf("read: exit %d, stdout %q; want 1 and empty", code, out)
	}
	if !strings.Contains(errOut, "no memory matches 'zzznope'") {
		t.Errorf("read stderr = %q, want the plain miss", errOut)
	}
	if strings.Contains(errOut, "--dir") {
		t.Errorf("read stderr = %q, want no offer", errOut)
	}
}

func TestSearchMissStillPointsAtAnAncestorProject(t *testing.T) {
	// The regression the first cut of this fix introduced: bounding the displaced
	// walk at the project root is what keeps a genuinely separate ancestor
	// project — one `mem projects` names and `--project` reaches — in the hints.
	// The whole stale DIRECTORY has to go, not just its file: leaving it there
	// halts the walk at this project's own default whatever the bound is, and the
	// test passes with the regression back in.
	r := newRedirected(t, true)
	if err := os.RemoveAll(r.courier); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(r.stale); err != nil {
		t.Fatal(err)
	}
	writeMemory(t, filepath.Join(r.root, "projects",
		resolve.Slugify(filepath.Join(r.tmp, "w")), "memory"),
		"mono.md", fm("Mono", "docker, in the enclosing project", "project"))
	_, errOut, code := exec("search", "docker")
	if code != exitNotFound {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "1 memory across 1 other project") {
		t.Errorf("stderr = %q, want the enclosing project still offered", errOut)
	}
	if strings.Contains(errOut, displacedNote) {
		t.Errorf("stderr = %q, want the enclosing project not called displaced", errOut)
	}
}

func TestUserSettingsRedirectFromASubdirectoryToo(t *testing.T) {
	// The bound has to find the project root from a subdirectory, or the original
	// bug survives for every project whose settings are user-level. §II.0:
	// subdirectories are the normal case, not an edge case.
	r := newRedirected(t, true)
	r.clearProjectSettings(t)
	writeSettingsFile(t, r.userSettings, `{"autoMemoryDirectory":"`+r.configured+`"}`)
	if err := os.MkdirAll(filepath.Join(r.proj, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(r.proj, "src", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	inDir(t, sub)
	_, errOut, code := exec("read", "stale")
	if code != exitNotFound {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if strings.Contains(errOut, "--project") {
		t.Errorf("stderr = %q, want this project's own displaced directory not "+
			"offered as another project from a subdirectory", errOut)
	}
	if !strings.Contains(errOut, "mem read --dir "+r.stale+" stale") {
		t.Errorf("stderr = %q, want it offered by path", errOut)
	}
}

func TestEmptyConfiguredDirectoryStillNamesTheDisplacedOne(t *testing.T) {
	// The state a project lands in the moment the key is added: nothing written
	// to the new directory yet, everything still in the old one. `list` is the
	// command you reach for first, and dead-ending there while `search` and
	// `read` name the directory is the inconsistency §II.0 exists to prevent.
	r := newRedirected(t, true)
	if err := os.Remove(filepath.Join(r.configured, "current.md")); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := exec("list")
	if code != exitOK {
		t.Fatalf("exit code = %d, want 0 — empty is not missing (§II.9)", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "memory directory is empty: "+r.configured) {
		t.Errorf("stderr = %q, want the empty-directory note", errOut)
	}
	if !strings.Contains(errOut, "mem list --dir "+r.stale) {
		t.Errorf("stderr = %q, want the displaced directory named and runnable", errOut)
	}
}

func TestEmptyConfiguredDirectoryStaysQuietWhenNothingWasDisplaced(t *testing.T) {
	dir := t.TempDir()
	_, errOut, code := exec("--dir", dir, "list")
	if code != exitOK {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if strings.Contains(errOut, "--dir "+dir) {
		t.Errorf("stderr = %q, want no displaced note", errOut)
	}
}

func TestTheDisplacedHintOffersEveryMatchInTheWinningTier(t *testing.T) {
	// The other-projects branch lists every match; the displaced one used to
	// name a single memory and call it "1 memory" whatever it found. Same
	// question, same answer shape.
	r := newRedirected(t, true)
	writeMemory(t, r.stale, "alpha_notes.md", fm("Alpha", "one", "project"))
	writeMemory(t, r.stale, "beta_notes.md", fm("Beta", "two", "project"))
	_, errOut, code := exec("read", "notes")
	if code != exitNotFound {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "2 memories "+displacedNote) {
		t.Errorf("stderr = %q, want both counted", errOut)
	}
	for _, want := range []string{"alpha_notes", "beta_notes"} {
		if !strings.Contains(errOut, "mem read --dir "+r.stale+" "+want) {
			t.Errorf("stderr = %q, want %q offered", errOut, want)
		}
	}
}

func TestTheDisplacedHintPrefersAnExactNameMatch(t *testing.T) {
	// The tiering that keeps a hint from being noise (§II.5) has to hold on the
	// displaced path too: an exact name beats a substring one, whatever order
	// the directory happens to list them in.
	r := newRedirected(t, true)
	writeMemory(t, r.stale, "notes.md", fm("Notes", "exact", "project"))
	writeMemory(t, r.stale, "aa_notes_old.md", fm("Old notes", "substring", "project"))
	_, errOut, code := exec("read", "notes")
	if code != exitNotFound {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "mem read --dir "+r.stale+" notes\n") {
		t.Errorf("stderr = %q, want the exact match offered", errOut)
	}
	if strings.Contains(errOut, "aa_notes_old") {
		t.Errorf("stderr = %q, want the substring match not offered beside an exact one", errOut)
	}
}

func TestASymlinkedDirectoryIsNotOfferedAsAnotherProject(t *testing.T) {
	// owns has to ask the same question SameDir asks, or --dir through a symlink
	// makes the directory you are reading right now show up as somewhere else to
	// look — the same category of wrong answer as the displaced default.
	//
	// An ambiguous name is what reaches the hints while pointed at courier itself:
	// `read note` matches two memories and neither exactly, so nothing is read
	// and the cross-project hints run — with the local store and courier being the
	// same directory spelled two ways.
	r := newRedirected(t, true)
	writeMemory(t, r.courier, "note_one.md", fm("Note one", "first", "project"))
	writeMemory(t, r.courier, "note_two.md", fm("Note two", "second", "project"))
	link := filepath.Join(r.tmp, "link-to-courier")
	if err := os.Symlink(r.courier, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, errOut, code := exec("--dir", link, "read", "note")
	if code != exitNotFound {
		t.Fatalf("exit code = %d, want 1 (stderr: %s)", code, errOut)
	}
	if strings.Contains(errOut, "mem read --project courier") {
		t.Errorf("stderr = %q, want courier not offered — it is the directory being read", errOut)
	}
}

func TestReadMissStillHintsAtGenuinelyOtherProjects(t *testing.T) {
	// The fix must not silence the hint that made refusing --all on read
	// worthwhile (§II.5) — only the entry that was never another project.
	newRedirected(t, true)
	_, errOut, code := exec("read", "courier")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "mem read --project courier courier") {
		t.Errorf("stderr = %q, want courier's memory still offered", errOut)
	}
}
