package resolve

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Resolution is the outcome of the §II.3 layer walk: which directory, whether
// it is there, which layer decided, and the trace --explain renders.
type Resolution struct {
	Dir    string
	Exists bool
	Via    string
	Source string // settings layers only: "<key> in <file>", for the exit-3 message
	// Displaced are the computed-default directories a settings layer displaced:
	// what Claude Code wrote before the key landed, still sitting under the
	// projects root with everything in it. No longer this project's memory — but
	// not another project's either, so the views that enumerate projects have to
	// be able to tell them apart (§II.6). Plural because the default is keyed on
	// where `claude` was launched rather than on the repo root (§II.0), so one
	// project can have several and a setting displaces all of them.
	Displaced []string
	Trace     []string
}

// Resolver holds every input to resolution explicitly rather than reading the
// process environment, so the layer precedence is testable in isolation.
type Resolver struct {
	cwd     string // absolute
	home    string // $HOME
	root    string // ${CLAUDE_CONFIG_DIR:-~/.claude}
	envDir  string // $CLAUDE_MEM_DIR
	dirFlag string // --dir
	project string // --project
}

// New reads the ambient environment into an explicit Resolver. The two CLI
// overrides are passed in rather than read from a flags struct, so this package
// stays independent of the command layer.
func New(dirFlag, project string) (Resolver, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Resolver{}, fmt.Errorf("cannot determine current directory: %w", err)
	}
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	home, _ := os.UserHomeDir()
	// Cleaned, because it arrives from the environment and is compared against
	// filepath.Join'd paths: a trailing slash must not make it a different
	// directory.
	root := os.Getenv("CLAUDE_CONFIG_DIR")
	if root == "" {
		root = filepath.Join(home, ".claude")
	}
	root = filepath.Clean(root)
	return Resolver{
		cwd:     cwd,
		home:    home,
		root:    root,
		envDir:  os.Getenv("CLAUDE_MEM_DIR"),
		dirFlag: dirFlag,
		project: project,
	}, nil
}

// projectsRoot is where Claude Code keeps one directory per project (§I.1).
func (r Resolver) ProjectsRoot() string { return filepath.Join(r.root, "projects") }

// resolve walks the six layers of §II.3, first hit wins. Layers 1-3 are taken
// verbatim without an existence check — an explicit override pointing nowhere
// is an error to report, not a reason to fall through. Layers 4-6 are
// candidates: the first that exists wins.
func (r Resolver) Resolve() (Resolution, error) {
	res := Resolution{Trace: []string{"resolving memory directory"}}

	// Layer 1 — --dir.
	if r.dirFlag != "" {
		return r.stop(res, "--dir flag", r.expand(r.dirFlag, r.cwd)), nil
	}
	res.note("--dir flag", "not set")

	// Layer 2 — --project. Stops here deliberately: layers 3-5 belong to the
	// directory you are standing in, not the project you asked for (§II.3).
	if r.project != "" {
		dir, err := r.projectDir(r.project)
		if err != nil {
			return res, err
		}
		return r.stop(res, "--project flag", dir), nil
	}
	res.note("--project flag", "not set")

	// Layer 3 — $CLAUDE_MEM_DIR.
	if r.envDir != "" {
		return r.stop(res, "$CLAUDE_MEM_DIR", r.expand(r.envDir, r.cwd)), nil
	}
	res.note("$CLAUDE_MEM_DIR", "not set")

	stop := r.stopDir()

	// Layers 4 and 5 are as binding as 1-3, because Claude Code treats them that
	// way: its resolver validates the value's FORM and returns it without ever
	// asking the filesystem whether the directory is there, computing the
	// default only when no layer supplies a well-formed one. A configured
	// directory that does not exist is therefore still the answer — Claude Code
	// creates it on the next session, and reads nothing if it cannot. Falling
	// through to the default here would report memories Claude Code does not
	// read (§II.3).

	// Layer 4 — project settings, cwd upward to the stop directory.
	checked := ""
	for _, dir := range ancestors(r.cwd, stop) {
		for _, name := range []string{"settings.local.json", "settings.json"} {
			path := filepath.Join(dir, ".claude", name)
			if p, key, ok := r.memoryDirFrom(path); ok {
				res.note(".claude/settings.*", key+" in "+path)
				res.Source = key + " in " + path
				res.Displaced = r.displacedBy(dir, p)
				return r.stop(res, "project settings", p), nil
			}
		}
		if checked == "" {
			if _, err := os.Stat(filepath.Join(dir, ".claude")); err == nil {
				checked = dir
			}
		}
	}
	if checked == "" {
		checked = r.cwd
	}
	res.note(".claude/settings.*", "no memory key (checked "+checked+")")

	// Layer 5 — user settings.
	userSettings := filepath.Join(r.root, "settings.json")
	if p, key, ok := r.memoryDirFrom(userSettings); ok {
		res.note("~/.claude/settings.json", key)
		res.Source = key + " in " + userSettings
		res.Displaced = r.displacedNearest(p)
		return r.stop(res, "user settings", p), nil
	}
	res.note("~/.claude/settings.json", "no memory key")

	// Layer 6 — computed default: the ancestor walk of §II.3.
	res.note("default path", "cwd "+r.cwd)
	if cand, ok := r.defaultDir(stop); ok {
		res.indent(cand + "  ✓ exists")
		res.Dir, res.Exists, res.Via = cand, true, "default path"
		res.arrow()
		return res, nil
	}
	// Nothing anywhere up the chain: report the default applied to cwd, so the
	// exit-3 message can name exactly what was looked for.
	guess := filepath.Join(r.ProjectsRoot(), Slugify(r.cwd), "memory")
	res.indent(guess + "  ✗ missing")
	res.Dir, res.Exists, res.Via = guess, false, "default path"
	res.arrow()
	return res, nil
}

