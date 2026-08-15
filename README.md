# `mem`

[![CI](https://github.com/moj4b/claude-mem/actions/workflows/ci.yml/badge.svg)](https://github.com/moj4b/claude-mem/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/moj4b/claude-mem?sort=semver)](https://github.com/moj4b/claude-mem/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A CLI for reading Claude Code's per-project memory. Go, stdlib only, single static binary.

Claude Code saves memories as markdown files under
`${CLAUDE_CONFIG_DIR:-~/.claude}/projects/<slug>/memory/`. `mem` reads them — it lists, greps and
prints them, and completes their names on TAB.

```
$ cd ~/projects/courier && mem
memory: /home/you/.claude/projects/-home-you-projects-courier/memory
5 memories · index: MEMORY.md

user
  user_prefs            User's experience level, communication style, and proje…
feedback
  feedback_build_cache  Known issues with Docker layer caching and CI build tim…
project
  project_targets       The two runtimes the service must support — and why the…
  project_roadmap       User is considering renaming the service — may affect d…
  project_milestones    Current implementation phase and what's been completed …
```

## Install

### Prebuilt binary

```sh
curl -fsSL https://raw.githubusercontent.com/moj4b/claude-mem/main/install.sh | sh
```

Installs the latest release to `~/.local/bin` (or `/usr/local/bin` as root), after checking the
download against the release's `checksums.txt`. Set `MEM_INSTALL_DIR` to install somewhere else, or
`MEM_VERSION` to pin a tag.

If you would rather not pipe a script into a shell, the archive is two commands — `latest/download`
is a stable URL, so there is no version to look up:

```sh
curl -fsSL https://github.com/moj4b/claude-mem/releases/latest/download/mem_linux_amd64.tar.gz | tar -xz mem
install -m 0755 mem ~/.local/bin/mem
```

Builds are published for `linux_amd64`, `linux_arm64`, `darwin_amd64` and `darwin_arm64`. Every
archive holds one static binary — no runtime, no dependencies.

Windows is not built or released. The path layer is written against `filepath` and looks portable,
but nothing has verified it: the test suite itself is not portable yet, so there is no evidence to
stand a release on. Linux and macOS are what CI runs and what ships.

### With Go

```sh
go install github.com/moj4b/claude-mem/cmd/mem@latest
```

### From source

```sh
git clone https://github.com/moj4b/claude-mem
cd claude-mem
go build -o mem ./cmd/mem
install -m 0755 mem ~/.local/bin/mem
```

Needs Go 1.23 or newer. No dependencies — `go list -m all` reports only this module.

### Completion

```sh
echo 'eval "$(mem completion bash)"' >> ~/.bashrc
```

## Layout

```
cmd/mem/            entry point: argv → cli.Run → exit code
internal/term/      TTY detection, width, colour, truncation
internal/memory/    Memory, Store, frontmatter and index parsing,
                    name matching, content search
internal/resolve/   the six-layer path walk, slugs, settings
internal/cli/       argument parsing, dispatch, exit codes, scope,
                    one file per command, and every formatter
```

Dependencies run one way: `term`, `memory` and `resolve` are independent leaves;
only `cli` imports them. Rendering lives entirely in `cli`, so the domain
packages never format output — which is what keeps the TTY and pipe views in
one place.

## Scope: which project a command acts on

**Every command acts on the current project's memory, and only that, unless you explicitly widen
it.** No command silently searches other projects.

The current project is the nearest ancestor directory of the working directory (including it) that
has a memory directory, searching upward and stopping at the git root, or at `$HOME` if the
directory is not in a repo. So `mem list` works from `~/projects/courier/client/src`, and git is not
required — memory directories exist for projects that are not repos.

| Command | Default scope | Can widen to |
|---|---|---|
| `list` | current project | `--all`, `--project <name>` |
| `search` | current project | `--all`, `--project <name>` |
| `read` | current project | `--project <name>` — **never `--all`** |
| `path` | current project | `--all` (lists every memory directory) |
| `projects` | always all — that is its purpose | — |

`read` has no `--all` because it must resolve to exactly one memory, and the obvious query is the
colliding one: a name like `user_prefs` typically exists in every project you have. Instead, a
miss tells you where the name does live, as a command you can run:

```
$ mem read user_prefs
no memory matches 'user_prefs'
  found in 3 other projects:
    mem read --project courier user_prefs
    mem read --project ledger user_prefs
    mem read --project photo-lib user_prefs
```

A `search` that finds nothing locally does the same for `--all`.

Scope is always on the first line of output, so you never have to guess which memory you are
looking at.

## Commands

```
mem                       alias for `mem list`
mem list                  list memories, grouped by type
mem read <name>           print a memory; partial names accepted
mem show <name>           alias for `read`
mem search <query>        case-insensitive search across memory contents
mem path [--explain]      print the resolved memory directory
mem projects              list every project that has memory
mem completion bash       print the bash completion script
mem --help | -h
mem --version | -v
```

Flags are accepted before or after the subcommand. `--dir`, `--all` and `--project` are mutually
exclusive.

| Flag | Applies to | Meaning |
|---|---|---|
| `--dir <path>` | all | Use this directory, skipping resolution |
| `--all` | `list`, `search`, `path` | Widen to every project that has memory |
| `--project <name>` | `list`, `search`, `read` | Act on a different project's memory |

### `list`

Groups by type in the order `user`, `feedback`, `project`, `reference`, then any other type
alphabetically, then untyped last. Descriptions come from the frontmatter `description`, falling
back to the memory's hook in `MEMORY.md`. `MEMORY.md` itself is excluded from the groups and
counted in the header.

`mem list --all` groups by project first, most memories first.

### `read`

```
$ mem read naming
Roadmap for the next release
project · project_roadmap.md

As of 2026-01-15, user is exploring renaming the app — the current name collides…
```

Piped, it emits the file byte-for-byte with frontmatter intact, so `mem read naming > copy.md`
yields a valid memory file. `mem read MEMORY` works even though `list` hides the index.

### `search`

```
$ mem search docker
memory: /home/you/.claude/projects/-home-you-projects-courier/memory
9 matches in 3 memories for "docker"

feedback_build_cache.md
   2  name: Build cache misses on every CI run
   7  1. **Layer cache invalidated by a stale ARG**: the base image rebuil…
```

Case-insensitive literal substring — not a regex. It searches the full raw file including
frontmatter, so `mem search 'type: reference'` matches on type. Piped, it emits grep format:
`feedback_build_cache.md:7:1. **Layer cache invalidated by a stale ARG**…`, or
`courier:feedback_build_cache.md:7:…` under `--all`.

### `projects`

```
$ mem projects
9 projects with memory · 41 memories total

  ledger                       12
  report-gen                    9
  courier                       5
  sandbox                       1   (no index)
  claude-mem                    0
```

Projects with an empty memory directory are listed with a count of 0 — knowing a project exists but
holds nothing is useful. Piped: `<name>\t<count>\t<absolute path>`.

### `path`

Prints the resolved directory, or nothing at all and exit 3 if there is none, so `cd $(mem path)`
cannot land somewhere wrong. `mem path --all` prints every memory directory, one absolute path per
line, for piping into `xargs`/`rg`.

## Path resolution

First hit wins:

| # | Layer | Source |
|---|---|---|
| 1 | `--dir <path>` | CLI flag — used verbatim |
| 2 | `--project <name>` | CLI flag — resolved against the projects root, **skipping layers 3–5** |
| 3 | `$CLAUDE_MEM_DIR` | this tool's own escape hatch |
| 4 | Project settings | `.claude/settings.local.json` then `.claude/settings.json`, cwd upward |
| 5 | User settings | `${CLAUDE_CONFIG_DIR:-~/.claude}/settings.json` |
| 6 | Computed default | `${CLAUDE_CONFIG_DIR:-~/.claude}/projects/<slug>/memory` |

Layers 1–5 are explicit intent: one pointing nowhere is an error to report, not a reason to fall
through to the default. Only layer 6 is a candidate walk, where the first directory that exists
wins.

`--project` skips the settings layers deliberately. Those settings belong to the directory you are
standing in, not the project you asked for; letting them redirect `mem --project courier list` would
show you something other than courier's memory.

The settings key is **`autoMemoryDirectory`** — the one Claude Code itself reads, from
`.claude/settings.local.json`, then `.claude/settings.json`, then `~/.claude/settings.json`. Its
value must be absolute, with `~/` expanded; Claude Code rejects a relative path outright rather
than resolving it against anything, and so does `mem`. Projects that keep memory beside the code
instead of under `~/.claude` are the reason this key matters:

```
$ cat .claude/settings.local.json
{ "autoMemoryDirectory": "/home/you/projects/bookmarks/.claude/memory" }
$ mem path
/home/you/projects/bookmarks/.claude/memory
```

Also read, below it and liberally: `memoryDir`, `memoryPath`, `memoryDirectory`, `claudeMemoryDir`,
and nested `memory.dir` / `memory.path`. None of these appear in Claude Code; they are kept only as
a fallback. A settings file that is missing, unreadable or invalid JSON is skipped silently.

**A configured directory wins whether or not it exists.** Claude Code validates the value's form
and returns it without ever asking the filesystem; it computes the default only when no layer
supplies a well-formed one. So it reads the configured directory — creating it on the next session,
or reading nothing if it cannot — and never consults the default. `mem` reports the same directory,
because the goal is that `mem path` names exactly what Claude Code will use, even when the answer
is a directory that is not there:

```
$ mem path                       # settings name a path this machine does not have
no memory directory for this project
  looked for: /opt/shared/ledger/.claude/memory
  named by autoMemoryDirectory in /home/you/projects/ledger/.claude/settings.local.json,
  which is where Claude Code reads too
  create it, or fix the setting — other memory is still reachable with --project/--dir
```

Checked-in settings routinely name another machine's or a container's path, and older memories may
well be sitting at the default location. Those are no longer this project's memory as far as Claude
Code is concerned, so `mem` will not show them here — reach them with `mem --project <name>` or
`mem --dir <path>`.

**The displaced directory is not another project's either.** It is still on disk under the projects
root, holding what Claude Code wrote there before the key landed, so `search` and `read` name it as
what it is — this project's own history — rather than counting it among the other projects:

```
$ mem search docker              # memory is configured beside the code
no memory matches "docker" in this project
  1 memory in the directory the memory setting replaced
    mem search --dir /home/you/.claude/projects/-home-you-projects-edge-proxy/memory docker
  found in 3 memories across 2 other projects — retry with: mem search --all docker
```

It is offered by path — always absolute, as printed — rather than by `--project`, because under the
projects root it carries this project's own name, and `mem read --project edge-proxy x` would read as
pointing somewhere else. `mem read` offers every matching name the same way, and `mem list` says the
same thing when the configured directory is there but empty — the state a project is in the moment
the key is added.

Only a settings key displaces anything. `--dir`, `--project` and `$CLAUDE_MEM_DIR` retarget a single
invocation without restating where the project keeps its memory, so under them the default directory
is still the project's own and still appears in the hints as usual.

What counts as displaced depends on whether the key came with a project boundary attached:

- **A `.claude/settings*.json` in the tree** declares one. Everything at or below that directory is
  one project, so every default from the working directory up to it is displaced — plural, because
  the default is keyed on where `claude` was launched rather than on the repo root, and one project
  can have several.
- **A user-level key** (`~/.claude/settings.json`) declares nothing, so exactly one directory is
  displaced: the one layer 6 would itself have answered with — the nearest existing default, and
  nothing above it.

A key found in `${CLAUDE_CONFIG_DIR:-~/.claude}/settings.json` is always the second kind, even
though the ancestor walk reaches that file like any other directory's. It is the user-level file by
convention, not `$HOME` declaring itself a project.

Either way an enclosing project is left alone when it is genuinely separate — one that has its own
memory directory, which `mem projects` names and `mem --project <name>` reaches. When it does not,
it was never separate: `mem path` resolves there too, and displacing it is the same answer both
commands give.

The projects-root views — `--all`, `projects`, `--project` and project-name completion — deliberately
show the displaced directory as an ordinary project, because every row they print has to stay
reachable by `mem read --project <name> <file>`, and because a settings key belongs to the directory
you are standing in rather than to a project you named from elsewhere.

The one fall-through is about the value's form, never about whether the directory is there: a
relative or otherwise malformed value is treated as unset, exactly as Claude Code treats it, and
the next source is consulted.

`mem path --explain` prints the path to stdout and the full trace to stderr:

```
$ mem path --explain
  .claude/settings.*       memory key in /home/you/projects/edge-proxy/.claude/settings.json
→ /opt/shared/edge-proxy/.claude/memory
```

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success — **including an existing-but-empty memory directory** |
| 1 | Not found — no match, ambiguous name, or zero search results |
| 2 | Usage error — unknown subcommand, missing argument, bad flag |
| 3 | No memory directory |

The distinction the tool never blurs: **no memory directory (3) ≠ directory is empty (0) ≠ name not
found (1).** Three situations, three user actions.

## Name matching

Queries are normalized (lowercase, trailing `.md` stripped) and matched in tiers: exact, then
prefix, then substring, then subsequence fuzzy. Within a tier, one hit wins and several is an
error listing the candidates — ambiguity is reported, never guessed. So `mem read docker` finds
`feedback_build_cache`, and an exact filename always beats a coincidental fuzzy hit.

## Output

Colour, alignment and truncation appear only when stdout is a terminal; `NO_COLOR` (any non-empty
value) disables colour. Piped output is stable and tab-separated, payload only — headers and
diagnostics go to stderr, so `mem read x > x.md` never contains one.

## Completion

```sh
eval "$(mem completion bash)"
```

The emitted script is a thin shim; all the logic lives in the binary behind a hidden `__complete`,
so memory-name completion stays in sync automatically. It completes subcommands, flags, memory
names, and project names, and it honours scope — `mem read --project courier <TAB>` offers courier's
memories. It never writes to stderr and never exits non-zero, because a noisy completion would
corrupt the prompt line. Measured at 2 ms.

## Tests

```sh
go test ./...                                # unit tests — self-contained, no fixtures on disk
go vet ./...
```

That is what CI runs, on Linux and macOS, at Go 1.23 and stable. They do not yet pass on Windows,
for reasons that are the tests' own — see the note under [Install](#prebuilt-binary).

## Provenance

Most of this code was written by an AI assistant, working from a written specification and against
the tests above. Read it the way you would read any code you did not write yourself.

## Contributing

Issues and pull requests are welcome. Please keep `go test ./...`, `go vet ./...` and `gofmt -l .`
clean; CI checks all three. The tool is deliberately stdlib-only — a change that adds a dependency
needs to argue for it.

## License

[MIT](LICENSE).
