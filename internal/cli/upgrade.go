package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/moj4b/claude-mem/internal/term"
	"github.com/moj4b/claude-mem/internal/update"
)

// §II.12 — staying current.
//
// Two halves that never block each other. `mem upgrade` is the deliberate one:
// it fetches, verifies and swaps the binary while the user watches. The notice
// is the ambient one: every other command prints a line on stderr when a newer
// release is known, and *knowing* costs no network at all, because the answer
// comes from a cache file that a detached background process refreshes once a
// day. No command on the read path ever waits on GitHub — §II.2's speed rule
// survives intact.

const (
	// updateCheckCmd is the hidden background refresh, spawned by the notice.
	updateCheckCmd = "__update-check"

	// checkTimeout bounds the background refresh: it is detached, so nothing
	// would ever reap a hung connection.
	checkTimeout = 15 * time.Second

	// upgradeTimeout bounds a whole upgrade — feed, archive, checksums — with
	// room for a slow link.
	upgradeTimeout = 5 * time.Minute

	// exitUpgradeFailed is what a failed upgrade returns. It is exitNotFound:
	// "could not do what you asked" already has a code, and exit 3 is reserved
	// for the one thing it means everywhere else — no memory directory (§II.9).
	exitUpgradeFailed = exitNotFound
)

// The endpoints and the cache location are variables so the tests can point the
// whole path — feed, download, checksum, swap — at a local server and a
// temporary file.
var (
	updateAPI       = update.DefaultAPI
	updateDownloads = update.DefaultDownloads
	updateStatePath = update.StatePath

	newUpgrader = func(current string, progress io.Writer) (update.Upgrader, error) {
		u, err := update.New(current)
		if err != nil {
			return u, err
		}
		u.API, u.Downloads = updateAPI, updateDownloads
		u.Client = &http.Client{Timeout: upgradeTimeout}
		u.Note = func(s string) { fmt.Fprintln(progress, s) }
		return u, nil
	}
)

// cmdUpgradeFn is `mem upgrade` (§II.12). Progress goes to stderr and the
// outcome to stdout, so the answer to "what happened" is one line and the noise
// around it is separable (§II.2).
func cmdUpgradeFn(o options, stdout, stderr io.Writer) int {
	current := versionString()
	u, err := newUpgrader(current, stderr)
	if err != nil {
		return fail(stderr, exitUpgradeFailed, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), upgradeTimeout)
	defer cancel()

	latest, err := u.Latest(ctx)
	if err != nil {
		return fail(stderr, exitUpgradeFailed, fmt.Errorf(
			"could not reach the release feed: %w\n  see %s", err, update.ReleasesPage))
	}
	// An explicit check is a check: it resets the notice's clock, so asking by
	// hand does not leave a background refresh queued behind it.
	rememberLatest(latest)

	switch {
	case o.check:
		printCheck(stdout, current, latest)
		return exitOK
	case current == latest:
		fmt.Fprintf(stdout, "mem %s is already the latest release\n", current)
		return exitOK
	case update.IsRelease(current) && !update.Newer(current, latest):
		// Ahead of the feed — a release candidate, or a build from a tag that
		// has not been published. Say so; do not quietly downgrade it.
		fmt.Fprintf(stdout, "mem %s is newer than the latest release (%s) — nothing to do\n", current, latest)
		return exitOK
	}

	if !update.IsRelease(current) {
		// A `go build` or `go install` from source: there is no version to
		// compare, but there is still an obvious thing the user asked for.
		fmt.Fprintf(stderr, "this build reports %s, so there is nothing to compare — installing %s\n", current, latest)
	}
	fmt.Fprintf(stderr, "upgrading mem %s → %s\n", current, latest)
	if err := u.Install(ctx, latest); err != nil {
		return fail(stderr, exitUpgradeFailed, err)
	}
	fmt.Fprintf(stdout, "upgraded to mem %s\n  %s\n", latest, u.ExecPath)
	return exitOK
}

