package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moj4b/claude-mem/internal/memory"
)

// completeFixture points the environment at a courier-like memory directory.
func completeFixture(t *testing.T) string {
	t.Helper()
	root := projectsFixture(t)
	t.Setenv("CLAUDE_MEM_DIR", filepath.Join(root, "projects", "-w-courier", "memory"))
	return root
}

func complete(t *testing.T, words ...string) []string {
	t.Helper()
	args := append([]string{"__complete"}, words...)
	out, errOut, code := exec(args...)
	// Non-negotiable: a noisy __complete corrupts the user's prompt (§II.10).
	if code != 0 {
		t.Errorf("__complete %v: exit code = %d, want 0", words, code)
	}
	if errOut != "" {
		t.Errorf("__complete %v: stderr = %q, want empty", words, errOut)
	}
	if out == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n")
}

func TestCompleteWithNoWordsOffersEverySubcommandAndFlag(t *testing.T) {
	completeFixture(t)
	got := complete(t)
	for _, want := range []string{
		"list", "read", "show", "search", "path", "projects", "completion",
		"--help", "--version", "--dir", "--all", "--project",
	} {
		if !contains(got, want) {
			t.Errorf("candidates %v missing %q", got, want)
		}
	}
	if contains(got, "__complete") {
		t.Error("__complete is hidden and must not be offered")
	}
}

func TestCompleteFiltersByPrefixOfTheWordBeingCompleted(t *testing.T) {
	completeFixture(t)
	if got := complete(t, "li"); len(got) != 1 || got[0] != "list" {
		t.Errorf("complete(li) = %v, want [list]", got)
	}
	if got := complete(t, "pro"); len(got) != 1 || got[0] != "projects" {
		t.Errorf("complete(pro) = %v, want [projects]", got)
	}
}

func TestCompleteReadOffersMemoryNamesWithoutMdPlusIndex(t *testing.T) {
	completeFixture(t)
	got := complete(t, "read", "")
	if !contains(got, "project_roadmap") {
		t.Errorf("candidates %v missing a memory name", got)
	}
	if !contains(got, memory.IndexName) {
		t.Errorf("candidates %v missing %q — the index is readable (§II.10)", got, memory.IndexName)
	}
	for _, c := range got {
		if strings.HasSuffix(c, ".md") {
			t.Errorf("candidate %q keeps its .md suffix", c)
		}
	}
}

func TestCompleteShowBehavesLikeRead(t *testing.T) {
	completeFixture(t)
	read, show := complete(t, "read", ""), complete(t, "show", "")
	if strings.Join(read, ",") != strings.Join(show, ",") {
		t.Errorf("show = %v, want the same as read = %v", show, read)
	}
}

func TestCompleteReadFiltersNamesByPrefix(t *testing.T) {
	completeFixture(t)
	got := complete(t, "read", "project")
	if len(got) != 1 || got[0] != "project_roadmap" {
		t.Errorf("complete(read project) = %v, want [project_roadmap]", got)
	}
}

func TestCompleteAfterProjectFlagOffersProjectNamesNotSlugs(t *testing.T) {
	completeFixture(t)
	got := complete(t, "--project", "")
	if !contains(got, "courier") {
		t.Errorf("candidates %v missing %q", got, "courier")
	}
	for _, c := range got {
		if strings.HasPrefix(c, "-w-") {
			t.Errorf("candidate %q is a slug; offer human names (§II.10)", c)
		}
	}
}

func TestCompleteHonoursAnEarlierProjectFlag(t *testing.T) {
	// `mem read --project courier <TAB>` must offer courier's memories, not the local
	// project's — completion has to honour scope (§II.10).
	root := projectsFixture(t)
	t.Setenv("CLAUDE_MEM_DIR", filepath.Join(root, "projects", "-w-solo", "memory"))

	got := complete(t, "--project", "courier", "read", "")
	if !contains(got, "project_roadmap") {
		t.Errorf("candidates %v want courier's memories", got)
	}
	if contains(got, "only") {
		t.Errorf("candidates %v leaked the local project's memories", got)
	}
}

