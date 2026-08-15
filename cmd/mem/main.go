// Command mem reads Claude Code's per-project memory.
package main

import (
	"os"

	"github.com/moj4b/claude-mem/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
