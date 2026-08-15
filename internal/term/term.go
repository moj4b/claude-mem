// Package term holds the terminal-awareness helpers of §II.2: output is
// decorated only when stdout is a terminal, and never when NO_COLOR is set.
package term

import (
	"io"
	"os"
	"strconv"
)

const defaultWidth = 80

// ANSI escapes used by the formatters.
const (
	Reset  = "\x1b[0m"
	Dim    = "\x1b[2m"
	Bold   = "\x1b[1m"
	Cyan   = "\x1b[36m"
	Yellow = "\x1b[33m"
)

// IsTTY reports whether w is a character device. Output is decorated only when
// it is (§II.2), so `mem read x > x.md` gets bytes and a human gets colour.
func IsTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// Width reads COLUMNS, which is not exported to non-interactive shells (§I.8),
// then falls back to 80.
func Width() int {
	if n, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && n > 0 {
		return n
	}
	return defaultWidth
}

// UseColor honours NO_COLOR with any non-empty value (§II.2).
func UseColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return IsTTY(w)
}

// Paint wraps s in an ANSI code when on is true, so callers stay branch-free.
func Paint(on bool, code, s string) string {
	if !on || s == "" {
		return s
	}
	return code + s + Reset
}

// Truncate clips s to max runes, replacing the tail with an ellipsis. Counts
// runes because descriptions contain non-ASCII (§I.4).
func Truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