func TestCompletePathOffersItsTwoFlags(t *testing.T) {
	completeFixture(t)
	got := complete(t, "path", "")
	if len(got) != 2 || !contains(got, "--explain") || !contains(got, "--all") {
		t.Errorf("complete(path) = %v, want [--all --explain]", got)
	}
}

func TestCompleteListAndSearchOfferScopeFlagsOnlyForACourier(t *testing.T) {
	completeFixture(t)
	for _, cmd := range []string{"list", "search"} {
		got := complete(t, cmd, "-")
		if !contains(got, "--all") || !contains(got, "--project") {
			t.Errorf("complete(%s -) = %v, want the scope flags", cmd, got)
		}
		if got := complete(t, cmd, "some"); len(got) != 0 {
			t.Errorf("complete(%s some) = %v, want nothing", cmd, got)
		}
	}
}

func TestCompleteCompletionOffersBash(t *testing.T) {
	completeFixture(t)
	if got := complete(t, "completion", ""); len(got) != 1 || got[0] != "bash" {
		t.Errorf("complete(completion) = %v, want [bash]", got)
	}
}

func TestCompleteOffersNothingInUnknownContexts(t *testing.T) {
	completeFixture(t)
	if got := complete(t, "projects", ""); len(got) != 0 {
		t.Errorf("complete(projects) = %v, want nothing", got)
	}
	if got := complete(t, "read", "a", ""); len(got) != 0 {
		t.Errorf("complete(read a) = %v, want nothing — read takes one name", got)
	}
}

func TestCompleteIsSilentAndZeroWhenEverythingIsWrong(t *testing.T) {
	// A broken completion must not spew onto the user's prompt line (§II.10).
	t.Setenv("CLAUDE_MEM_DIR", "/nope/nope")
	if got := complete(t, "read", ""); len(got) != 0 {
		t.Errorf("candidates = %v, want none for a broken directory", got)
	}
	t.Setenv("CLAUDE_MEM_DIR", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "/nope/nope")
	inDir(t, t.TempDir())
	if got := complete(t, "read", ""); len(got) != 0 {
		t.Errorf("candidates = %v, want none when there is no memory at all", got)
	}
}

func TestCompleteDoesNotReadFileBodies(t *testing.T) {
	// §II.10: filenames only, to stay inside the 20 ms budget. An unreadable
	// file must therefore still be offered.
	dir := t.TempDir()
	path := filepath.Join(dir, "locked.md")
	if err := os.WriteFile(path, []byte(fm("Locked", "d", "user")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Skip("cannot chmod in this environment")
	}
	t.Cleanup(func() { os.Chmod(path, 0o644) })
	if os.Geteuid() == 0 {
		t.Skip("running as root; permissions are not enforced")
	}
	t.Setenv("CLAUDE_MEM_DIR", dir)
	if got := complete(t, "read", ""); !contains(got, "locked") {
		t.Errorf("candidates = %v, want %q — completion must not read bodies", got, "locked")
	}
}

func TestCompletionBashScriptIsTheDocumentedShim(t *testing.T) {
	out, errOut, code := exec("completion", "bash")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if errOut != "" {
		t.Errorf("stderr = %q, want empty", errOut)
	}
	for _, want := range []string{
		"_mem_complete()",
		`mapfile -t candidates < <(mem __complete "${COMP_WORDS[@]:1}" 2>/dev/null)`,
		`COMPREPLY=("${candidates[@]}")`,
		"complete -F _mem_complete mem",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("script missing %q; got:\n%s", want, out)
		}
	}
}

func TestCompletionRejectsAnUnknownShell(t *testing.T) {
	out, errOut, code := exec("completion", "zsh")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "bash") {
		t.Errorf("stderr = %q, want it to name the supported shell", errOut)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