// stop finalises a resolution decided by an explicit layer.
func (r Resolver) stop(res Resolution, via, dir string) Resolution {
	res.note(via, dir)
	res.Dir, res.Via = dir, via
	res.Exists = IsDir(dir)
	res.arrow()
	return res
}

// defaultDir is layer 6's answer: the first computed-default directory that
// exists, from cwd upward through stop.
func (r Resolver) defaultDir(stop string) (string, bool) {
	for _, dir := range ancestors(r.cwd, stop) {
		if cand, ok := r.defaultFor(dir); ok {
			return cand, true
		}
	}
	return "", false
}

// Only the settings layers displace anything — --dir, --project and
// $CLAUDE_MEM_DIR retarget one invocation without restating where this project
// keeps its memory, so the default directory is still its own. What they
// displace depends on whether the key came with a project boundary attached.

// displacedBy lists what a settings file in dir/.claude displaced: every
// existing default from cwd up to dir. That file declares the boundary —
// everything at or below it is one project — and one project can have several
// defaults, because Claude Code keys them on where `claude` was launched rather
// than on the repo root (§II.0).
//
// The config directory declares nothing of the sort. It sits on every path
// under $HOME, so a key found there is the user-level key it looks like — both
// ${CLAUDE_CONFIG_DIR} and ~/.claude, because ~/.claude/settings.json is the
// user-level file by convention whether or not the variable still points at it,
// and the ancestor walk reaches it like anyone else's.
func (r Resolver) displacedBy(dir, chosen string) []string {
	claude := filepath.Join(dir, ".claude")
	if SameDir(claude, r.root) || (r.home != "" && SameDir(claude, filepath.Join(r.home, ".claude"))) {
		return r.displacedNearest(chosen)
	}
	// Read the projects root rather than walking back up from cwd. Below counts
	// as much as above — a default belonging to a deeper launch directory is
	// inside the same boundary — and walking up would only ever catch that one
	// while standing in it, leaving it looking like another project from the
	// project root, which is where the key lives.
	entries, err := os.ReadDir(r.ProjectsRoot())
	if err != nil {
		return nil
	}
	want := norm(Slugify(dir))
	var out []string
	for _, e := range entries {
		if !e.IsDir() || !slugUnder(norm(e.Name()), want) {
			continue
		}
		if mem := filepath.Join(r.ProjectsRoot(), e.Name(), "memory"); IsDir(mem) &&
			!SameDir(mem, chosen) {
			out = append(out, mem)
		}
	}
	return out
}

// slugUnder reports whether slug names dir itself or something inside it. The
// slug transform turns every separator into '-' and so preserves path prefixes,
// which is what makes this a prefix test — with the separator required, or
// -w-mono would swallow the sibling -w-mono2.
func slugUnder(slug, dir string) bool {
	return slug == dir || strings.HasPrefix(slug, dir+"-")
}

// displacedNearest is what a key with no boundary of its own displaced: exactly
// the directory layer 6 would have answered with, and nothing above it.
//
// Nothing above it, because that is where a separate project gets swallowed: a
// monorepo launched at both its root and a package inside it has two projects
// under the projects root, and standing in the package, only the package's own
// default was ever this invocation's memory.
//
// And exactly that directory, with no other test applied. §II.0 defines the
// current project as the nearest ancestor with a memory directory; a .claude in
// between says nothing about it, and Claude Code makes an empty one in any
// subdirectory that gets local settings, agents or commands. Layer 6 climbs
// straight past those, so treating one as a boundary would leave the same
// directory this project's memory to `mem path` and another project's to the
// hints.
func (r Resolver) displacedNearest(chosen string) []string {
	dir, ok := r.defaultDir(r.stopDir())
	if !ok || SameDir(dir, chosen) {
		return nil
	}
	return []string{dir}
}

