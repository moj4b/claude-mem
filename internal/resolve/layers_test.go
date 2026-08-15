package resolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture builds a self-contained config root + workspace:
//
//	<tmp>/root/projects/<slug of <tmp>/w/proj>/memory   (exists)
//	<tmp>/root/projects/<slug of <tmp>/w/other>/memory  (exists)
//	<tmp>/w/proj/sub/deep                               (cwd for walk tests)
type fixture struct {
	tmp, root, proj, other, projMem, otherMem string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir()) // /tmp is a symlink on some systems
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{
		tmp:   tmp,
		root:  filepath.Join(tmp, "root"),
		proj:  filepath.Join(tmp, "w", "proj"),
		other: filepath.Join(tmp, "w", "other"),
	}
	f.projMem = filepath.Join(f.root, "projects", Slugify(f.proj), "memory")
	f.otherMem = filepath.Join(f.root, "projects", Slugify(f.other), "memory")
	for _, d := range []string{f.projMem, f.otherMem, filepath.Join(f.proj, "sub", "deep"), f.other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f fixture) Resolver(cwd string) Resolver {
	return Resolver{cwd: cwd, home: f.tmp, root: f.root}
}

func mustResolve(t *testing.T, r Resolver) Resolution {
	t.Helper()
	res, err := r.Resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return res
}

func TestDefaultPathFromProjectRoot(t *testing.T) {
	f := newFixture(t)
	got := mustResolve(t, f.Resolver(f.proj))
	if got.Dir != f.projMem {
		t.Errorf("Dir = %q, want %q", got.Dir, f.projMem)
	}
	if !got.Exists {
		t.Error("Exists = false, want true")
	}
	if got.Via != "default path" {
		t.Errorf("Via = %q, want %q", got.Via, "default path")
	}
}

func TestAncestorWalkFindsParentMemoryFromSubdirectory(t *testing.T) {
	// The normal case, not an edge case (§II.0): claude was launched at the
	// project root, so a deep cwd must climb to find it.
	f := newFixture(t)
	got := mustResolve(t, f.Resolver(filepath.Join(f.proj, "sub", "deep")))
	if got.Dir != f.projMem {
		t.Errorf("Dir = %q, want %q (walk from sub/deep)", got.Dir, f.projMem)
	}
}

func TestAncestorWalkStopsAtHomeAndReportsNothingFound(t *testing.T) {
	f := newFixture(t)
	lonely := filepath.Join(f.tmp, "w", "nomemory")
	if err := os.MkdirAll(lonely, 0o755); err != nil {
		t.Fatal(err)
	}
	got := mustResolve(t, f.Resolver(lonely))
	if got.Exists {
		t.Errorf("Exists = true for %q, want false", got.Dir)
	}
	// The reported path is the default applied to cwd (§II.3 step 4), so the
	// exit-3 message can name what it looked for.
	want := filepath.Join(f.root, "projects", Slugify(lonely), "memory")
	if got.Dir != want {
		t.Errorf("Dir = %q, want %q", got.Dir, want)
	}
}

