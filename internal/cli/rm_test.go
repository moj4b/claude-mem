package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRmDeletesTheFileAndNamesWhatItRemoved(t *testing.T) {
	dir := memDir(t, map[string]string{
		"project_roadmap.md": fm("Roadmap", "rename to Beacon", "project"),
		"user_prefs.md":      fm("User preferences", "terse dev", "user"),
	})
	out, errOut, code := exec("--dir", dir, "--force", "rm", "project_roadmap")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "project_roadmap.md")); !os.IsNotExist(err) {
		t.Errorf("project_roadmap.md still exists, want it removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "user_prefs.md")); err != nil {
		t.Errorf("user_prefs.md was disturbed: %v", err)
	}
	if !strings.Contains(out, "project_roadmap.md") {
		t.Errorf("stdout = %q, want it to name the file it removed", out)
	}
}

func TestRmAlsoRemovesTheIndexPointerLine(t *testing.T) {
	// MEMORY.md is what gets loaded into context each session, so a deleted
	// memory that keeps its pointer there is the failure this command exists
	// to prevent.
	index := "# Memory\n\n" +
		"- [Roadmap](project_roadmap.md) — rename to Beacon.\n" +
		"- [User preferences](user_prefs.md) — terse dev.\n"
	dir := memDir(t, map[string]string{
		"project_roadmap.md": fm("Roadmap", "rename to Beacon", "project"),
		"user_prefs.md":      fm("User preferences", "terse dev", "user"),
		"MEMORY.md":          index,
	})
	out, errOut, code := exec("--dir", dir, "--force", "rm", "project_roadmap")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut)
	}
	got, err := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "# Memory\n\n- [User preferences](user_prefs.md) — terse dev.\n"
	if string(got) != want {
		t.Errorf("MEMORY.md =\n%q\nwant\n%q", got, want)
	}
	if !strings.Contains(out, "MEMORY.md") {
		t.Errorf("stdout = %q, want it to say the index line went too", out)
	}
}

func TestRmReportsInboundLinksWithoutRewritingThem(t *testing.T) {
	linker := "---\nname: feedback_short_commit\n---\n\nKeep it terse per [[project_roadmap]].\n"
	other := "---\nname: user_prefs\n---\n\nAlso see [[project_roadmap]] for context.\n"
	dir := memDir(t, map[string]string{
		"project_roadmap.md":       fm("Roadmap", "rename to Beacon", "project"),
		"feedback_short_commit.md": linker,
		"user_prefs.md":            other,
	})
	_, errOut, code := exec("--dir", dir, "--force", "rm", "project_roadmap")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut)
	}
	for _, name := range []string{"feedback_short_commit", "user_prefs"} {
		if !strings.Contains(errOut, name) {
			t.Errorf("stderr = %q, want it to name the linking memory %q", errOut, name)
		}
	}
	// Reported, never rewritten — the [[link]] marks intent and must survive.
	got, err := os.ReadFile(filepath.Join(dir, "feedback_short_commit.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != linker {
		t.Errorf("linking memory was rewritten:\n%q\nwant it untouched\n%q", got, linker)
	}
}

func TestRmBacklinkNoteAgreesInNumber(t *testing.T) {
	dir := memDir(t, map[string]string{
		"project_roadmap.md":       fm("Roadmap", "rename to Beacon", "project"),
		"feedback_short_commit.md": "---\nname: x\n---\n\nSee [[project_roadmap]].\n",
	})
	_, errOut, _ := exec("--dir", dir, "--force", "rm", "project_roadmap")
	if !strings.Contains(errOut, "1 memory still links to") {
		t.Errorf("stderr = %q, want the singular to read \"1 memory still links to\"", errOut)
	}
}

func TestRmRefusesTheIndexItself(t *testing.T) {
	// MEMORY.md is in Readable() so `read` can print it, which makes it a legal
	// match target — but it is the index of everything, not a memory.
	index := "# Memory\n\n- [Roadmap](project_roadmap.md) — rename to Beacon.\n"
	for _, name := range []string{"MEMORY", "MEMORY.md", "memory"} {
		dir := memDir(t, map[string]string{
			"project_roadmap.md": fm("Roadmap", "rename to Beacon", "project"),
			"MEMORY.md":          index,
		})
		out, errOut, code := exec("--dir", dir, "rm", name)
		if code != 2 {
			t.Errorf("%q: exit code = %d, want 2", name, code)
		}
		if out != "" {
			t.Errorf("%q: stdout = %q, want empty", name, out)
		}
		if !strings.Contains(errOut, "MEMORY.md") {
			t.Errorf("%q: stderr = %q, want it to explain the index is not a memory", name, errOut)
		}
		got, err := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
		if err != nil || string(got) != index {
			t.Errorf("%q: the index was disturbed", name)
		}
	}
}

