package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/moj4b/claude-mem/internal/resolve"
)

// topLevelCandidates are offered when no subcommand has been typed yet
// (§II.10). __complete itself is hidden.
var topLevelCandidates = []string{
	"list", "read", "show", "search", "path", "projects", "completion",
	"--help", "--version", "--dir", "--all", "--project",
}

// completionScript is the thin shim of §II.10. All the logic lives in the
// binary behind __complete, so dynamic memory-name completion stays in sync
// automatically.
const completionScript = `_mem_complete() {
  local candidates
  mapfile -t candidates < <(mem __complete "${COMP_WORDS[@]:1}" 2>/dev/null)
  COMPREPLY=("${candidates[@]}")
}
complete -F _mem_complete mem
`

// cmdCompletionFn prints the shell completion script.
func cmdCompletionFn(o options, stdout, stderr io.Writer) int {
	shell := "bash"
	if len(o.args) > 0 {
		shell = o.args[0]
	}
	if shell != "bash" {
		fmt.Fprintf(stderr, "unsupported shell '%s'\n  usage: mem completion bash\n", shell)
		return exitUsage
	}
	fmt.Fprint(stdout, completionScript)
	return exitOK
}

// cmdCompleteFn is the hidden candidate generator run on every TAB. It must
// never write to stderr, never exit non-zero, and never block: on any error it
// prints nothing and exits 0, because a noisy __complete corrupts the user's
// prompt line (§II.10).
func cmdCompleteFn(o options, stdout, stderr io.Writer) int {
	defer func() { recover() }()
	for _, c := range completeCandidates(o.args) {
		fmt.Fprintln(stdout, c)
	}
	return exitOK
}

// completeCandidates maps the words after `mem` to candidates. bash appends an
// empty string to COMP_WORDS when the cursor sits after a space, which gives
// the correct arity for free — do not compensate for it (§II.10).
func completeCandidates(words []string) []string {
	if len(words) <= 1 {
		word := ""
		if len(words) == 1 {
			word = words[0]
		}
		return filterPrefix(topLevelCandidates, word)
	}

	word := words[len(words)-1]
	prior := words[:len(words)-1]

	// A flag's value is completed from the flag, not from the subcommand.
	switch prior[len(prior)-1] {
	case "--project":
		return filterPrefix(discoverProjectNames(), word)
	case "--dir":
		return nil // paths are the shell's job
	}

	cmd, project, operands := scanPrior(prior)
	switch cmd {
	case "read", "show":
		if operands > 0 {
			return nil // read takes exactly one name
		}
		return filterPrefix(memoryNames(project), word)
	case "path":
		return filterPrefix([]string{"--all", "--explain"}, word)
	case "list", "search":
		if strings.HasPrefix(word, "-") {
			return filterPrefix([]string{"--all", "--project"}, word)
		}
		return nil
	case "completion":
		return filterPrefix([]string{"bash"}, word)
	}
	return nil
}

// scanPrior finds the subcommand, any --project target, and how many operands
// have already been given, ignoring flags and their values.
func scanPrior(prior []string) (cmd, project string, operands int) {
	for i := 0; i < len(prior); i++ {
		w := prior[i]
		switch {
		case w == "--project" || w == "--dir":
			if i+1 < len(prior) {
				if w == "--project" {
					project = prior[i+1]
				}
				i++
			}
		case strings.HasPrefix(w, "--project="):
			project = strings.TrimPrefix(w, "--project=")
		case strings.HasPrefix(w, "-"):
			// a bare flag; nothing to consume
		case cmd == "":
			cmd = w
		default:
			operands++
		}
	}
	return cmd, project, operands
}

// completionResolver builds a resolve.Resolver from the ambient environment without
// ever reporting an error — completion is silent by contract.
func completionResolver(project string) (resolve.Resolver, bool) {
	r, err := resolve.New("", project)
	if err != nil {
		return resolve.Resolver{}, false
	}
	return r, true
}

// discoverProjectNames lists project names for --project completion. This
// costs a directory listing only — no file reads — so it stays inside the
// 20 ms budget (§II.10).
func discoverProjectNames() []string {
	r, ok := completionResolver("")
	if !ok {
		return nil
	}
	entries, err := os.ReadDir(r.ProjectsRoot())
	if err != nil {
		return nil
	}
	var slugs []string
	for _, e := range entries {
		if e.IsDir() && resolve.IsDir(filepath.Join(r.ProjectsRoot(), e.Name(), "memory")) {
			slugs = append(slugs, e.Name())
		}
	}
	names := resolve.ProjectNames(slugs)
	out := make([]string, 0, len(names))
	for _, slug := range slugs {
		out = append(out, names[slug])
	}
	sort.Strings(out)
	return out
}

// memoryNames lists completion candidates for the given project, or the local
// one when project is empty. Filenames only — it must not read file bodies.
func memoryNames(project string) []string {
	r, ok := completionResolver(project)
	if !ok {
		return nil
	}
	res, err := r.Resolve()
	if err != nil || !res.Exists {
		return nil
	}
	entries, err := os.ReadDir(res.Dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		out = append(out, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(out)
	return out
}

func filterPrefix(candidates []string, word string) []string {
	var out []string
	for _, c := range candidates {
		if strings.HasPrefix(c, word) {
			out = append(out, c)
		}
	}
	return out
}
