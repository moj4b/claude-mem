package resolve

import (
	"os"
	"path/filepath"
	"testing"
)

// A settings layer redirects this project's memory somewhere else, which leaves
// the computed default directories behind: real, populated, and no longer this
// project's memory as far as Claude Code is concerned. They are not another
// project's memory either, so every view that enumerates projects has to be
// able to tell them apart. Resolution.Displaced is how they are told.
//
// Plural, because the default is keyed on where `claude` was launched rather
// than on the repo root (§II.0): one project can have several, and a setting
// displaces all of them at once.

// wantDisplaced asserts the whole set, in order, so a test cannot pass by
// naming one of several displaced directories.
func wantDisplaced(t *testing.T, got Resolution, want ...string) {
	t.Helper()
	if len(got.Displaced) != len(want) {
		t.Fatalf("Displaced = %q, want %q", got.Displaced, want)
	}
	for i := range want {
		if got.Displaced[i] != want[i] {
			t.Errorf("Displaced[%d] = %q, want %q", i, got.Displaced[i], want[i])
		}
	}
}

func TestProjectSettingsDisplaceTheDefaultDirectoryTheyOverride(t *testing.T) {
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.local.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem)+`}`)
	got := mustResolve(t, f.Resolver(f.proj))
	if got.Dir != f.otherMem {
		t.Fatalf("Dir = %q, want the configured %q", got.Dir, f.otherMem)
	}
	wantDisplaced(t, got, f.projMem)
}

func TestUserSettingsDisplaceTheDefaultDirectoryTheyOverride(t *testing.T) {
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.root, "settings.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem)+`}`)
	got := mustResolve(t, f.Resolver(f.proj))
	wantDisplaced(t, got, f.projMem)
}

func TestDisplacementIsReportedEvenWhenTheConfiguredDirectoryIsMissing(t *testing.T) {
	// The whole point of #2: the configured directory wins whether or not it is
	// there. The default it displaced is exactly as displaced either way, and
	// this is the case where mistaking it for another project is most tempting,
	// because it is the only directory of the two that has anything in it.
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.local.json"),
		`{"autoMemoryDirectory":"/nope/nope"}`)
	got := mustResolve(t, f.Resolver(f.proj))
	if got.Exists {
		t.Fatal("Exists = true, want false")
	}
	wantDisplaced(t, got, f.projMem)
}

func TestDisplacementIsFoundFromASubdirectoryToo(t *testing.T) {
	// The settings walk starts at cwd and climbs; so must the default walk that
	// works out which directory the settings displaced.
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.local.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem)+`}`)
	got := mustResolve(t, f.Resolver(filepath.Join(f.proj, "sub", "deep")))
	wantDisplaced(t, got, f.projMem)
}

// defaultMemFor plants the computed-default memory directory for an arbitrary
// project path, for the tests about which default a setting does and does not
// displace.
func (f fixture) defaultMemFor(t *testing.T, dir string) string {
	t.Helper()
	mem := filepath.Join(f.root, "projects", Slugify(dir), "memory")
	if err := os.MkdirAll(mem, 0o755); err != nil {
		t.Fatal(err)
	}
	return mem
}