func TestGitRootBoundsTheWalk(t *testing.T) {
	// proj/sub is its own git repo, so the walk must stop there and NOT climb
	// to proj's memory.
	f := newFixture(t)
	sub := filepath.Join(f.proj, "sub")
	if err := os.MkdirAll(filepath.Join(sub, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := mustResolve(t, f.Resolver(filepath.Join(sub, "deep")))
	if got.Exists {
		t.Errorf("walk escaped the git root and found %q", got.Dir)
	}
}

func TestFallbackScanMatchesWhenPunctuationTransformDiffers(t *testing.T) {
	// §II.3 step 3b: we must not guess how '.' is transformed. Here the real
	// directory doubled it; the scan must still find it.
	f := newFixture(t)
	dotted := filepath.Join(f.tmp, "w", "my.app")
	if err := os.MkdirAll(dotted, 0o755); err != nil {
		t.Fatal(err)
	}
	onDisk := strings.ReplaceAll(Slugify(dotted), "my-app", "my--app")
	realMem := filepath.Join(f.root, "projects", onDisk, "memory")
	if err := os.MkdirAll(realMem, 0o755); err != nil {
		t.Fatal(err)
	}
	got := mustResolve(t, f.Resolver(dotted))
	if got.Dir != realMem {
		t.Errorf("Dir = %q, want %q (fallback scan)", got.Dir, realMem)
	}
}

func TestDirFlagWinsAndIsUsedVerbatim(t *testing.T) {
	f := newFixture(t)
	r := f.Resolver(f.proj)
	r.dirFlag = f.otherMem
	got := mustResolve(t, r)
	if got.Dir != f.otherMem {
		t.Errorf("Dir = %q, want %q", got.Dir, f.otherMem)
	}
	if got.Via != "--dir flag" {
		t.Errorf("Via = %q, want %q", got.Via, "--dir flag")
	}
}

func TestNonexistentOverrideErrorsRatherThanFallingThrough(t *testing.T) {
	// Layers 1-3 are explicit intent: a bad one is an error to report, never a
	// reason to silently use the default (§II.3).
	f := newFixture(t)
	for _, tc := range []struct {
		name, via string
		apply     func(*Resolver)
	}{
		{"--dir", "--dir flag", func(r *Resolver) { r.dirFlag = "/nope/nope" }},
		{"$CLAUDE_MEM_DIR", "$CLAUDE_MEM_DIR", func(r *Resolver) { r.envDir = "/nope/nope" }},
	} {
		r := f.Resolver(f.proj)
		tc.apply(&r)
		got := mustResolve(t, r)
		if got.Exists {
			t.Errorf("%s: Exists = true, want false", tc.name)
		}
		if got.Dir != "/nope/nope" {
			t.Errorf("%s: Dir = %q, want the override verbatim", tc.name, got.Dir)
		}
		if got.Via != tc.via {
			t.Errorf("%s: Via = %q, want %q", tc.name, got.Via, tc.via)
		}
	}
}

func TestEnvDirOutranksSettingsAndDefault(t *testing.T) {
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.json"), `{"memoryDir":`+quote(f.projMem)+`}`)
	r := f.Resolver(f.proj)
	r.envDir = f.otherMem
	if got := mustResolve(t, r); got.Dir != f.otherMem {
		t.Errorf("Dir = %q, want %q", got.Dir, f.otherMem)
	}
}

func TestProjectSettingsOutrankUserSettingsAndDefault(t *testing.T) {
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.root, "settings.json"), `{"memoryDir":`+quote(f.projMem)+`}`)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.json"), `{"memoryDir":`+quote(f.otherMem)+`}`)
	got := mustResolve(t, f.Resolver(f.proj))
	if got.Dir != f.otherMem {
		t.Errorf("Dir = %q, want project settings %q", got.Dir, f.otherMem)
	}
	if got.Via != "project settings" {
		t.Errorf("Via = %q, want %q", got.Via, "project settings")
	}
}

func TestSettingsLocalOutranksSettings(t *testing.T) {
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.json"), `{"memoryDir":`+quote(f.projMem)+`}`)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.local.json"), `{"memoryDir":`+quote(f.otherMem)+`}`)
	if got := mustResolve(t, f.Resolver(f.proj)); got.Dir != f.otherMem {
		t.Errorf("Dir = %q, want settings.local %q", got.Dir, f.otherMem)
	}
}

func TestUserSettingsUsedWhenNoProjectSettings(t *testing.T) {
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.root, "settings.json"), `{"memoryDir":`+quote(f.otherMem)+`}`)
	got := mustResolve(t, f.Resolver(f.proj))
	if got.Dir != f.otherMem {
		t.Errorf("Dir = %q, want user settings %q", got.Dir, f.otherMem)
	}
	if got.Via != "user settings" {
		t.Errorf("Via = %q, want %q", got.Via, "user settings")
	}
}

func TestSettingsKeyVariantsAllAccepted(t *testing.T) {
	// §I.7 is unverified, so be liberal (§II.3).
	f := newFixture(t)
	for _, body := range []string{
		`{"memoryDir":%s}`,
		`{"memoryPath":%s}`,
		`{"memoryDirectory":%s}`,
		`{"claudeMemoryDir":%s}`,
		`{"memory":{"dir":%s}}`,
		`{"memory":{"path":%s}}`,
	} {
		writeSettings(t, filepath.Join(f.proj, ".claude", "settings.json"),
			strings.Replace(body, "%s", quote(f.otherMem), 1))
		if got := mustResolve(t, f.Resolver(f.proj)); got.Dir != f.otherMem {
			t.Errorf("%s: Dir = %q, want %q", body, got.Dir, f.otherMem)
		}
	}
}