func TestRmRejectsAll(t *testing.T) {
	// rm resolves to exactly one memory, so widening it is a usage error —
	// and a silent widen here would delete across every project (§II.0).
	_, errOut, code := exec("--all", "rm", "project_roadmap")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut, "rm cannot be combined with --all") {
		t.Errorf("stderr = %q, want it to explain rm never widens", errOut)
	}
}

func TestRmOnAMissRemovesNothingAndSuggests(t *testing.T) {
	dir := memDir(t, map[string]string{
		"project_roadmap.md": fm("Roadmap", "rename to Beacon", "project"),
	})
	out, errOut, code := exec("--dir", dir, "rm", "projekt_roadmap_typo")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "did you mean") {
		t.Errorf("stderr = %q, want a suggestion", errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "project_roadmap.md")); err != nil {
		t.Error("a miss removed a file")
	}
}

func TestRmOnAnAmbiguousNameRemovesNothing(t *testing.T) {
	// Ambiguity is reported, never guessed (§II.8) — doubly so for a delete.
	dir := memDir(t, map[string]string{
		"feedback_docker.md": fm("Docker", "docker daily", "feedback"),
		"feedback_build.md":  fm("Build", "slow builds", "feedback"),
	})
	out, errOut, code := exec("--dir", dir, "rm", "feedback")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "ambiguous") {
		t.Errorf("stderr = %q, want it to report ambiguity", errOut)
	}
	for _, f := range []string{"feedback_docker.md", "feedback_build.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s was removed on an ambiguous name", f)
		}
	}
}

func TestRmWithoutANameIsAUsageError(t *testing.T) {
	_, errOut, code := exec("rm")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut, "rm requires a memory name") {
		t.Errorf("stderr = %q, want it to name the missing operand", errOut)
	}
}

func TestRmHonoursProjectRetargeting(t *testing.T) {
	root := projectsFixture(t)
	ledger := filepath.Join(root, "projects", "-w-ledger", "memory")
	_, errOut, code := exec("--project", "ledger", "--force", "rm", "project_api")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(ledger, "project_api.md")); !os.IsNotExist(err) {
		t.Error("project_api.md still exists, want it removed from the retargeted project")
	}
	// The project the cwd resolves to must be untouched.
	courier := filepath.Join(root, "projects", "-w-courier", "memory", "project_roadmap.md")
	if _, err := os.Stat(courier); err != nil {
		t.Error("rm --project disturbed another project")
	}
}

// hostileDir builds a memory directory next to files that must survive every
// removal: non-.md files, a dotfile, a subdirectory, and a sibling directory
// reachable only by escaping.
func hostileDir(t *testing.T) (dir, outside string) {
	t.Helper()
	root := t.TempDir()
	dir = filepath.Join(root, "memory")
	outside = filepath.Join(root, "outside")
	for _, d := range []string{dir, outside, filepath.Join(dir, "subdir")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "project_roadmap.md"), fm("Roadmap", "rename to Beacon", "project"))
	write(filepath.Join(dir, "notes.txt"), "not a memory\n")
	// courier really holds one of these (§I.2).
	write(filepath.Join(dir, ".consolidate-lock"), "lock\n")
	write(filepath.Join(dir, "subdir", "nested.md"), "nested\n")
	write(filepath.Join(outside, "secret.txt"), "SECRET\n")
	write(filepath.Join(outside, "target.md"), "OUTSIDE\n")
	return dir, outside
}

