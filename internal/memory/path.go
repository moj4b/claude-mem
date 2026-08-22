package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IsMemoryFile reports whether path is a memory belonging to dir: a direct
// child, ending in .md, and not the index.
//
// This restates what Load already filters (§I.2), on purpose. Load's filter
// decides what is *listed*; this decides what may be *unlinked*, and a
// destructive step must not depend on a distant caller having filtered
// correctly. The suffix and index tests mirror Load's exactly, so the two
// cannot drift into disagreeing about what a memory is.
func IsMemoryFile(dir, path string) bool {
	p := filepath.Clean(path)
	// Cleaned first, so any ".." in the path is resolved before the comparison
	// rather than after it.
	if filepath.Dir(p) != filepath.Clean(dir) {
		return false
	}
	base := filepath.Base(p)
	if base == IndexFile || !strings.HasSuffix(base, ".md") {
		return false
	}
	// A bare ".md" names no memory.
	return strings.TrimSuffix(base, ".md") != ""
}

// Removable is the check that stands between a resolved name and os.Remove:
// the path must be a memory of dir (IsMemoryFile) and must be a file on disk,
// never a directory. os.Remove deletes an empty directory as readily as a
// file, so the type test is not redundant with the name test.
//
// A symlink is removable and is removed as the link — os.Remove and Lstat both
// act on the link itself, so whatever it points at is never touched.
func Removable(dir, path string) error {
	if !IsMemoryFile(dir, path) {
		return fmt.Errorf("%s is not a memory of %s", filepath.Base(path), dir)
	}
	// Lstat, not Stat: a symlink must be judged as a link rather than as
	// whatever it resolves to.
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%s is not there", filepath.Base(path))
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory, not a memory", filepath.Base(path))
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s is not a regular file", filepath.Base(path))
	}
	return nil
}
