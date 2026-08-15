package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/moj4b/claude-mem/internal/memory"
	"github.com/moj4b/claude-mem/internal/term"
)

// cmdReadFn prints one memory (§II.5). It never widens scope: it must resolve
// to exactly one memory, and cross-project name collisions are concentrated in
// exactly the name you would reach for (§II.0).
func cmdReadFn(o options, stdout, stderr io.Writer) int {
	sc, code := resolveScope(o, stderr)
	if code != exitOK {
		return code
	}
	s := sc.Stores[0]
	query := o.args[0]
	m, err := memory.Match(s.Readable(), query)
	if err != nil {
		fmt.Fprintf(stderr, "%s\n", err)
		// read cannot widen, so any cross-project result must print the exact
		// command that reaches it (§II.0). Still exit 1 — nothing was read.
		writeReadHints(sc, query, stderr)
		return exitNotFound
	}
	// Re-read at print time so the bytes are current, and so an unreadable
	// file is an error rather than silently empty output.
	raw, readErr := os.ReadFile(m.Path)
	if readErr != nil {
		fmt.Fprintf(stderr, "cannot read memory: %s\n", m.Path)
		return exitNotFound
	}
	m.Raw = raw
	renderRead(m, stdout, term.IsTTY(stdout))
	return exitOK
}

// writeReadHints names every other project holding the query, as a runnable
// command (§II.5). Restricted to exact name hits so the hint is never noise;
// falls back to substring only when nothing matches exactly.
func writeReadHints(sc Scope, query string, stderr io.Writer) {
	q := memory.MatchKey(query)
	for _, s := range sc.displaced() {
		// By path, not by project: under the projects root this directory carries
		// this project's own name, so --project would read as pointing elsewhere.
		// Every match in the winning tier, as the other-projects branch does —
		// same question, so the same answer shape.
		ms := winningTier(s, q)
		for i, m := range ms {
			if i == 0 {
				fmt.Fprint(stderr, displacedOffer(len(ms), "read", s.Dir, m.Name))
				continue
			}
			fmt.Fprintf(stderr, "    mem read --dir %s %s\n", s.Dir, m.Name)
		}
	}
	var exact, loose []string
	for _, s := range sc.otherProjects() {
		reach := func(m memory.Memory) string {
			return fmt.Sprintf("mem read --project %s %s", s.Project, m.Name)
		}
		e, l := tierNames(s, q)
		for _, m := range e {
			exact = append(exact, reach(m))
		}
		for _, m := range l {
			loose = append(loose, reach(m))
		}
	}
	hits := exact
	if len(hits) == 0 {
		hits = loose
	}
	if len(hits) == 0 {
		return
	}
	sort.Strings(hits)
	if len(hits) == 1 {
		fmt.Fprintf(stderr, "  found in another project: %s\n", hits[0])
		return
	}
	fmt.Fprintf(stderr, "  found in %d other projects:\n", len(hits))
	for _, h := range hits {
		fmt.Fprintf(stderr, "    %s\n", h)
	}
}

// tierNames splits a store's readable memories by how they match q: exact name
// hits, then substring ones. Callers use the second tier only when the first is
// empty, which is what keeps a hint from being noise (§II.5).
func tierNames(s memory.Store, q string) (exact, loose []memory.Memory) {
	for _, m := range s.Readable() {
		switch {
		case memory.MatchKey(m.Name) == q:
			exact = append(exact, m)
		case strings.Contains(memory.MatchKey(m.Name), q):
			loose = append(loose, m)
		}
	}
	return exact, loose
}

// winningTier is the memories in s worth naming for q: the exact ones, or the
// substring ones when there are no exact ones, and nothing when neither.
func winningTier(s memory.Store, q string) []memory.Memory {
	exact, loose := tierNames(s, q)
	if len(exact) > 0 {
		return exact
	}
	return loose
}

// renderRead writes a memory: decorated for a human, byte-identical for a
// pipe, so `mem read x > x.md` yields a valid memory file (§II.5).
func renderRead(m memory.Memory, w io.Writer, tty bool) {
	if !tty {
		w.Write(m.Raw)
		return
	}
	color := term.UseColor(w)
	subtitle := m.File
	if m.Type != "" {
		subtitle = m.Type + " · " + m.File
	}
	fmt.Fprintln(w, term.Paint(color, term.Bold, m.Title))
	fmt.Fprintln(w, term.Paint(color, term.Dim, subtitle))
	fmt.Fprintln(w)
	body := m.Body
	if body == "" {
		body = string(m.Raw)
	}
	fmt.Fprint(w, body)
	if !strings.HasSuffix(body, "\n") {
		fmt.Fprintln(w)
	}
}