// autoMemoryDirectory is the key Claude Code actually reads. §I.7 could not
// verify a key and guessed four; the 2.1.x bundle settles it — none of the four
// appear in it, and this one does, described as "Custom directory path for
// auto-memory storage. Supports ~/ prefix for home directory expansion. When
// unset, defaults to ~/.claude/projects/<sanitized-cwd>/memory/".
func TestAutoMemoryDirectoryInSettingsLocal(t *testing.T) {
	// The reported bug: the settings say the memory lives in the repo, and the
	// computed default directory also exists. Reading only the default shows a
	// different project's memories, or none at all.
	f := newFixture(t)
	inRepo := filepath.Join(f.proj, ".claude", "memory")
	if err := os.MkdirAll(inRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.local.json"),
		`{"autoMemoryDirectory":`+quote(inRepo)+`}`)
	got := mustResolve(t, f.Resolver(f.proj))
	if got.Dir != inRepo {
		t.Errorf("Dir = %q, want %q", got.Dir, inRepo)
	}
	if !got.Exists {
		t.Error("Exists = false, want true")
	}
	if got.Via != "project settings" {
		t.Errorf("Via = %q, want %q", got.Via, "project settings")
	}
}

func TestAutoMemoryDirectoryFoundFromSubdirectory(t *testing.T) {
	f := newFixture(t)
	inRepo := filepath.Join(f.proj, ".claude", "memory")
	if err := os.MkdirAll(inRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.local.json"),
		`{"autoMemoryDirectory":`+quote(inRepo)+`}`)
	if got := mustResolve(t, f.Resolver(filepath.Join(f.proj, "sub", "deep"))); got.Dir != inRepo {
		t.Errorf("Dir = %q, want %q (walk from sub/deep)", got.Dir, inRepo)
	}
}

func TestAutoMemoryDirectoryInUserSettings(t *testing.T) {
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.root, "settings.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem)+`}`)
	got := mustResolve(t, f.Resolver(f.proj))
	if got.Dir != f.otherMem {
		t.Errorf("Dir = %q, want user settings %q", got.Dir, f.otherMem)
	}
	if got.Via != "user settings" {
		t.Errorf("Via = %q, want %q", got.Via, "user settings")
	}
}

func TestAutoMemoryDirectoryTildeExpansion(t *testing.T) {
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.json"),
		`{"autoMemoryDirectory":"~/w/other"}`)
	want := filepath.Join(f.tmp, "w", "other")
	if got := mustResolve(t, f.Resolver(f.proj)); got.Dir != want {
		t.Errorf("Dir = %q, want %q", got.Dir, want)
	}
}

func TestAutoMemoryDirectoryTrailingSlashStripped(t *testing.T) {
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem+"/")+`}`)
	if got := mustResolve(t, f.Resolver(f.proj)); got.Dir != f.otherMem {
		t.Errorf("Dir = %q, want %q without the trailing separator", got.Dir, f.otherMem)
	}
}

func TestAutoMemoryDirectoryRelativeIsIgnored(t *testing.T) {
	// Claude Code validates this key as absolute-after-~-expansion and drops it
	// otherwise, falling back to the default. Resolving it against the settings
	// file instead — as the legacy keys do — would point mem at a directory
	// Claude Code never writes to.
	f := newFixture(t)
	if err := os.MkdirAll(filepath.Join(f.proj, "mem"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.json"),
		`{"autoMemoryDirectory":"../mem"}`)
	if got := mustResolve(t, f.Resolver(f.proj)); got.Dir != f.projMem {
		t.Errorf("Dir = %q, want the default %q", got.Dir, f.projMem)
	}
}

func TestAutoMemoryDirectoryOutranksLegacyKeysInSameFile(t *testing.T) {
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.json"),
		`{"memoryDir":`+quote(f.projMem)+`,"autoMemoryDirectory":`+quote(f.otherMem)+`}`)
	if got := mustResolve(t, f.Resolver(f.proj)); got.Dir != f.otherMem {
		t.Errorf("Dir = %q, want the real key's value %q", got.Dir, f.otherMem)
	}
}

func TestSettingsPointingNowhereWinsOverExistingDefault(t *testing.T) {
	// A configured autoMemoryDirectory is where Claude Code looks whether or not
	// it is there: its resolver validates the value's FORM — ~/ expanded,
	// absolute — and returns it without ever consulting the filesystem, falling
	// back to the default only when the key is unset or malformed. Verified
	// against a real session: pointed at a missing directory with memories
	// planted at the default path, Claude Code reported the missing one, created
	// it, and never read the default. So a settings hit must end the walk, or
	// mem reports memories Claude Code does not read.
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.local.json"),
		`{"autoMemoryDirectory":"/nope/nope"}`)
	got := mustResolve(t, f.Resolver(f.proj))
	if got.Dir != "/nope/nope" {
		t.Errorf("Dir = %q, want the configured path, not the default %q", got.Dir, f.projMem)
	}
	if got.Exists {
		t.Error("Exists = true, want false")
	}
	if got.Via != "project settings" {
		t.Errorf("Via = %q, want %q", got.Via, "project settings")
	}
}

