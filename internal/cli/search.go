package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/moj4b/claude-mem/internal/memory"
	"github.com/moj4b/claude-mem/internal/term"
)

// cmdSearchFn searches the memories in scope (§II.6).
func cmdSearchFn(o options, stdout, stderr io.Writer) int {
	sc, code := resolveScope(o, stderr)
	if code != exitOK {
		return code
	}
	query := o.args[0]
	tty := term.IsTTY(stdout)

	if sc.Widened() {
		return searchAll(sc, query, stdout, stderr, tty)
	}

	s := sc.Stores[0]
	hits := memory.Search(s, query)
	if len(hits) == 0 {
		// Dead-ending is the wrong answer for a "where did I write that down?"
		// tool, and the whole-machine scan costs milliseconds (§II.0, §II.6).
		n, projects := countElsewhere(sc, query)
		left := searchDisplaced(sc, query)
		if n == 0 && len(left) == 0 {
			fmt.Fprintf(stderr, "no memory matches %q\n", query)
			return exitNotFound
		}
		fmt.Fprintf(stderr, "no memory matches %q in this project\n", query)
		for _, d := range left {
			fmt.Fprint(stderr, displacedOffer(d.n, "search", d.dir, query))
		}
		if n > 0 {
			fmt.Fprintf(stderr, "  found in %s across %s — retry with: mem search --all %s\n",
				plural(n, "memory", "memories"),
				plural(projects, "other project", "other projects"), query)
		}
		return exitNotFound
	}
	renderSearch(s, hits, query, stdout, tty)
	return exitOK
}

// searchAll groups results by project, in descending order of match count
// (§II.6). A hit in another project must be readable from the output, so the
// project heading and file name together give `mem read --project X file`.
func searchAll(sc Scope, query string, stdout, stderr io.Writer, tty bool) int {
	type group struct {
		store memory.Store
		hits  []memory.Hit
	}
	var groups []group
	files, total := 0, 0
	for _, s := range sc.Stores {
		if hits := memory.Search(s, query); len(hits) > 0 {
			groups = append(groups, group{s, hits})
			total += len(hits)
			files += memory.CountFiles(hits)
		}
	}
	if total == 0 {
		fmt.Fprintf(stderr, "no memory matches %q in any project\n", query)
		return exitNotFound
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if len(groups[i].hits) != len(groups[j].hits) {
			return len(groups[i].hits) > len(groups[j].hits)
		}
		return groups[i].store.Project < groups[j].store.Project
	})
	if tty {
		fmt.Fprintln(stdout, sc.Label)
		fmt.Fprintf(stdout, "matches in %s across %s for %q\n",
			plural(files, "memory", "memories"), plural(len(groups), "project", "projects"), query)
	} else {
		fmt.Fprintln(stderr, sc.Label)
	}
	for _, g := range groups {
		if !tty {
			for _, h := range g.hits {
				fmt.Fprintf(stdout, "%s:%s:%d:%s\n", g.store.Project, h.File, h.Line, h.Text)
			}
			continue
		}
		fmt.Fprintf(stdout, "\n%s  (%d)\n",
			term.Paint(term.UseColor(stdout), term.Bold, g.store.Project), len(g.hits))
		writeHits(g.hits, query, stdout, term.UseColor(stdout), "  ")
	}
	return exitOK
}

type displacedCount struct {
	dir string
	n   int
}

// searchDisplaced reports, per directory the memory setting replaced, how
// many of its memories hold the query — skipping the ones that hold none, so a
// miss stays as quiet as it was (§II.0).
func searchDisplaced(sc Scope, query string) []displacedCount {
	var out []displacedCount
	for _, s := range sc.displaced() {
		if n := memory.CountFiles(memory.Search(s, query)); n > 0 {
			out = append(out, displacedCount{s.Dir, n})
		}
	}
	return out
}

// countElsewhere reports how many memories in other projects hold the query,
// and how many projects those are, for the --all hint (§II.6).
func countElsewhere(sc Scope, query string) (memories, projects int) {
	for _, s := range sc.otherProjects() {
		if hits := memory.Search(s, query); len(hits) > 0 {
			memories += memory.CountFiles(hits)
			projects++
		}
	}
	return memories, projects
}

// renderSearch writes results as grep-format lines for a pipe, or grouped and
// aligned for a human (§II.6).
func renderSearch(s memory.Store, hits []memory.Hit, query string, w io.Writer, tty bool) {
	if !tty {
		for _, h := range hits {
			fmt.Fprintf(w, "%s:%d:%s\n", h.File, h.Line, h.Text)
		}
		return
	}
	fmt.Fprintf(w, "memory: %s\n", s.Dir)
	fmt.Fprintf(w, "%s in %s for %q\n",
		plural(len(hits), "match", "matches"),
		plural(memory.CountFiles(hits), "memory", "memories"), query)
	writeHits(hits, query, w, term.UseColor(w), "")
}

// writeHits writes file-grouped hits with right-aligned line numbers, the
// match highlighted and the line trimmed to width (§II.6).
func writeHits(hits []memory.Hit, query string, w io.Writer, color bool, indent string) {
	maxLine := 0
	for _, h := range hits {
		if h.Line > maxLine {
			maxLine = h.Line
		}
	}
	widest := len(fmt.Sprint(maxLine))
	total := term.Width()
	current := ""
	for _, h := range hits {
		if h.File != current {
			current = h.File
			fmt.Fprintf(w, "\n%s%s\n", indent, term.Paint(color, term.Bold, h.File))
		}
		// indent + 2 leading spaces + number column + 2 separating spaces.
		text := term.Truncate(strings.TrimRight(h.Text, " \t"), total-widest-4-len(indent))
		fmt.Fprintf(w, "%s  %*d  %s\n", indent, widest, h.Line, highlight(text, query, color))
	}
}

// highlight marks every occurrence of query in line when colour is on.
func highlight(line, query string, color bool) string {
	if !color || query == "" {
		return line
	}
	lower, q := strings.ToLower(line), strings.ToLower(query)
	var b strings.Builder
	for {
		i := strings.Index(lower, q)
		if i < 0 {
			b.WriteString(line)
			return b.String()
		}
		b.WriteString(line[:i])
		b.WriteString(term.Paint(true, term.Yellow, line[i:i+len(q)]))
		line, lower = line[i+len(q):], lower[i+len(q):]
	}
}
