package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/moj4b/claude-mem/internal/resolve"
)

// resolveDir runs the §II.3 layer walk and turns a missing directory into the
// exit-3 diagnostics of §II.9. The three situations the tool must never blur —
// no directory (3), empty directory (0), name not found (1) — are separated
// here: this function only ever reports the first.
func resolveDir(o options, stderr io.Writer) (resolve.Resolution, int) {
	r, err := resolve.New(o.dir, o.project)
	if err != nil {
		fmt.Fprintf(stderr, "%s\n", err)
		return resolve.Resolution{}, exitNoDir
	}
	res, err := r.Resolve()
	if err != nil {
		// An unknown or ambiguous --project is exit 1, not a usage error (§II.0).
		fmt.Fprintf(stderr, "%s\n", err)
		return res, exitNotFound
	}
	if o.explain {
		fmt.Fprintln(stderr, strings.Join(res.Trace, "\n"))
	}
	if res.Exists {
		return res, exitOK
	}
	switch res.Via {
	case "--dir flag", "$CLAUDE_MEM_DIR":
		from := "--dir"
		if res.Via == "$CLAUDE_MEM_DIR" {
			from = "$CLAUDE_MEM_DIR"
		}
		fmt.Fprintf(stderr, "memory directory does not exist: %s  (from %s)\n", res.Dir, from)
	case "--project flag":
		fmt.Fprintf(stderr, "no memory directory for that project\n  looked for: %s\n", res.Dir)
	case "project settings", "user settings":
		// Settings name a directory that is not there. That is a setting to fix,
		// not memory waiting to be written — and Claude Code will read the same
		// missing directory, so say whose path it is.
		fmt.Fprintf(stderr, "no memory directory for this project\n"+
			"  looked for: %s\n"+
			"  named by %s, which is where Claude Code reads too\n"+
			"  create it, or fix the setting — other memory is still reachable with --project/--dir\n",
			res.Dir, res.Source)
	default:
		fmt.Fprintf(stderr, "no memory directory for this project\n"+
			"  looked for: %s\n"+
			"  Claude Code has not saved memory for this project yet\n"+
			"  run `mem path --explain` to see how this path was resolved\n", res.Dir)
	}
	return res, exitNoDir
}

// cmdPathFn prints the resolved memory directory (§II.7). Nothing reaches
// stdout unless the directory exists, so `cd $(mem path)` cannot go wrong.
func cmdPathFn(o options, stdout, stderr io.Writer) int {
	if o.all {
		// Every existing memory directory, one absolute path per line, sorted
		// by project name — no header, no decoration, TTY or not, so it pipes
		// cleanly into xargs/rg (§II.7).
		sc, code := resolveAll(o, stderr)
		if code != exitOK {
			return exitNoDir
		}
		for _, s := range sc.Stores {
			fmt.Fprintln(stdout, s.Dir)
		}
		return exitOK
	}
	res, code := resolveDir(o, stderr)
	if code != exitOK {
		return code
	}
	fmt.Fprintln(stdout, res.Dir)
	return exitOK
}