func TestUserSettingsPointingNowhereWinsOverExistingDefault(t *testing.T) {
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.root, "settings.json"),
		`{"autoMemoryDirectory":"/nope/nope"}`)
	got := mustResolve(t, f.Resolver(f.proj))
	if got.Dir != "/nope/nope" {
		t.Errorf("Dir = %q, want the configured path, not the default %q", got.Dir, f.projMem)
	}
	if got.Via != "user settings" {
		t.Errorf("Via = %q, want %q", got.Via, "user settings")
	}
}

func TestSettingsPointingNowhereIsNamedWhenNothingExists(t *testing.T) {
	// With no memory anywhere, the miss should name the configured path — that
	// is the one the user can act on — rather than the computed default, which
	// they never asked for.
	f := newFixture(t)
	lonely := filepath.Join(f.tmp, "w", "nomemory")
	if err := os.MkdirAll(lonely, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, filepath.Join(lonely, ".claude", "settings.local.json"),
		`{"autoMemoryDirectory":"/nope/nope"}`)
	got := mustResolve(t, f.Resolver(lonely))
	if got.Exists {
		t.Errorf("Exists = true for %q, want false", got.Dir)
	}
	if got.Dir != "/nope/nope" {
		t.Errorf("Dir = %q, want the configured path %q", got.Dir, "/nope/nope")
	}
}

func TestSettingsLocalWinsEvenWhenItsTargetIsMissing(t *testing.T) {
	// Claude Code takes localSettings ?? projectSettings: the nearer file that
	// SETS the key wins outright, and a missing target is not a reason to prefer
	// the farther one.
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.local.json"),
		`{"autoMemoryDirectory":"/nope/nope"}`)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem)+`}`)
	if got := mustResolve(t, f.Resolver(f.proj)); got.Dir != "/nope/nope" {
		t.Errorf("Dir = %q, want settings.local's value even though it is missing", got.Dir)
	}
}

func TestMalformedSettingsLocalFallsThroughToSettingsJSON(t *testing.T) {
	// Not-set and malformed are the same thing to Claude Code's ?? chain: a
	// value that fails validation never becomes the answer, so the next source
	// is consulted. This is the ONE fall-through, and it is about the value's
	// form, never about whether the directory is there.
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.local.json"),
		`{"autoMemoryDirectory":"relative/nope"}`)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem)+`}`)
	if got := mustResolve(t, f.Resolver(f.proj)); got.Dir != f.otherMem {
		t.Errorf("Dir = %q, want %q from settings.json", got.Dir, f.otherMem)
	}
}

func TestSettingsIgnoredWhenNoMemoryKey(t *testing.T) {
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.json"), `{"autoMemoryEnabled":true}`)
	if got := mustResolve(t, f.Resolver(f.proj)); got.Dir != f.projMem {
		t.Errorf("Dir = %q, want the default %q", got.Dir, f.projMem)
	}
}

func TestInvalidJSONSettingsSkippedSilently(t *testing.T) {
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.local.json"), "not json{")
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.json"), `{"memoryDir":`+quote(f.otherMem)+`}`)
	got, err := f.Resolver(f.proj).Resolve()
	if err != nil {
		t.Fatalf("invalid JSON was fatal: %v", err)
	}
	if got.Dir != f.otherMem {
		t.Errorf("Dir = %q, want %q — bad JSON should be skipped, not fatal", got.Dir, f.otherMem)
	}
}

func TestSettingsRelativePathResolvedAgainstSettingsFile(t *testing.T) {
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.json"), `{"memoryDir":"../mem"}`)
	if err := os.MkdirAll(filepath.Join(f.proj, "mem"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The settings file lives in <proj>/.claude, so "../mem" is <proj>/mem.
	want := filepath.Join(f.proj, "mem")
	if got := mustResolve(t, f.Resolver(f.proj)); got.Dir != want {
		t.Errorf("Dir = %q, want %q", got.Dir, want)
	}
}

func TestTildeExpansion(t *testing.T) {
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.json"),
		`{"memoryDir":"~/w/other"}`)
	want := filepath.Join(f.tmp, "w", "other")
	if got := mustResolve(t, f.Resolver(f.proj)); got.Dir != want {
		t.Errorf("Dir = %q, want %q", got.Dir, want)
	}
}

func TestTraceRecordsEveryLayer(t *testing.T) {
	f := newFixture(t)
	got := mustResolve(t, f.Resolver(f.proj))
	joined := strings.Join(got.Trace, "\n")
	for _, want := range []string{
		"resolving memory directory", "--dir flag", "$CLAUDE_MEM_DIR",
		".claude/settings.*", "settings.json", "default path", "✓ exists",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("trace missing %q; got:\n%s", want, joined)
		}
	}
}

func writeSettings(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func quote(s string) string { return `"` + s + `"` }
