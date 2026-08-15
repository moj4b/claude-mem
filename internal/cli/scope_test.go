package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// projectsFixture builds a config root holding several projects with memory,
// including an empty one, and points the process env at it.
func projectsFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	write := func(slug, file, body string) {
		dir := filepath.Join(root, "projects", slug, "memory")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if file != "" {
			if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A partial index: it lists only two of the three memories. Index drift is
	// tolerated by design (§I.3), and keeping feedback_docker.md out of it
	// leaves the docker search assertions below unambiguous.
	write("-w-courier", "MEMORY.md",
		"- [User preferences](user_prefs.md) — terse dev\n"+
			"- [Roadmap](project_roadmap.md) — rename to Beacon\n")
	write("-w-courier", "user_prefs.md", fm("User preferences", "terse dev", "user"))
	write("-w-courier", "project_roadmap.md", fm("Roadmap", "rename to Beacon", "project"))
	write("-w-courier", "feedback_docker.md", fm("Docker", "uses docker daily", "feedback"))
	write("-w-ledger", "user_prefs.md", fm("User preferences", "other project", "user"))
	write("-w-ledger", "project_api.md", fm("API", "v2 rollout", "project"))
	write("-w-solo", "only.md", fm("Only", "just one", "project"))
	write("-w-blank", "", "") // exists but empty — claude-mem's real state today
	// A project directory with no memory subdirectory at all (7 exist for real).
	if err := os.MkdirAll(filepath.Join(root, "projects", "-w-nomem"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	t.Setenv("CLAUDE_MEM_DIR", "")
	return root
}

func TestProjectsListsEveryProjectWithAMemoryDirectoryIncludingEmptyOnes(t *testing.T) {
	// §II.7a: knowing a project exists but holds nothing is useful information.
	projectsFixture(t)
	out, _, code := exec("projects")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 {
		t.Errorf("got %d projects, want 4 (blank included, nomem excluded):\n%s", len(lines), out)
	}
	if !strings.Contains(out, "blank\t0\t") {
		t.Errorf("output = %q, want the empty project listed with count 0", out)
	}
	if strings.Contains(out, "nomem") {
		t.Errorf("output = %q, want a project with no memory/ excluded", out)
	}
}

func TestProjectsSortedByCountDescendingThenName(t *testing.T) {
	projectsFixture(t)
	out, _, _ := exec("projects")
	var got []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		got = append(got, strings.Split(l, "\t")[0])
	}
	want := "courier,ledger,solo,blank" // 3, 2, 1, 0
	if strings.Join(got, ",") != want {
		t.Errorf("order = %v, want %s", got, want)
	}
}

func TestProjectsNonTTYIsNameCountAbsolutePath(t *testing.T) {
	root := projectsFixture(t)
	out, _, _ := exec("projects")
	first := strings.Split(strings.Split(out, "\n")[0], "\t")
	if len(first) != 3 {
		t.Fatalf("got %d columns, want 3: %q", len(first), out)
	}
	if first[0] != "courier" || first[1] != "3" {
		t.Errorf("got %v, want [courier 3 <path>]", first)
	}
	want := filepath.Join(root, "projects", "-w-courier", "memory")
	if first[2] != want {
		t.Errorf("path = %q, want %q", first[2], want)
	}
}

func TestProjectsTTYAlignsCountsEvenWhenNamesAreColoured(t *testing.T) {
	// Padding must be computed from the visible name, not the string length —
	// ANSI escapes are bytes but occupy no columns.
	projectsFixture(t)
	stores := byCountThenName(discoverProjects(filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects")))
	for _, color := range []bool{false, true} {
		var buf bytes.Buffer
		renderProjects(stores, 6, &buf, true, color)
		var cols []int
		for _, line := range strings.Split(buf.String(), "\n") {
			plain := stripANSI(line)
			if !strings.HasPrefix(plain, "  ") || strings.TrimSpace(plain) == "" {
				continue
			}
			// Drop the trailing "(no index)" note so the count is last.
			if i := strings.Index(plain, "   (no index)"); i >= 0 {
				plain = plain[:i]
			}
			cols = append(cols, strings.LastIndex(strings.TrimRight(plain, " "), " ")+1)
		}
		if len(cols) != 4 {
			t.Fatalf("color=%v: got %d rows, want 4:\n%s", color, len(cols), buf.String())
		}
		for i, c := range cols {
			if c != cols[0] {
				t.Errorf("color=%v: row %d count starts at column %d, want %d", color, i, c, cols[0])
			}
		}
	}
}

// stripANSI removes colour escapes so tests can measure visible columns.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func TestProjectsWithNoProjectsAtAllIsStillExitZero(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	out, errOut, code := exec("projects")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "no projects have memory yet") {
		t.Errorf("stderr = %q, want the note", errOut)
	}
}

func TestProjectFlagMatchesHumanNameNotSlug(t *testing.T) {
	projectsFixture(t)
	out, errOut, code := exec("--project", "courier", "list")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut)
	}
	if n := len(strings.Split(strings.TrimSpace(out), "\n")); n != 3 {
		t.Errorf("listed %d memories, want 3", n)
	}
}