// SameDir reports whether two paths are the same directory, following symlinks,
// so a configured value that reaches the default the long way round is not
// mistaken for having displaced it. Exported because cli's Scope.owns asks the
// same question of the same directories and must not answer it differently.
func SameDir(a, b string) bool {
	if a == b {
		return true
	}
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	return err == nil && os.SameFile(ai, bi)
}

// defaultFor tries the computed slug for dir, then the fallback scan of
// §II.3 step 3b, which removes any need to guess the punctuation transform.
func (r Resolver) defaultFor(dir string) (string, bool) {
	cand := filepath.Join(r.ProjectsRoot(), Slugify(dir), "memory")
	if IsDir(cand) {
		return cand, true
	}
	entries, err := os.ReadDir(r.ProjectsRoot())
	if err != nil {
		return "", false
	}
	want := norm(Slugify(dir))
	for _, e := range entries {
		if !e.IsDir() || norm(e.Name()) != want {
			continue
		}
		if m := filepath.Join(r.ProjectsRoot(), e.Name(), "memory"); IsDir(m) {
			return m, true
		}
	}
	return "", false
}

// projectDir maps a human project name to its memory directory (§II.0):
// trailing slug segment first, then substring. Ambiguity is reported, never
// guessed.
func (r Resolver) projectDir(name string) (string, error) {
	entries, err := os.ReadDir(r.ProjectsRoot())
	if err != nil {
		return "", fmt.Errorf("no projects directory: %s", r.ProjectsRoot())
	}
	var slugs []string
	for _, e := range entries {
		if e.IsDir() {
			slugs = append(slugs, e.Name())
		}
	}
	sort.Strings(slugs)
	names := ProjectNames(slugs)
	want := norm(name)
	var exact, partial []string
	for _, s := range slugs {
		if norm(names[s]) == want {
			exact = append(exact, s)
		} else if strings.Contains(norm(s), want) {
			partial = append(partial, s)
		}
	}
	hits := exact
	if len(hits) == 0 {
		hits = partial
	}
	switch len(hits) {
	case 1:
		return filepath.Join(r.ProjectsRoot(), hits[0], "memory"), nil
	case 0:
		return "", fmt.Errorf("no project named '%s'\n  run `mem projects` to list them", name)
	default:
		candidates := make([]string, len(hits))
		for i, h := range hits {
			candidates[i] = names[h]
		}
		return "", fmt.Errorf("'%s' is ambiguous — %d projects match\n  %s",
			name, len(hits), strings.Join(candidates, ", "))
	}
}

// projectNames maps each slug to its human name (§II.0). The slug is lossy —
// '/' and '-' both became '-' — so the name cannot be "everything after the
// last dash": that would turn claude-mem into "mem". Instead strip the leading
// segments every sibling shares, which is the containing path, leaving the
// directory name whole however many dashes it holds.
func ProjectNames(slugs []string) map[string]string {
	out := make(map[string]string, len(slugs))
	if len(slugs) == 0 {
		return out
	}
	split := make([][]string, len(slugs))
	shortest := -1
	for i, s := range slugs {
		split[i] = strings.Split(s, "-")
		if shortest < 0 || len(split[i]) < shortest {
			shortest = len(split[i])
		}
	}
	// Always leave at least one segment, so a lone slug still yields a name.
	common := 0
	for common < shortest-1 {
		seg := split[0][common]
		same := true
		for _, parts := range split[1:] {
			if parts[common] != seg {
				same = false
				break
			}
		}
		if !same {
			break
		}
		common++
	}
	for i, s := range slugs {
		out[s] = strings.Join(split[i][common:], "-")
	}
	return out
}

// projectName is the human name of a single slug, without sibling context: the
// trailing path-like segment after the conventional projects prefix. Prefer
// projectNames when the sibling slugs are available.
func ProjectName(slug string) string {
	if i := strings.LastIndex(slug, "-"); i >= 0 {
		return slug[i+1:]
	}
	return slug
}

