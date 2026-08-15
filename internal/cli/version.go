package cli

import (
	"runtime/debug"
	"strings"
)

// version is stamped at link time by the release build:
//
//	-ldflags "-X github.com/moj4b/claude-mem/internal/cli.version=v0.1.0"
//
// It is empty for a plain `go build` and for `go install`, which is why the
// fallback below exists: the Go toolchain already records the module version
// and the VCS commit in the binary, so an unstamped `mem --version` can still
// say which build it is rather than lying about a hardcoded number.
var version string

func versionString() string {
	if version != "" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(devel)"
	}
	// `go install ...@v0.1.0` records the tag here; a local build says "(devel)".
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}

	var rev, dirty string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
			if len(rev) > 12 {
				rev = rev[:12]
			}
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "-dirty"
			}
		}
	}
	if rev == "" {
		return "(devel)"
	}
	return strings.Join([]string{"(devel", rev + dirty + ")"}, " ")
}