func TestNothingIsDisplacedWhenNoDefaultDirectoryExists(t *testing.T) {
	// A project with settings but no default directory of its own: the setting
	// is the first thing that ever named a memory directory here, so it
	// displaced nothing.
	f := newFixture(t)
	app := filepath.Join(f.tmp, "w", "app")
	writeSettings(t, filepath.Join(app, ".claude", "settings.local.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem)+`}`)
	got := mustResolve(t, f.Resolver(app))
	if got.Dir != f.otherMem {
		t.Fatalf("Dir = %q, want %q", got.Dir, f.otherMem)
	}
	wantDisplaced(t, got)
}

func TestAnAncestorProjectsDefaultDirectoryIsNotDisplaced(t *testing.T) {
	// A setting displaces the default directory of the project it governs — the
	// one it sits in — not whatever layer 6's ancestor climb would have landed
	// on. `mono` here is a separate project by every other measure: `mem
	// projects` names it, `--project mono` reaches it. Treating it as displaced
	// would drop a real answer out of the cross-project hints (§II.6).
	f := newFixture(t)
	mono := filepath.Join(f.tmp, "w", "mono")
	monoMem := f.defaultMemFor(t, mono)
	app := filepath.Join(mono, "app")
	writeSettings(t, filepath.Join(app, ".claude", "settings.local.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem)+`}`)
	got := mustResolve(t, f.Resolver(app))
	if got.Dir != f.otherMem {
		t.Fatalf("Dir = %q, want %q", got.Dir, f.otherMem)
	}
	if len(got.Displaced) != 0 {
		t.Errorf("Displaced = %q, want empty — %q is another project's memory, "+
			"not this project's displaced default", got.Displaced, monoMem)
	}
}

func TestAUserLevelKeyClimbsPastADirectoryWithNoMemoryOfItsOwn(t *testing.T) {
	// §II.0 defines the current project as "the nearest ancestor directory of
	// cwd that has a memory directory" — a .claude says nothing about it, and
	// Claude Code creates an empty one in any subdirectory that gets local
	// settings, agents or commands. Layer 6 climbs straight past it, so from
	// here mono's memory IS this project's memory; a user-level key displaces
	// exactly that, and stopping short would have the same directory be this
	// project's memory to `mem path` and another project's to the hints.
	f := newFixture(t)
	mono := filepath.Join(f.tmp, "w", "mono")
	monoMem := f.defaultMemFor(t, mono)
	app := filepath.Join(mono, "app")
	if err := os.MkdirAll(filepath.Join(app, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Without the key, mono's memory is what this project resolves to.
	if got := mustResolve(t, f.Resolver(app)); got.Dir != monoMem {
		t.Fatalf("Dir = %q, want %q — the premise of this test", got.Dir, monoMem)
	}
	writeSettings(t, filepath.Join(f.root, "settings.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem)+`}`)
	wantDisplaced(t, mustResolve(t, f.Resolver(app)), monoMem)
}

func TestALeftoverHomeClaudeSettingsFileIsStillAUserLevelKey(t *testing.T) {
	// ~/.claude/settings.json is the user-level file by convention, whether or
	// not CLAUDE_CONFIG_DIR still points at it. The layer-4 walk reaches it like
	// any other ancestor's, and treating it as $HOME declaring a project
	// boundary makes displacedBy swallow every default from cwd up to $HOME —
	// including $HOME's own, a project `mem projects` names.
	f := newFixture(t)
	homeMem := f.defaultMemFor(t, f.tmp)
	writeSettings(t, filepath.Join(f.tmp, ".claude", "settings.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem)+`}`)
	// f.root is <tmp>/root, deliberately not <tmp>/.claude.
	got := mustResolve(t, f.Resolver(f.proj))
	if got.Dir != f.otherMem {
		t.Fatalf("Dir = %q, want %q", got.Dir, f.otherMem)
	}
	wantDisplaced(t, got, f.projMem)
	for _, d := range got.Displaced {
		if d == homeMem {
			t.Errorf("Displaced holds %q, which is $HOME's own memory", homeMem)
		}
	}
}

func TestADeeperLaunchDirectoryInsideTheBoundaryIsDisplacedToo(t *testing.T) {
	// "Everything at or below that directory is one project" has to mean below
	// as well as above. Walking back up from cwd only catches the deeper default
	// when you happen to be standing in it — and the project root is the most
	// common place to stand, which left a subdirectory's default looking like
	// another project from the one directory the key actually lives in.
	f := newFixture(t)
	app := filepath.Join(f.proj, "app")
	appMem := f.defaultMemFor(t, app)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.local.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem)+`}`)
	// Standing at the project root, where the upward walk sees only its own.
	wantDisplaced(t, mustResolve(t, f.Resolver(f.proj)), f.projMem, appMem)
	// ...and standing in the subdirectory, which used to be the only way.
	wantDisplaced(t, mustResolve(t, f.Resolver(app)), f.projMem, appMem)
}

func TestOnlyDirectoriesThatExistAreDisplaced(t *testing.T) {
	// §I.2: 7 of 21 project directories under the projects root have no memory
	// subdirectory at all. Nothing was displaced there, and every Displaced
	// entry has to be a directory a caller can actually read.
	f := newFixture(t)
	app := filepath.Join(f.proj, "app")
	bare := filepath.Join(f.root, "projects", Slugify(app)) // no memory/ inside
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.local.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem)+`}`)
	got := mustResolve(t, f.Resolver(f.proj))
	wantDisplaced(t, got, f.projMem)
	for _, d := range got.Displaced {
		if !IsDir(d) {
			t.Errorf("Displaced holds %q, which is not a directory", d)
		}
	}
}

func TestASiblingWithASharedSlugPrefixIsNotDisplaced(t *testing.T) {
	// The slug transform preserves path prefixes, which is what makes the sweep
	// possible — and what makes -w-mono swallow -w-mono2 without a separator.
	f := newFixture(t)
	sibling := f.proj + "2"
	siblingMem := f.defaultMemFor(t, sibling)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.local.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem)+`}`)
	got := mustResolve(t, f.Resolver(f.proj))
	wantDisplaced(t, got, f.projMem)
	for _, d := range got.Displaced {
		if d == siblingMem {
			t.Errorf("Displaced holds %q, a sibling that merely shares a slug prefix", siblingMem)
		}
	}
}

func TestAProjectLocalConfigDirectoryIsStillTheConfigDirectory(t *testing.T) {
	// CLAUDE_CONFIG_DIR can point into the tree. The settings file there is
	// where layer 5 reads, so a key in it is user-level and declares no project
	// boundary — even though the layer-4 walk reaches it as an ancestor's
	// .claude like any other.
	f := newFixture(t)
	repo := filepath.Join(f.tmp, "w", "repo")
	root := filepath.Join(repo, ".claude") // CLAUDE_CONFIG_DIR, inside the tree
	app := filepath.Join(repo, "app")
	repoMem := filepath.Join(root, "projects", Slugify(repo), "memory")
	appMem := filepath.Join(root, "projects", Slugify(app), "memory")
	for _, d := range []string{repoMem, appMem, app} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeSettings(t, filepath.Join(root, "settings.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem)+`}`)

	got := mustResolve(t, Resolver{cwd: app, home: f.tmp, root: root})
	if got.Dir != f.otherMem {
		t.Fatalf("Dir = %q, want %q", got.Dir, f.otherMem)
	}
	wantDisplaced(t, got, appMem)
	for _, d := range got.Displaced {
		if d == repoMem {
			t.Errorf("Displaced holds %q — the config directory declared a boundary "+
				"it has no business declaring", repoMem)
		}
	}
}

func TestNothingIsDisplacedWhenAUserLevelKeyNamesTheDefaultItself(t *testing.T) {
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.root, "settings.json"),
		`{"autoMemoryDirectory":`+quote(f.projMem)+`}`)
	got := mustResolve(t, f.Resolver(f.proj))
	if got.Dir != f.projMem {
		t.Fatalf("Dir = %q, want %q", got.Dir, f.projMem)
	}
	wantDisplaced(t, got)
}

func TestNewCleansTheConfigRootItReadsFromTheEnvironment(t *testing.T) {
	// CLAUDE_CONFIG_DIR is compared against filepath.Join'd paths, so a trailing
	// slash must not make it a different directory. Through New, because that is
	// the only place the cleaning happens and a hand-built Resolver would skip
	// it entirely.
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir+string(filepath.Separator))
	r, err := New("", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.root != dir {
		t.Errorf("root = %q, want the cleaned %q", r.root, dir)
	}
}

func TestAUserLevelKeyReachesTheProjectRootFromASubdirectory(t *testing.T) {
	// §II.0: "Subdirectories work. This is the normal case, not an edge case."
	// A repo with no .claude of its own is the common shape, and looking only for
	// .claude would collapse the bound to cwd and let the original bug back in
	// from any subdirectory.
	f := newFixture(t)
	if err := os.MkdirAll(filepath.Join(f.proj, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, filepath.Join(f.root, "settings.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem)+`}`)
	got := mustResolve(t, f.Resolver(filepath.Join(f.proj, "sub", "deep")))
	wantDisplaced(t, got, f.projMem)
}

// homeFixture is the shape $HOME takes on a real machine: the config directory
// really is ~/.claude, which every ancestor walk under $HOME crosses, and $HOME
// itself is a project with memory because someone once ran `claude` there. The
// shared fixture's root is <tmp>/root, which no walk crosses, so it cannot show
// any of this.
func homeFixture(t *testing.T, f fixture) (root, homeMem string) {
	t.Helper()
	root = filepath.Join(f.tmp, ".claude")
	homeMem = filepath.Join(root, "projects", Slugify(f.tmp), "memory")
	if err := os.MkdirAll(homeMem, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, filepath.Join(root, "settings.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem)+`}`)
	return root, homeMem
}

func TestTheConfigDirectoryIsNotAProjectMarker(t *testing.T) {
	// ~/.claude is on every path under $HOME. Counting it would make $HOME the
	// root of every project that has no marker of its own, and $HOME's own
	// memory — a project `mem projects` names and `--project` reaches — would be
	// swallowed as displaced from anywhere below it.
	f := newFixture(t)
	root, homeMem := homeFixture(t, f)
	app := filepath.Join(f.tmp, "w", "app")
	appMem := filepath.Join(root, "projects", Slugify(app), "memory")
	if err := os.MkdirAll(appMem, 0o755); err != nil {
		t.Fatal(err)
	}
	got := mustResolve(t, Resolver{cwd: app, home: f.tmp, root: root})
	if got.Dir != f.otherMem {
		t.Fatalf("Dir = %q, want %q", got.Dir, f.otherMem)
	}
	wantDisplaced(t, got, appMem)
	for _, d := range got.Displaced {
		if d == homeMem {
			t.Errorf("Displaced holds %q, which is $HOME's own memory", homeMem)
		}
	}
}

func TestATrailingSlashOnTheConfigRootChangesNothing(t *testing.T) {
	// CLAUDE_CONFIG_DIR arrives from the environment and nothing cleans it. A
	// raw string compare against a filepath.Join'd path misses on a trailing
	// slash, and the config directory quietly becomes a project marker again.
	f := newFixture(t)
	root, homeMem := homeFixture(t, f)
	app := filepath.Join(f.tmp, "w", "app")
	appMem := filepath.Join(root, "projects", Slugify(app), "memory")
	if err := os.MkdirAll(appMem, 0o755); err != nil {
		t.Fatal(err)
	}
	got := mustResolve(t, Resolver{cwd: app, home: f.tmp, root: root + string(filepath.Separator)})
	wantDisplaced(t, got, appMem)
	for _, d := range got.Displaced {
		if d == homeMem {
			t.Errorf("Displaced holds %q with a trailing slash on the config root", homeMem)
		}
	}
}

func TestAUserLevelKeyDisplacesTheProjectRootFromAMarkerlessSubdirectory(t *testing.T) {
	// §I.8: "courier and claude-mem both have memory and are not git repos. The
	// ancestor walk must not depend on git existing." A project with no .git and
	// no .claude of its own is the ordinary case, and §II.0 calls subdirectories
	// the normal case — so from one, the key must still reach the directory that
	// really is this project's.
	f := newFixture(t)
	root, _ := homeFixture(t, f)
	proj := filepath.Join(f.tmp, "w", "plain")
	projMem := filepath.Join(root, "projects", Slugify(proj), "memory")
	deep := filepath.Join(proj, "src", "deep")
	for _, d := range []string{projMem, deep} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := mustResolve(t, Resolver{cwd: deep, home: f.tmp, root: root})
	wantDisplaced(t, got, projMem)
}

func TestAUserLevelKeyStopsAtAGitRoot(t *testing.T) {
	// Via stopDir, which already ends every ancestor walk at the git root
	// (§II.3), so a repo with no memory of its own yet reaches nothing above
	// itself. §I.8 says the walk must not depend on git existing — not that git
	// carries no information when it is there.
	f := newFixture(t)
	root, _ := homeFixture(t, f)
	outer := filepath.Join(f.tmp, "w", "outer")
	outerMem := filepath.Join(root, "projects", Slugify(outer), "memory")
	app := filepath.Join(outer, "app") // its own repo, no default of its own
	for _, d := range []string{outerMem, filepath.Join(app, ".git")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := mustResolve(t, Resolver{cwd: app, home: f.tmp, root: root})
	if len(got.Displaced) != 0 {
		t.Errorf("Displaced = %q, want empty — %q belongs to outer", got.Displaced, outerMem)
	}
}

func TestAUserLevelKeyDisplacesOnlyTheNearestDefaultInAMonorepo(t *testing.T) {
	// §II.0: the default "is keyed on where `claude` was launched, not on the
	// repo root — 'repo' is a useful approximation, not the rule." A monorepo
	// launched at both its root and a package inside it has two projects under
	// the projects root, both named by `mem projects` and reachable by
	// --project. A user-level key does not make the outer one this one's
	// history: only the nearest default is what layer 6 would have answered.
	f := newFixture(t)
	root, _ := homeFixture(t, f)
	repo := filepath.Join(f.tmp, "w", "repo")
	app := filepath.Join(repo, "app")
	repoMem := filepath.Join(root, "projects", Slugify(repo), "memory")
	appMem := filepath.Join(root, "projects", Slugify(app), "memory")
	for _, d := range []string{filepath.Join(repo, ".git"), app, repoMem, appMem} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := mustResolve(t, Resolver{cwd: app, home: f.tmp, root: root})
	wantDisplaced(t, got, appMem)
	for _, d := range got.Displaced {
		if d == repoMem {
			t.Errorf("Displaced holds %q, the enclosing project's own memory", repoMem)
		}
	}
}

func TestEveryDefaultTheSettingsFileGovernsIsDisplaced(t *testing.T) {
	// The default is keyed on where `claude` was launched, not on the repo root
	// (§II.0), so a project that has been launched from two directories has two
	// default directories. One setting displaces both; naming only the first
	// leaves the other to be offered back as another project.
	f := newFixture(t)
	src := filepath.Join(f.proj, "src")
	srcMem := f.defaultMemFor(t, src)
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.local.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem)+`}`)
	// Slug order, which is the projects root's own order: shallowest first.
	got := mustResolve(t, f.Resolver(src))
	wantDisplaced(t, got, f.projMem, srcMem)
}

func TestASymlinkedConfiguredDirectoryDisplacesNothing(t *testing.T) {
	// A directory cannot displace itself, however it was spelled.
	f := newFixture(t)
	link := filepath.Join(f.tmp, "link-to-proj-mem")
	if err := os.Symlink(f.projMem, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.local.json"),
		`{"autoMemoryDirectory":`+quote(link)+`}`)
	got := mustResolve(t, f.Resolver(f.proj))
	wantDisplaced(t, got)
}

func TestAUserLevelKeyReachesADeclaredProjectRootFromASubdirectory(t *testing.T) {
	// ...and the bound is the project root, not cwd, so standing deeper still
	// displaces the directory that is genuinely this project's.
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.json"), `{}`)
	writeSettings(t, filepath.Join(f.root, "settings.json"),
		`{"autoMemoryDirectory":`+quote(f.otherMem)+`}`)
	got := mustResolve(t, f.Resolver(filepath.Join(f.proj, "sub", "deep")))
	wantDisplaced(t, got, f.projMem)
}

func TestNothingIsDisplacedWhenSettingsNameTheDefaultItself(t *testing.T) {
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.local.json"),
		`{"autoMemoryDirectory":`+quote(f.projMem)+`}`)
	got := mustResolve(t, f.Resolver(f.proj))
	if got.Dir != f.projMem {
		t.Fatalf("Dir = %q, want %q", got.Dir, f.projMem)
	}
	wantDisplaced(t, got)
}

func TestNothingIsDisplacedWhenTheDefaultPathItselfWins(t *testing.T) {
	f := newFixture(t)
	got := mustResolve(t, f.Resolver(f.proj))
	if got.Dir != f.projMem {
		t.Fatalf("Dir = %q, want %q", got.Dir, f.projMem)
	}
	wantDisplaced(t, got)
}

func TestOneOffOverridesDisplaceNothing(t *testing.T) {
	// --dir, --project and $CLAUDE_MEM_DIR retarget one invocation; they do not
	// restate where this project keeps its memory, so the default directory is
	// still this project's and must keep showing up as such. Only a settings key
	// — the thing Claude Code itself reads — displaces it.
	f := newFixture(t)
	for _, tc := range []struct {
		name string
		r    Resolver
	}{
		{"--dir", func() Resolver { r := f.Resolver(f.proj); r.dirFlag = f.otherMem; return r }()},
		{"--project", func() Resolver { r := f.Resolver(f.proj); r.project = "other"; return r }()},
		{"$CLAUDE_MEM_DIR", func() Resolver { r := f.Resolver(f.proj); r.envDir = f.otherMem; return r }()},
	} {
		got := mustResolve(t, tc.r)
		if len(got.Displaced) != 0 {
			t.Errorf("%s: Displaced = %q, want empty", tc.name, got.Displaced)
		}
	}
}

func TestMalformedSettingsDisplaceNothing(t *testing.T) {
	// A value Claude Code would reject is treated as unset, so the default is not
	// displaced at all — it is the answer.
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.local.json"),
		`{"autoMemoryDirectory":"relative/nope"}`)
	got := mustResolve(t, f.Resolver(f.proj))
	if got.Dir != f.projMem {
		t.Fatalf("Dir = %q, want the default %q", got.Dir, f.projMem)
	}
	wantDisplaced(t, got)
}

func TestLegacyKeysDisplaceLikeTheRealOne(t *testing.T) {
	// The legacy keys are honoured as memory directories, so they displace the
	// default exactly as autoMemoryDirectory does.
	f := newFixture(t)
	writeSettings(t, filepath.Join(f.proj, ".claude", "settings.json"),
		`{"memoryDir":`+quote(f.otherMem)+`}`)
	got := mustResolve(t, f.Resolver(f.proj))
	wantDisplaced(t, got, f.projMem)
}