// stopDir bounds the ancestor walk: the git root, else $HOME, else the
// filesystem root. Git only bounds the climb — it never gates it, because
// projects with memory are not always repos (§I.8).
func (r Resolver) stopDir() string {
	for dir := r.cwd; ; {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if r.home != "" && within(r.cwd, r.home) {
		return r.home
	}
	// E2E runs from /tmp, which is not under $HOME.
	return string(filepath.Separator)
}

// ancestors lists cwd upward through stop, inclusive, nearest first.
func ancestors(cwd, stop string) []string {
	var out []string
	for dir := cwd; ; {
		out = append(out, dir)
		if dir == stop {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return out
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// expand applies ~/ expansion and resolves a relative path against base.
func (r Resolver) expand(p, base string) string {
	if p == "~" {
		return r.home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(r.home, p[2:])
	}
	if !filepath.IsAbs(p) {
		return filepath.Join(base, p)
	}
	return filepath.Clean(p)
}

// autoMemoryKey is the settings key Claude Code actually reads. §I.7 could not
// verify one and guessed; the shipped bundle settles it — it describes this key
// as "Custom directory path for auto-memory storage. Supports ~/ prefix for
// home directory expansion. When unset, defaults to
// ~/.claude/projects/<sanitized-cwd>/memory/", and reads it from
// .claude/settings.local.json, then .claude/settings.json, then user settings —
// the precedence layers 4 and 5 already walk.
const autoMemoryKey = "autoMemoryDirectory"

// legacyKeys are §I.7's guesses. None of them appear in Claude Code, but they
// cost nothing to keep honouring, below the real key.
var legacyKeys = []string{"memoryDir", "memoryPath", "memoryDirectory", "claudeMemoryDir"}

// memoryDirFrom returns the memory directory a settings file names, absolute
// and cleaned, with the key it came from. A file that is missing, unreadable,
// or invalid JSON is skipped silently (§II.3), as is a value Claude Code would
// itself reject — malformed and unset are the same thing to its ?? chain, so
// the next source is consulted. Existence is never consulted: that is the one
// thing Claude Code does not check either.
func (r Resolver) memoryDirFrom(path string) (dir, key string, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return "", "", false
	}
	if s, ok := m[autoMemoryKey].(string); ok && s != "" {
		if dir, ok := r.autoMemoryDir(s); ok {
			return dir, autoMemoryKey, true
		}
	}
	for _, k := range legacyKeys {
		if s, ok := m[k].(string); ok && s != "" {
			return r.expand(s, filepath.Dir(path)), k, true
		}
	}
	if nested, ok := m["memory"].(map[string]any); ok {
		for _, k := range []string{"dir", "path"} {
			if s, ok := nested[k].(string); ok && s != "" {
				return r.expand(s, filepath.Dir(path)), "memory." + k, true
			}
		}
	}
	return "", "", false
}

// autoMemoryDir mirrors Claude Code's own normalisation of autoMemoryDirectory:
// a leading ~/ expands against $HOME, the result is cleaned, and anything that
// is not absolute afterwards — or is a UNC path, or holds a NUL — is dropped so
// the walk continues to the next layer. Dropping is the point: Claude Code does
// not resolve a relative value against anything, so neither may mem, or it
// would name a directory nothing ever wrote to.
func (r Resolver) autoMemoryDir(p string) (string, bool) {
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		rest := filepath.Clean(p[2:])
		if rest == "." || rest == ".." || strings.HasPrefix(rest, ".."+string(filepath.Separator)) {
			return "", false
		}
		p = filepath.Join(r.home, rest)
	}
	dir := filepath.Clean(p)
	if !filepath.IsAbs(dir) || len(dir) < 3 ||
		strings.HasPrefix(dir, `\\`) || strings.ContainsRune(dir, 0) {
		return "", false
	}
	return dir, true
}

func IsDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// Trace rendering — built always, rendered only by --explain (§II.3).

func (res *Resolution) note(label, detail string) {
	res.Trace = append(res.Trace, fmt.Sprintf("  %-24s %s", label, detail))
}

func (res *Resolution) indent(s string) {
	res.Trace = append(res.Trace, fmt.Sprintf("  %-24s %s", "", s))
}

func (res *Resolution) arrow() {
	res.Trace = append(res.Trace, "→ "+res.Dir)
}

// slugify reproduces Claude Code's directory-name transform: every character
// outside [A-Za-z0-9] becomes '-'. Verified for '/' and '_' (§I.1); the
// treatment of '.' is unverified, which is what norm and the fallback scan
// exist to cover.
func Slugify(path string) string {
	var b strings.Builder
	b.Grow(len(path))
	for _, r := range path {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// norm loosens a slug for the fallback scan: lowercase, and runs of '-'
// collapsed to one, so a guessed slug still matches the real directory even
// when the true transform differs on punctuation.
func norm(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	prevCourier := false
	for _, r := range s {
		if r == '-' {
			if !prevCourier {
				b.WriteRune(r)
			}
			prevCourier = true
			continue
		}
		prevCourier = false
		b.WriteRune(r)
	}
	return b.String()
}
