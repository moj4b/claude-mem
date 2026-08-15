package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/moj4b/claude-mem/internal/memory"
	"github.com/moj4b/claude-mem/internal/resolve"
	"github.com/moj4b/claude-mem/internal/term"
)

// cmdProjectsFn lists every project that has a memory directory (§II.7a). It
// exists to make --project discoverable and completable.
func cmdProjectsFn(o options, stdout, stderr io.Writer) int {
	r, err := resolve.New(o.dir, o.project)
	if err != nil {
		fmt.Fprintf(stderr, "%s\n", err)
		return exitNoDir
	}
	stores := byCountThenName(discoverProjects(r.ProjectsRoot()))
	if len(stores) == 0 {
		fmt.Fprintln(stderr, "no projects have memory yet")
		return exitOK
	}
	total := 0
	for _, s := range stores {
		total += len(s.Memories)
	}
	renderProjects(stores, total, stdout, term.IsTTY(stdout), term.UseColor(stdout))
	return exitOK
}

// renderProjects writes the project list: tab-separated for a pipe, aligned
// for a human (§II.7a).
func renderProjects(stores []memory.Store, total int, w io.Writer, tty, color bool) {
	if !tty {
		for _, s := range stores {
			fmt.Fprintf(w, "%s\t%d\t%s\n", s.Project, len(s.Memories), s.Dir)
		}
		return
	}
	fmt.Fprintf(w, "%s with memory · %d memories total\n\n",
		plural(len(stores), "project", "projects"), total)
	nameCol := 0
	for _, s := range stores {
		if n := len([]rune(s.Project)); n > nameCol {
			nameCol = n
		}
	}
	for _, s := range stores {
		note := ""
		if !s.HasIndex && len(s.Memories) > 0 {
			note = "   (no index)"
		}
		// Pad from the visible name: ANSI escapes are bytes but occupy no
		// columns, so %-*s on a painted string under-pads.
		pad := strings.Repeat(" ", nameCol-len([]rune(s.Project)))
		fmt.Fprintf(w, "  %s%s  %3d%s\n",
			term.Paint(color, term.Cyan, s.Project), pad, len(s.Memories), note)
	}
}
