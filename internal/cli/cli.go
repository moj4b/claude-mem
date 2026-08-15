// Package cli is the command layer: argument parsing, dispatch, the exit-code
// contract of §II.9, and the formatters that turn stores into output.
package cli

import (
	"fmt"
	"io"
	"strings"
)

// Exit codes are the tool's API (§II.9).
const (
	exitOK       = 0 // success, including an existing-but-empty memory directory
	exitNotFound = 1 // no match, ambiguous name, or zero search results
	exitUsage    = 2 // unknown subcommand, missing argument, bad flag
	exitNoDir    = 3 // no memory directory
)

const usage = `mem — read Claude Code's per-project memory

usage:
  mem                       alias for ` + "`mem list`" + `
  mem list                  list memories, grouped by type
  mem read <name>           print a memory; partial names accepted
  mem show <name>           alias for ` + "`read`" + `
  mem search <query>        case-insensitive search across memory contents
  mem path [--explain]      print the resolved memory directory
  mem projects              list every project that has memory
  mem completion bash       print the bash completion script
  mem upgrade [--check]     replace this binary with the latest release
  mem update                alias for ` + "`upgrade`" + `
  mem --help | -h
  mem --version | -v

flags:
  --dir <path>              use this directory, skipping resolution
  --all                     widen to every project that has memory
  --project <name>          act on a different project's memory
  --check                   with ` + "`upgrade`" + `: report, install nothing
`

// Run is the whole command line: argv in, exit code out. Everything the tool
// does is reachable from here, which is what makes the process boundary
// testable without spawning a process.
func Run(args []string, stdout, stderr io.Writer) int {
	cmd, code := run(args, stdout, stderr)
	// Last, so a newer release is a footnote to the output rather than
	// something the payload scrolls away (§II.12).
	notifyUpdate(cmd, stderr)
	return code
}

// run does the work and names the command it ran, which is all notifyUpdate
// needs to know whether it may speak.
func run(args []string, stdout, stderr io.Writer) (string, int) {
	for _, a := range args {
		switch a {
		case "--help", "-h":
			fmt.Fprint(stdout, usage)
			return "--help", 0
		case "--version", "-v":
			fmt.Fprintf(stdout, "mem %s\n", versionString())
			return "--version", 0
		}
	}

	opts, err := parseArgs(args)
	if err != nil {
		return opts.cmd, fail(stderr, exitUsage, err)
	}
	return opts.cmd, dispatch(opts, stdout, stderr)
}

// options is the parsed command line: one subcommand, its operands, and the
// scope flags, which may appear on either side of the subcommand (§II.1).
type options struct {
	cmd     string
	args    []string
	dir     string
	all     bool
	project string
	explain bool
	check   bool
}

// commands are every dispatchable subcommand. `mem` with no subcommand is
// `list` (§II.1); __complete (§II.10) and __update-check (§II.12) are hidden
// but dispatchable.
var commands = map[string]bool{
	"list": true, "read": true, "show": true, "search": true,
	"path": true, "projects": true, "completion": true, "__complete": true,
	"upgrade": true, "update": true, updateCheckCmd: true,
}

func parseArgs(args []string) (options, error) {
	o := options{}
	// Everything after `__complete` is a raw word list from the shell and must
	// never be interpreted as a flag for mem itself (§II.10).
	for i := 0; i < len(args); i++ {
		a := args[i]
		if o.cmd == "__complete" {
			o.args = append(o.args, a)
			continue
		}
		switch {
		case a == "--dir", a == "--project":
			if i+1 >= len(args) {
				return o, fmt.Errorf("%s requires a value\n  usage: mem %s <%s>", a, a,
					map[string]string{"--dir": "path", "--project": "name"}[a])
			}
			i++
			if a == "--dir" {
				o.dir = args[i]
			} else {
				o.project = args[i]
			}
		case strings.HasPrefix(a, "--dir="):
			o.dir = strings.TrimPrefix(a, "--dir=")
		case strings.HasPrefix(a, "--project="):
			o.project = strings.TrimPrefix(a, "--project=")
		case a == "--all":
			o.all = true
		case a == "--explain":
			o.explain = true
		case a == "--check":
			o.check = true
		case strings.HasPrefix(a, "-") && a != "-":
			return o, fmt.Errorf("unknown flag '%s'\n  run `mem --help` for usage", a)
		case o.cmd == "":
			o.cmd = a
		default:
			o.args = append(o.args, a)
		}
	}
	if o.cmd == "" {
		o.cmd = "list"
	}
	if !commands[o.cmd] {
		return o, fmt.Errorf("unknown command '%s'\n  run `mem --help` for usage", o.cmd)
	}
	set := 0
	for _, on := range []bool{o.dir != "", o.all, o.project != ""} {
		if on {
			set++
		}
	}
	if set > 1 {
		return o, fmt.Errorf("--dir, --all and --project cannot be combined")
	}
	// read resolves to exactly one memory, so widening it is a usage error
	// rather than a silent widen (§II.0, §II.5).
	if o.all && (o.cmd == "read" || o.cmd == "show") {
		return o, fmt.Errorf("%s cannot be combined with --all\n"+
			"  read resolves to one memory; use --project <name> to retarget", o.cmd)
	}
	return o, nil
}

// operand names the single required argument of a subcommand, as {noun,
// placeholder}, for the usage errors of §II.9. Absent = takes no operand.
var operand = map[string][2]string{
	"read":   {"memory name", "name"},
	"show":   {"memory name", "name"},
	"search": {"query", "query"},
}

// handlers routes each accepted subcommand to its implementation. Every key of
// `commands` must appear here and vice versa.
var handlers = map[string]func(options, io.Writer, io.Writer) int{
	"list":         cmdList,
	"read":         cmdRead,
	"show":         cmdRead,
	"search":       cmdSearch,
	"path":         cmdPath,
	"projects":     cmdProjects,
	"completion":   cmdCompletion,
	"__complete":   cmdComplete,
	"upgrade":      cmdUpgrade,
	"update":       cmdUpgrade,
	updateCheckCmd: cmdUpdateCheck,
}

func dispatch(o options, stdout, stderr io.Writer) int {
	if what, needs := operand[o.cmd]; needs && len(o.args) == 0 {
		return fail(stderr, exitUsage, fmt.Errorf("%s requires a %s\n  usage: mem %s <%s>",
			o.cmd, what[0], o.cmd, what[1]))
	}
	return handlers[o.cmd](o, stdout, stderr)
}

var (
	cmdList       = cmdListFn
	cmdRead       = cmdReadFn
	cmdSearch     = cmdSearchFn
	cmdPath       = cmdPathFn
	cmdProjects   = cmdProjectsFn
	cmdCompletion = cmdCompletionFn
	cmdComplete   = cmdCompleteFn

	cmdUpgrade     = cmdUpgradeFn
	cmdUpdateCheck = cmdUpdateCheckFn
)

// fail writes a diagnostic to stderr (§II.2: payload to stdout, diagnostics to
// stderr) and returns the exit code, so callers can `return fail(...)`.
func fail(stderr io.Writer, code int, err error) int {
	fmt.Fprintf(stderr, "%s\n", err)
	return code
}

// plural formats a count with the right noun.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