func TestRmNeverReachesOutsideTheMemoryDirectory(t *testing.T) {
	dir, outside := hostileDir(t)
	// These pin the outer property — nothing outside the directory is ever
	// touched — which every query below satisfies by failing to match at all.
	// They do not exercise memory.Removable: that guard sits behind a
	// successful match and is tested directly in internal/memory/path_test.go.
	// The query is matched against the loaded memories; it must never be
	// treated as a path, however path-like it looks.
	for _, q := range []string{
		"../outside/secret.txt", "../outside/target", "../../outside/target.md",
		"/etc/hosts", "./notes.txt", "subdir/nested", "..", ".",
	} {
		out, _, code := exec("--dir", dir, "rm", q)
		if code == 0 {
			t.Errorf("rm %q: exit code = 0, want a refusal (stdout: %q)", q, out)
		}
	}
	for _, p := range []string{
		filepath.Join(outside, "secret.txt"),
		filepath.Join(outside, "target.md"),
		filepath.Join(dir, "subdir", "nested.md"),
		filepath.Join(dir, "project_roadmap.md"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed, want it untouched", p)
		}
	}
}

func TestRmRemovesOnlyMarkdownMemoriesNotOtherFilesOrDirectories(t *testing.T) {
	// As above: these names never enter the pool, so they are refused at the
	// match rather than at the removability guard. Both layers matter, and
	// each is tested where it actually runs.
	dir, _ := hostileDir(t)
	for _, q := range []string{"notes", "notes.txt", "consolidate", "subdir", "lock"} {
		out, _, code := exec("--dir", dir, "rm", q)
		if code == 0 {
			t.Errorf("rm %q: exit code = 0, want a refusal (stdout: %q)", q, out)
		}
	}
	for _, p := range []string{"notes.txt", ".consolidate-lock", "subdir"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("%s was removed, want it untouched", p)
		}
	}
	// The one real memory is still removable — the guards refuse non-memories,
	// they do not break the command.
	if _, _, code := exec("--dir", dir, "--force", "rm", "project_roadmap"); code != 0 {
		t.Errorf("exit code = %d, want 0 — a real memory must still be removable", code)
	}
}

func TestRmUnlinksASymlinkedMemoryWithoutTouchingItsTarget(t *testing.T) {
	dir, outside := hostileDir(t)
	target := filepath.Join(outside, "target.md")
	link := filepath.Join(dir, "linked.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, errOut, code := exec("--dir", dir, "--force", "rm", "linked"); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Error("the symlink survived, want it removed")
	}
	body, err := os.ReadFile(target)
	if err != nil || string(body) != "OUTSIDE\n" {
		t.Errorf("the symlink's target was disturbed: %v %q", err, body)
	}
}

// nonInteractively runs the CLI as if stdin were a pipe, so the test does not
// depend on whether the suite itself was started from a terminal.
func nonInteractively(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	ttyWas := promptInteractive
	promptInteractive = func() bool { return false }
	t.Cleanup(func() { promptInteractive = ttyWas })
	return exec(args...)
}

// interactively runs the CLI as if stdin were a terminal, answering the
// prompt with `answer`.
func interactively(t *testing.T, answer string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	inWas, ttyWas := promptIn, promptInteractive
	promptIn = strings.NewReader(answer)
	promptInteractive = func() bool { return true }
	t.Cleanup(func() { promptIn, promptInteractive = inWas, ttyWas })
	return exec(args...)
}

