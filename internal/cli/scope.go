package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/moj4b/claude-mem/internal/memory"
	"github.com/moj4b/claude-mem/internal/resolve"
)

// ScopeKind is which projects a command acts on (§II.0).
type ScopeKind int

const (
	ScopeLocal   ScopeKind = iota // the current project, resolved from cwd
	ScopeProject                  // --project <name>
	ScopeAll                      // --all
	ScopeDir                      // --dir <path>
)

// Scope is what every command receives. Commands never branch on scope
// internally — only their formatters know whether to print project headings.
// That is what keeps §II.0 honest instead of leaving each command to
// reimplement it.
type Scope struct {
	Kind   ScopeKind
	Stores []memory.Store // 1 for local/project/dir, N for all
	Label  string         // header line: a path, or "all projects (N directories, M memories)"
	Dirs   int            // memory directories in scope, including empty ones
	Total  int            // memories in scope
	// Displaced is resolve.Resolution.Displaced: the directories a settings key
	// replaced. They are this project's too — owns says so, and the misses name
	// them by path rather than dropping them (§II.0, §II.6).
	Displaced []string
}

// owns reports whether dir is this scope's own memory. More directories can be
// than the one the walk resolved: everything a settings key displaced is still
// on disk under the projects root, holding this project's own history. Offering
// those back as another project is the bug this exists to prevent (§II.6).
func (s Scope) owns(dir string) bool {
	for _, st := range s.Stores {
		if resolve.SameDir(st.Dir, dir) {
			return true
		}
	}
	for _, d := range s.Displaced {
		if resolve.SameDir(d, dir) {
			return true
		}
	}
	return false
}

// Widened reports whether output must name the project on every row.
func (s Scope) Widened() bool { return s.Kind == ScopeAll }

// resolveScope turns the parsed flags into the one or more stores a command
// acts on. --all does not use the §II.3 layer walk at all: it enumerates the
// projects root directly (§II.7a).
func resolveScope(o options, stderr io.Writer) (Scope, int) {
	if o.all {
		return resolveAll(o, stderr)
	}
	res, code := resolveDir(o, stderr)
	if code != exitOK {
		return Scope{}, code
	}
	s, err := memory.Load(res.Dir)
	if err != nil {
		fmt.Fprintf(stderr, "cannot read memory directory: %s\n", res.Dir)
		return Scope{}, exitNoDir
	}
	s.Project = resolve.ProjectName(filepath.Base(filepath.Dir(res.Dir)))
	kind := ScopeLocal
	switch res.Via {
	case "--dir flag":
		kind = ScopeDir
	case "--project flag":
		kind = ScopeProject
	}
	return Scope{
		Kind:      kind,
		Stores:    []memory.Store{s},
		Label:     "memory: " + s.Dir,
		Dirs:      1,
		Total:     len(s.Memories),
		Displaced: res.Displaced,
	}, exitOK
}

// resolveAll enumerates every project that has a memory directory.
func resolveAll(o options, stderr io.Writer) (Scope, int) {
	r, err := resolve.New(o.dir, o.project)
	if err != nil {
		fmt.Fprintf(stderr, "%s\n", err)
		return Scope{}, exitNoDir
	}
	stores := discoverProjects(r.ProjectsRoot())
	if len(stores) == 0 {
		fmt.Fprintf(stderr, "no projects have memory yet\n")
		return Scope{}, exitNoDir
	}
	total := 0
	for _, s := range stores {
		total += len(s.Memories)
	}
	return Scope{
		Kind:   ScopeAll,
		Stores: stores,
		Label: fmt.Sprintf("memory: all projects (%d directories, %d memories)",
			len(stores), total),
		Dirs:  len(stores),
		Total: total,
	}, exitOK
}