// printCheck answers `mem upgrade --check` without installing anything.
func printCheck(stdout io.Writer, current, latest string) {
	switch {
	case update.Newer(current, latest):
		fmt.Fprintf(stdout, "mem %s is available (you have %s)\n  run `mem upgrade` to install it\n", latest, current)
	case current == latest:
		fmt.Fprintf(stdout, "mem %s is the latest release\n", current)
	default:
		fmt.Fprintf(stdout, "the latest release is mem %s (this build reports %s)\n", latest, current)
	}
}

// cmdUpdateCheckFn is the hidden background refresh. It is spawned detached
// with no terminal, so it must be silent, must not block forever, and must
// always exit 0 — nothing will ever read its status.
func cmdUpdateCheckFn(o options, stdout, stderr io.Writer) int {
	path, err := updateStatePath()
	if err != nil {
		return exitOK
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()

	st := update.ReadState(path)
	st.CheckedAt = time.Now()
	if tag, err := update.Latest(ctx, &http.Client{Timeout: checkTimeout}, updateAPI); err == nil {
		st.Latest = tag
	}
	// A failed check still writes the timestamp — an unreachable feed must back
	// off for the full interval, not spawn a fresh child on every command. The
	// previous answer is kept: a version that was newer yesterday still is.
	_ = update.WriteState(path, st)
	return exitOK
}

// rememberLatest records what a foreground command already learned, so the
// notice neither repeats a version the user has just installed nor schedules a
// background check that would ask the same question again.
func rememberLatest(latest string) {
	path, err := updateStatePath()
	if err != nil {
		return
	}
	_ = update.WriteState(path, update.State{CheckedAt: time.Now(), Latest: latest})
}

// notifyUpdate prints the one line of §II.12, after the command has had its
// say, and schedules the next check. Everything it needs is already on disk, so
// it adds a file read to a command and nothing else.
func notifyUpdate(cmd string, stderr io.Writer) {
	if !notifyAllowed(cmd, term.IsTTY(stderr)) {
		return
	}
	path, err := updateStatePath()
	if err != nil {
		return
	}
	st := update.ReadState(path)
	if line := updateNotice(versionString(), st.Latest, term.UseColor(stderr)); line != "" {
		fmt.Fprintln(stderr, line)
	}
	if !st.Fresh(time.Now()) {
		refreshInBackground(path, st)
	}
}

// updateNotice is the line itself, or "" when there is nothing to say. It
// names both versions: "there is a newer one" is not actionable without
// knowing which one you are on.
func updateNotice(current, latest string, color bool) string {
	if !update.Newer(current, latest) {
		return ""
	}
	return term.Paint(color, term.Dim,
		fmt.Sprintf("mem %s is available (you have %s) — run `mem upgrade`", latest, current))
}

// notifyAllowed is every reason to stay quiet, in one place. It is given the
// terminal answer rather than asking for it, which keeps the decision a table
// a test can walk.
func notifyAllowed(cmd string, tty bool) bool {
	switch cmd {
	case "__complete", updateCheckCmd:
		// __complete runs on every TAB and must never write anything but
		// candidates (§II.10); the refresh is the machinery itself.
		return false
	case "completion":
		// Its output is eval'd from a shell rc file — a notice here would
		// greet every new shell.
		return false
	case "upgrade", "update":
		// It says its own piece, with a fresher answer than the cache.
		return false
	}
	if os.Getenv(update.NoCheckEnv) != "" {
		return false
	}
	// A terminal is a person. Pipes, scripts and CI get the payload they asked
	// for and nothing else (§II.2).
	if !tty {
		return false
	}
	// An unstamped local build has no place in the release order, so there is
	// nothing truthful to say about it.
	return update.IsRelease(versionString())
}

// refreshInBackground starts `mem __update-check` detached and returns
// immediately: the current command must not pay for the fetch, and its result
// is for tomorrow's run.
func refreshInBackground(path string, st update.State) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	// Claim the slot before spawning. A burst of commands then starts one child
	// rather than one per command, and a check that dies without writing still
	// backs off instead of respawning forever.
	st.CheckedAt = time.Now()
	if err := update.WriteState(path, st); err != nil {
		return
	}
	_ = update.Detach(exe, updateCheckCmd)
}
