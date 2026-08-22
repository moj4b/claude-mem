package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/moj4b/claude-mem/internal/memory"
	"github.com/moj4b/claude-mem/internal/term"
)

// cmdRmFn removes exactly one memory. Like `read` it never widens: a delete
// resolves to one file or it does not happen (§II.0, §II.5).
func cmdRmFn(o options, stdout, stderr io.Writer) int {
	sc, code := resolveScope(o, stderr)
	if code != exitOK {
		return code
	}
	if len(o.args) > 1 {
		return fail(stderr, exitUsage, fmt.Errorf(
			"rm removes one memory at a time\n  usage: mem rm <name>"))
	}
	s := sc.Stores[0]
	// A blank operand is not a name. The prefix tier of §II.8 matches every
	// memory against "", so an unset shell variable would otherwise resolve to
	// whatever happens to be in the directory. `read` shares this shape
	// harmlessly; a delete cannot afford it.
	//
	// Tested through MatchKey, not TrimSpace: the extension is stripped during
	// matching, so ".md" reaches Match as the empty query too — and
	// `mem rm "$name.md"` with $name unset is the likelier accident.
	query := strings.TrimSpace(o.args[0])
	if memory.MatchKey(query) == "" {
		return fail(stderr, exitUsage, fmt.Errorf(
			"rm requires a memory name\n  usage: mem rm <name>"))
	}
	m, err := memory.Match(s.Readable(), query)
	if err != nil {
		return fail(stderr, exitNotFound, err)
	}
	// The index is in Readable() so `read` can print it (§II.5). Resolving to
	// it here is refused rather than dropped from the pool: matching against
	// the memories alone would let "MEMORY" fuzzy-match some unrelated file.
	if m.File == memory.IndexFile {
		return fail(stderr, exitUsage, fmt.Errorf(
			"%s is the index, not a memory\n  remove a memory and its line goes with it",
			memory.IndexFile))
	}
	// The last check before an irreversible step, and deliberately independent
	// of how m was resolved: only a .md file that is a direct child of this
	// memory directory may be unlinked, and never a directory. Load's filter
	// already agrees on almost all of that, so most of this is defence in
	// depth against a later change to Load or Match — but not all of it: a
	// file named exactly ".md" passes Load's suffix test and is refused only
	// here.
	if err := memory.Removable(s.Dir, m.Path); err != nil {
		return fail(stderr, exitUsage, err)
	}
	if !o.force {
		if !promptInteractive() {
			return fail(stderr, exitUsage, fmt.Errorf(
				"rm needs a terminal to confirm\n  pass --force to remove %s without asking", m.Name))
		}
		if !confirm(m, stderr) {
			fmt.Fprintf(stderr, "kept %s\n", m.Name)
			return exitNotFound
		}
	}
	// Computed before anything changes, from the store as it stood when the
	// memory was still in it.
	inbound := memory.Backlinks(s.Memories, m.Name)

	// The index is rewritten first. Of the two ways a half-done removal can
	// end, this picks the recoverable one: a memory whose index line is gone
	// is still listed, because `list` reads the directory rather than the
	// index, and re-running finishes the job. A memory removed while its
	// pointer survives is the failure this command exists to prevent, and no
	// re-run can repair it — `rm` would no longer find the name.
	indexDropped, err := dropIndexEntry(s.Dir, m.File, stderr)
	if err != nil {
		return fail(stderr, exitNotFound, err)
	}
	if err := os.Remove(m.Path); err != nil {
		fmt.Fprintf(stderr, "cannot remove memory: %s\n", m.Path)
		if indexDropped {
			fmt.Fprintf(stderr, "  its line in %s is already gone — run the same command again\n",
				memory.IndexFile)
		}
		return exitNotFound
	}
	fmt.Fprintf(stdout, "removed %s (%s)\n", m.Name, m.File)
	if indexDropped {
		fmt.Fprintf(stdout, "  and its line in %s\n", memory.IndexFile)
	}
	writeBacklinkNote(inbound, m.Name, stderr)
	return exitOK
}

// promptIn and promptInteractive are the confirmation's seam: what to read an
// answer from, and whether anyone is there to type one.
var (
	promptIn          io.Reader = os.Stdin
	promptInteractive           = func() bool { return term.IsTTYFile(os.Stdin) }
)

// confirm asks before the irreversible step. It names what the query actually
// resolved to, because a name is matched in four tiers down to a fuzzy one
// (§II.8) — `mem rm ur` can reach user_prefs, and the prompt is the only place
// that guess becomes visible. Anything but y/yes is no, so a bare Enter keeps
// the memory.
func confirm(m memory.Memory, stderr io.Writer) bool {
	fmt.Fprintf(stderr, "remove %s (%s)? [y/N] ", m.Name, m.File)
	line, _ := bufio.NewReader(promptIn).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

// writeBacklinkNote names the memories that still link to what was just
// removed. Advisory, so it goes to stderr and never changes the exit code: an
// unresolved [[link]] marks intent to write that memory, not a broken file.
func writeBacklinkNote(inbound []memory.Memory, name string, stderr io.Writer) {
	if len(inbound) == 0 {
		return
	}
	verb := "still link to"
	if len(inbound) == 1 {
		verb = "still links to"
	}
	fmt.Fprintf(stderr, "%s %s [[%s]]:\n",
		plural(len(inbound), "memory", "memories"), verb, name)
	for _, m := range inbound {
		fmt.Fprintf(stderr, "  %s\n", m.Name)
	}
}

// dropIndexEntry removes the memory's pointer line from MEMORY.md, reporting
// whether it found one. An absent or entry-free index is not an error; a
// rewrite that cannot be completed is, and is reported before anything has
// been unlinked.
func dropIndexEntry(dir, file string, stderr io.Writer) (bool, error) {
	path := filepath.Join(dir, memory.IndexFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // no index is normal (§II.4), not an error
		}
		// An index that is there but unreadable is not the same situation, and
		// treating it as one removes the memory while whatever it says about
		// it stays behind.
		return false, fmt.Errorf("cannot read %s: %s\n  nothing was removed",
			memory.IndexFile, path)
	}
	next, removed := memory.RemoveIndexEntry(data, file)
	// Whether or not a bullet went, the index may still point at the file — a
	// bare link is read as an entry but is never deleted, since it is more
	// often prose than a pointer. Saying so beats implying it is gone.
	if memory.PointsAt(next, file) {
		fmt.Fprintf(stderr, "%s still points at %s — the line is not a bullet, so it was left alone\n",
			memory.IndexFile, file)
	}
	if !removed {
		return false, nil
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := writeAtomic(path, next, mode); err != nil {
		return false, fmt.Errorf("cannot rewrite %s: %s\n  nothing was removed", memory.IndexFile, path)
	}
	return true, nil
}

// writeAtomic replaces path through a temporary file in the same directory,
// the same way update.WriteState does. Two properties matter here: a write
// that fails cannot truncate the index it was replacing, and a symlinked
// MEMORY.md is replaced rather than written through, so the rewrite stays
// inside the memory directory the unlink was confined to.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mem-index-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // a no-op once the rename below has moved it
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		return err
	}
	return os.Rename(name, path)
}