// discoverProjects loads every <projects>/<slug>/memory that exists, sorted by
// project name. A project directory without a memory subdirectory is skipped —
// 7 of 21 are like that (§I.2) — but an existing-but-empty one is kept, because
// knowing a project holds nothing is useful information (§II.7a).
func discoverProjects(projectsRoot string) []memory.Store {
	entries, err := os.ReadDir(projectsRoot)
	if err != nil {
		return nil
	}
	// Compute names from every project directory, not only those with memory,
	// so the shared-prefix stripping sees the full sibling set.
	var allSlugs []string
	for _, e := range entries {
		if e.IsDir() {
			allSlugs = append(allSlugs, e.Name())
		}
	}
	names := resolve.ProjectNames(allSlugs)

	var stores []memory.Store
	for _, slug := range allSlugs {
		dir := filepath.Join(projectsRoot, slug, "memory")
		if !resolve.IsDir(dir) {
			continue
		}
		s, err := memory.Load(dir)
		if err != nil {
			continue
		}
		s.Project = names[slug]
		stores = append(stores, s)
	}
	sort.Slice(stores, func(i, j int) bool { return stores[i].Project < stores[j].Project })
	return stores
}

// byCountThenName orders projects for display: most memories first, ties by
// name (§II.4, §II.7a).
func byCountThenName(stores []memory.Store) []memory.Store {
	out := append([]memory.Store(nil), stores...)
	sort.SliceStable(out, func(i, j int) bool {
		if len(out[i].Memories) != len(out[j].Memories) {
			return len(out[i].Memories) > len(out[j].Memories)
		}
		return out[i].Project < out[j].Project
	})
	return out
}

// displaced loads the memory a settings key left behind — this project's own
// history, at the directories Claude Code stopped reading when the key landed.
// Not another project, and not nothing either: the misses that would otherwise
// dead-end have to keep naming them, or the fix trades one wrong answer for no
// answer (§II.0).
func (s Scope) displaced() []memory.Store {
	var out []memory.Store
	for _, dir := range s.Displaced {
		st, err := memory.Load(dir)
		if err != nil {
			continue
		}
		out = append(out, st)
	}
	return out
}

// displacedNote is the one phrase every command uses for a directory a settings
// key replaced, so they cannot drift apart in wording. It lives beside
// displaced() rather than in cli.go, because it names that concept and nothing
// else.
//
// "the memory setting", not "this project's settings": the key may equally be
// the user-level one, and pointing at the wrong file to fix is the opposite of
// what these offers are for. `mem path --explain` names the deciding file.
const displacedNote = "in the directory the memory setting replaced"

// displacedOffer is the one shape every command uses to offer a displaced
// directory: how much of what you asked for is in it, and the command that
// reaches it. `arg` is the query for search, the memory name for read, and
// empty for list. One shape so the three cannot drift apart in wording.
func displacedOffer(n int, cmd, dir, arg string) string {
	if arg != "" {
		arg = " " + arg
	}
	return fmt.Sprintf("  %s %s\n    mem %s --dir %s%s\n",
		plural(n, "memory", "memories"), displacedNote, cmd, dir, arg)
}

// writeDisplacedOffers names the displaced directories that still hold
// something. `list` needs it for the state a project is in the moment the key
// is added — nothing written to the new directory yet, everything still in the
// old one — where dead-ending would be exactly the inconsistency §II.0 exists
// to prevent.
func writeDisplacedOffers(sc Scope, cmd string, stderr io.Writer) {
	for _, s := range sc.displaced() {
		if len(s.Memories) > 0 {
			fmt.Fprint(stderr, displacedOffer(len(s.Memories), cmd, s.Dir, ""))
		}
	}
}

// otherProjects lists every project the scope does not already own, for the
// cross-project hints on a read or search miss (§II.5, §II.6).
func (s Scope) otherProjects() []memory.Store {
	// The projects root is the same wherever the scope came from, so the two
	// retargeting flags have nothing to say here.
	r, err := resolve.New("", "")
	if err != nil {
		return nil
	}
	var out []memory.Store
	for _, st := range discoverProjects(r.ProjectsRoot()) {
		if !s.owns(st.Dir) {
			out = append(out, st)
		}
	}
	return out
}