func TestRmAsksBeforeRemovingAndDecliningKeepsEverything(t *testing.T) {
	index := "# Memory\n\n- [Roadmap](project_roadmap.md) — rename to Beacon.\n"
	dir := memDir(t, map[string]string{
		"project_roadmap.md": fm("Roadmap", "rename to Beacon", "project"),
		"MEMORY.md":          index,
	})
	_, errOut, code := interactively(t, "n\n", "--dir", dir, "rm", "roadmap")
	if code != 1 {
		t.Errorf("exit code = %d, want 1 — declining is not success", code)
	}
	// The prompt must name what the fuzzy match resolved to, or the user is
	// confirming a guess they cannot see.
	if !strings.Contains(errOut, "project_roadmap") {
		t.Errorf("stderr = %q, want the prompt to name the resolved memory", errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "project_roadmap.md")); err != nil {
		t.Error("the memory was removed after declining")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if string(got) != index {
		t.Errorf("MEMORY.md = %q, want it untouched after declining", got)
	}
}

func TestRmConfirmedRemovesTheMemory(t *testing.T) {
	for _, answer := range []string{"y\n", "Y\n", "yes\n", "YES\n"} {
		dir := memDir(t, map[string]string{
			"project_roadmap.md": fm("Roadmap", "rename to Beacon", "project"),
		})
		out, errOut, code := interactively(t, answer, "--dir", dir, "rm", "roadmap")
		if code != 0 {
			t.Errorf("%q: exit code = %d, want 0 (stderr: %s)", answer, code, errOut)
		}
		if !strings.Contains(out, "removed project_roadmap") {
			t.Errorf("%q: stdout = %q, want the removal reported", answer, out)
		}
		if _, err := os.Stat(filepath.Join(dir, "project_roadmap.md")); !os.IsNotExist(err) {
			t.Errorf("%q: the memory survived a confirmation", answer)
		}
	}
}

func TestRmTreatsABareEnterAndAnythingElseAsNo(t *testing.T) {
	// [y/N] means the safe answer is the default one.
	for _, answer := range []string{"\n", "", "no\n", "yep\n", "q\n"} {
		dir := memDir(t, map[string]string{
			"project_roadmap.md": fm("Roadmap", "rename to Beacon", "project"),
		})
		_, _, code := interactively(t, answer, "--dir", dir, "rm", "roadmap")
		if code != 1 {
			t.Errorf("%q: exit code = %d, want 1", answer, code)
		}
		if _, err := os.Stat(filepath.Join(dir, "project_roadmap.md")); err != nil {
			t.Errorf("%q: the memory was removed on a non-confirmation", answer)
		}
	}
}

func TestRmForceSkipsThePromptEntirely(t *testing.T) {
	for _, flag := range []string{"--force", "-f"} {
		dir := memDir(t, map[string]string{
			"project_roadmap.md": fm("Roadmap", "rename to Beacon", "project"),
		})
		// Stdin answers "n": --force must not consult it at all.
		out, errOut, code := interactively(t, "n\n", "--dir", dir, flag, "rm", "roadmap")
		if code != 0 {
			t.Errorf("%s: exit code = %d, want 0 (stderr: %s)", flag, code, errOut)
		}
		if strings.Contains(errOut, "[y/N]") {
			t.Errorf("%s: stderr = %q, want no prompt", flag, errOut)
		}
		if !strings.Contains(out, "removed project_roadmap") {
			t.Errorf("%s: stdout = %q, want the removal reported", flag, out)
		}
	}
}

func TestRmWithoutATerminalRefusesRatherThanAssumingYes(t *testing.T) {
	// Piped or scripted, nobody can answer — so the answer is not assumed.
	dir := memDir(t, map[string]string{
		"project_roadmap.md": fm("Roadmap", "rename to Beacon", "project"),
	})
	out, errOut, code := nonInteractively(t, "--dir", dir, "rm", "roadmap")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "--force") {
		t.Errorf("stderr = %q, want it to name the flag that proceeds", errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "project_roadmap.md")); err != nil {
		t.Error("the memory was removed without a confirmation")
	}
}

func TestRmRefusesABlankName(t *testing.T) {
	// `mem rm -f "$name"` with an unset variable must not resolve to whatever
	// happens to be there: the prefix tier matches every name, so a directory
	// holding one memory would otherwise lose it.
	dir := memDir(t, map[string]string{
		"only_memory.md": fm("Only", "just one", "project"),
	})
	// ".md" and friends normalize to the empty query, because MatchKey strips
	// the extension — `mem rm -f "$var.md"` with $var unset is the idiomatic
	// shell shape of the same accident.
	for _, blank := range []string{"", "   ", ".md", ".MD", "  .md  "} {
		out, errOut, code := exec("--dir", dir, "--force", "rm", blank)
		if code != 2 {
			t.Errorf("%q: exit code = %d, want 2", blank, code)
		}
		if out != "" {
			t.Errorf("%q: stdout = %q, want empty", blank, out)
		}
		if !strings.Contains(errOut, "requires a memory name") {
			t.Errorf("%q: stderr = %q, want it to name the missing operand", blank, errOut)
		}
		if _, err := os.Stat(filepath.Join(dir, "only_memory.md")); err != nil {
			t.Errorf("%q: the memory was removed on a blank name", blank)
		}
	}
}

