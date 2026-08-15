package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/moj4b/claude-mem/internal/memory"
	"github.com/moj4b/claude-mem/internal/term"
)

// typeOrder is the group order of §II.4. Types outside it sort
// alphabetically after these, and untyped memories come last.
var typeOrder = []string{"user", "feedback", "project", "reference"}

// untypedGroup is the heading for memories with no type (§II.4).
const untypedGroup = "untyped"

// header writes the scope line. Scope must always be visible: the user must
// never have to guess which memory they are looking at (§II.0). When stdout is
// a pipe the payload must stay clean, so it goes to stderr instead (§II.2).
func header(sc Scope, stdout, stderr io.Writer, tty bool, extra string) {
	w := stdout
	if !tty {
		w = stderr
	}
	fmt.Fprintln(w, sc.Label)
	if extra != "" {
		fmt.Fprintln(w, extra)
	}
	if tty {
		fmt.Fprintln(w)
	}
}

// cmdListFn lists the memories in scope (§II.4).
func cmdListFn(o options, stdout, stderr io.Writer) int {
	sc, code := resolveScope(o, stderr)
	if code != exitOK {
		return code
	}
	tty := term.IsTTY(stdout)

	if sc.Widened() {
		header(sc, stdout, stderr, tty, "")
		renderListAll(sc, stdout, tty)
		return exitOK
	}

	s := sc.Stores[0]
	// An existing-but-empty directory is success (§II.9) — a different
	// situation from a missing one, calling for a different user action.
	if len(s.Memories) == 0 {
		fmt.Fprintf(stderr, "memory directory is empty: %s\n", s.Dir)
		writeDisplacedOffers(sc, "list", stderr)
		return exitOK
	}
	renderList(s, stdout, tty)
	return exitOK
}

// renderList writes a store as either the aligned, grouped, truncated TTY view
// or the tab-separated plain view consumed by scripts (§II.4).
func renderList(s memory.Store, w io.Writer, tty bool) {
	if !tty {
		for _, m := range s.Memories {
			fmt.Fprintf(w, "%s\t%s\t%s\n", m.Name, m.Type, m.Description)
		}
		return
	}
	index := "no index"
	if s.HasIndex {
		index = "index: " + memory.IndexFile
	}
	fmt.Fprintf(w, "memory: %s\n", s.Dir)
	fmt.Fprintf(w, "%s · %s\n\n", plural(len(s.Memories), "memory", "memories"), index)
	writeGroups(s.Memories, w, term.UseColor(w), nameWidth(s.Memories), "")
}

// renderListAll groups by project first, then by type within each project
// (§II.4). Projects with an empty memory directory are omitted here but were
// still counted in the header.
func renderListAll(sc Scope, w io.Writer, tty bool) {
	stores := byCountThenName(sc.Stores)
	if !tty {
		for _, s := range stores {
			for _, m := range s.Memories {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Project, m.Name, m.Type, m.Description)
			}
		}
		return
	}
	color := term.UseColor(w)
	for _, s := range stores {
		if len(s.Memories) == 0 {
			continue
		}
		fmt.Fprintf(w, "%s  (%d)\n", term.Paint(color, term.Bold, s.Project), len(s.Memories))
		writeGroups(s.Memories, w, color, nameWidth(s.Memories), "  ")
		fmt.Fprintln(w)
	}
}

// writeGroups writes type-grouped memory rows at the given indent.
func writeGroups(ms []memory.Memory, w io.Writer, color bool, nameCol int, indent string) {
	total := term.Width()
	for _, g := range groupByType(ms) {
		fmt.Fprintf(w, "%s%s\n", indent, term.Paint(color, term.Bold, g.name))
		for _, m := range g.memories {
			pad := strings.Repeat(" ", nameCol-len([]rune(m.Name)))
			// indent + 2 leading spaces + name column + 2 separating spaces.
			desc := term.Truncate(m.Description, total-nameCol-4-len(indent))
			line := indent + "  " + term.Paint(color, term.Cyan, m.Name) + pad + "  " +
				term.Paint(color, term.Dim, desc)
			fmt.Fprintln(w, strings.TrimRight(line, " "))
		}
	}
}

func nameWidth(ms []memory.Memory) int {
	n := 0
	for _, m := range ms {
		if l := len([]rune(m.Name)); l > n {
			n = l
		}
	}
	return n
}

type memoryGroup struct {
	name     string
	memories []memory.Memory
}

// groupByType buckets memories per §II.4. Memories arrive sorted by filename
// from memory.Load, so each group stays in filename order.
func groupByType(ms []memory.Memory) []memoryGroup {
	buckets := map[string][]memory.Memory{}
	for _, m := range ms {
		key := m.Type
		if key == "" {
			key = untypedGroup
		}
		buckets[key] = append(buckets[key], m)
	}
	var out []memoryGroup
	seen := map[string]bool{}
	for _, t := range typeOrder {
		if ms, ok := buckets[t]; ok {
			out = append(out, memoryGroup{t, ms})
			seen[t] = true
		}
	}
	var rest []string
	for t := range buckets {
		if !seen[t] && t != untypedGroup {
			rest = append(rest, t)
		}
	}
	sort.Strings(rest)
	for _, t := range rest {
		out = append(out, memoryGroup{t, buckets[t]})
	}
	if ms, ok := buckets[untypedGroup]; ok {
		out = append(out, memoryGroup{untypedGroup, ms})
	}
	return out
}