func TestUnknownProjectIsExitOneListingCandidates(t *testing.T) {
	// §II.0: ambiguous or unknown project → exit 1.
	projectsFixture(t)
	out, errOut, code := exec("--project", "zzz", "list")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "no project named 'zzz'") {
		t.Errorf("stderr = %q, want it to name the project", errOut)
	}
}

func TestProjectFlagIgnoresCwdSettingsAndEnvDir(t *testing.T) {
	// §II.3: those settings belong to the directory you are standing in, not
	// the project you asked for. Letting them redirect --project is incoherent.
	root := projectsFixture(t)
	cwd := t.TempDir()
	writeSettingsFile(t, filepath.Join(cwd, ".claude", "settings.json"), `{"memoryDir":"/nope/nope"}`)
	inDir(t, cwd)
	t.Setenv("CLAUDE_MEM_DIR", "/nope/nope")
	t.Setenv("CLAUDE_CONFIG_DIR", root)

	out, errOut, code := exec("--project", "courier", "list")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut)
	}
	if n := len(strings.Split(strings.TrimSpace(out), "\n")); n != 3 {
		t.Errorf("listed %d memories, want courier's 3", n)
	}
}

func TestListAllGroupsByProjectAndOmitsEmptyOnes(t *testing.T) {
	projectsFixture(t)
	out, errOut, code := exec("--all", "list")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 6 {
		t.Errorf("got %d lines, want 6 memories:\n%s", len(lines), out)
	}
	// Non-TTY gains a leading project column (§II.4).
	for _, l := range lines {
		if n := len(strings.Split(l, "\t")); n != 4 {
			t.Errorf("line %q has %d columns, want 4 (project, name, type, desc)", l, n)
		}
	}
	if strings.Contains(out, "blank\t") {
		t.Errorf("output = %q, want the empty project omitted from the listing", out)
	}
	// ...but still counted in the header, which goes to stderr when piped.
	if !strings.Contains(errOut, "4 directories, 6 memories") {
		t.Errorf("stderr = %q, want the scope header counting all 4 directories", errOut)
	}
}

func TestSearchAllPrefixesTheProject(t *testing.T) {
	projectsFixture(t)
	out, _, code := exec("--all", "search", "docker")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	// §II.6: photo-lib:project_photo_lib.md:8:docker compose...
	if !strings.HasPrefix(out, "courier:feedback_docker.md:") {
		t.Errorf("output = %q, want project:file:line:text", out)
	}
}

func TestLocalSearchDoesNotWiden(t *testing.T) {
	// The most important behavioural contract (§II.0).
	root := projectsFixture(t)
	solo := filepath.Join(root, "projects", "-w-solo", "memory")
	_, _, code := exec("--dir", solo, "search", "docker")
	if code != 1 {
		t.Errorf("exit code = %d, want 1 — a local search must not see other projects", code)
	}
}

func TestSearchMissHintsAtAllWhenOtherProjectsHaveHits(t *testing.T) {
	root := projectsFixture(t)
	solo := filepath.Join(root, "projects", "-w-solo", "memory")
	_, errOut, code := exec("--dir", solo, "search", "docker")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "mem search --all docker") {
		t.Errorf("stderr = %q, want the --all hint (§II.6)", errOut)
	}
}

func TestReadMissHintsAtEveryOtherProjectHoldingTheName(t *testing.T) {
	// The payoff for refusing --all on read (§II.5).
	root := projectsFixture(t)
	solo := filepath.Join(root, "projects", "-w-solo", "memory")
	out, errOut, code := exec("--dir", solo, "read", "user_prefs")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 — it did not read anything", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if n := strings.Count(errOut, "mem read --project"); n != 2 {
		t.Errorf("stderr has %d hint lines, want 2 (courier and ledger):\n%s", n, errOut)
	}
	for _, p := range []string{"courier", "ledger"} {
		if !strings.Contains(errOut, "mem read --project "+p+" user_prefs") {
			t.Errorf("stderr = %q, want an actionable command for %q", errOut, p)
		}
	}
}

func TestPathAllListsEveryMemoryDirectorySortedByProject(t *testing.T) {
	root := projectsFixture(t)
	out, _, code := exec("path", "--all")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 {
		t.Errorf("got %d paths, want 4 (empty directories included):\n%s", len(lines), out)
	}
	want := []string{"-w-blank", "-w-courier", "-w-ledger", "-w-solo"} // by project name
	for i, l := range lines {
		if !filepath.IsAbs(l) {
			t.Errorf("path %q is not absolute", l)
		}
		if !strings.Contains(l, want[i]) {
			t.Errorf("path %d = %q, want it to be %s", i, l, want[i])
		}
	}
	_ = root
}

func TestPathAllWithNoMemoryAnywhereIsExit3(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	out, _, code := exec("path", "--all")
	if code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
}