func TestRmAbortsWhenTheIndexCannotBeRewritten(t *testing.T) {
	// The index is rewritten before the unlink, so a rewrite that cannot
	// happen costs nothing: the alternative is a pointer to a file that is
	// already gone, which is the exact failure this command exists to prevent
	// and which re-running cannot repair.
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	index := "# Memory\n\n- [Roadmap](project_roadmap.md) — rename to Beacon.\n"
	dir := memDir(t, map[string]string{
		"project_roadmap.md": fm("Roadmap", "rename to Beacon", "project"),
		"MEMORY.md":          index,
	})
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	out, errOut, code := exec("--dir", dir, "--force", "rm", "project_roadmap")
	if code == 0 {
		t.Errorf("exit code = 0, want a failure (stdout: %q)", out)
	}
	if !strings.Contains(errOut, "MEMORY.md") {
		t.Errorf("stderr = %q, want it to name the index it could not rewrite", errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "project_roadmap.md")); err != nil {
		t.Error("the memory was removed even though its index line could not be")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if string(got) != index {
		t.Errorf("MEMORY.md = %q, want it untouched", got)
	}
}

func TestRmRewritesTheIndexWithoutFollowingItOutOfTheDirectory(t *testing.T) {
	// A symlinked MEMORY.md must not turn the rewrite into a write outside the
	// memory directory.
	root := t.TempDir()
	dir := filepath.Join(root, "memory")
	outside := filepath.Join(root, "outside")
	for _, d := range []string{dir, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	shared := filepath.Join(outside, "shared_index.md")
	const sharedBody = "# Memory\n\n- [Roadmap](project_roadmap.md) — rename to Beacon.\n"
	if err := os.WriteFile(shared, []byte(sharedBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "project_roadmap.md"),
		[]byte(fm("Roadmap", "rename to Beacon", "project")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, filepath.Join(dir, "MEMORY.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, errOut, code := exec("--dir", dir, "--force", "rm", "project_roadmap"); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut)
	}
	got, err := os.ReadFile(shared)
	if err != nil || string(got) != sharedBody {
		t.Errorf("the file outside the memory directory was rewritten:\n%q\nwant\n%q", got, sharedBody)
	}
}

func TestRmAbortsWhenTheIndexExistsButCannotBeRead(t *testing.T) {
	// "No index" and "an index I could not open" are different situations.
	// Treating the second as the first removes the memory and leaves whatever
	// the index said about it in place.
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	dir := memDir(t, map[string]string{
		"project_roadmap.md": fm("Roadmap", "rename to Beacon", "project"),
		"MEMORY.md":          "# Memory\n\n- [Roadmap](project_roadmap.md) — rename.\n",
	})
	if err := os.Chmod(filepath.Join(dir, "MEMORY.md"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, "MEMORY.md"), 0o644) })

	out, errOut, code := exec("--dir", dir, "--force", "rm", "project_roadmap")
	if code == 0 {
		t.Errorf("exit code = 0, want a failure (stdout: %q)", out)
	}
	if !strings.Contains(errOut, "MEMORY.md") {
		t.Errorf("stderr = %q, want it to name the index it could not read", errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "project_roadmap.md")); err != nil {
		t.Error("the memory was removed although its index could not be read")
	}
}

func TestRmRefusesMoreThanOneName(t *testing.T) {
	// Silently dropping a name the user typed is the wrong answer for a
	// delete: they asked for two things and would be told only about one.
	dir := memDir(t, map[string]string{
		"aaa_one.md": fm("One", "first", "project"),
		"bbb_two.md": fm("Two", "second", "project"),
	})
	out, errOut, code := exec("--dir", dir, "--force", "rm", "aaa_one", "bbb_two")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "one memory") {
		t.Errorf("stderr = %q, want it to say rm takes one name", errOut)
	}
	for _, f := range []string{"aaa_one.md", "bbb_two.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s was removed", f)
		}
	}
}

func TestRmSaysSoWhenTheIndexStillPointsAtTheRemovedMemory(t *testing.T) {
	// A bare link is left alone on purpose — deleting a whole line that is not
	// a bullet risks destroying prose. But the user must be told, or they are
	// left with the dangling pointer this command promises to clear.
	dir := memDir(t, map[string]string{
		"project_roadmap.md": fm("Roadmap", "rename to Beacon", "project"),
		"MEMORY.md":          "# Memory\n\n[Roadmap](project_roadmap.md) — a bare link.\n",
	})
	out, errOut, code := exec("--dir", dir, "--force", "rm", "project_roadmap")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut)
	}
	if !strings.Contains(errOut, "MEMORY.md") || !strings.Contains(errOut, "still") {
		t.Errorf("stderr = %q, want it to say the index still points at the memory", errOut)
	}
	if strings.Contains(out, "and its line") {
		t.Errorf("stdout = %q, want no claim that the index line went", out)
	}
}
