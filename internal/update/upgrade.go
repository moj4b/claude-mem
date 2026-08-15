package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Upgrader replaces the running binary with a release build. Every field the
// network and the filesystem are reached through is here rather than read from
// the ambient environment, which is what lets the whole path — feed, download,
// checksum, extract, swap — run against a local server in a test.
type Upgrader struct {
	Current   string // version of the running binary
	ExecPath  string // the file to replace
	GOOS      string
	GOARCH    string
	API       string // latest-release feed
	Downloads string // release asset base; tag and asset name are appended
	Client    *http.Client
	Note      func(string) // progress, one line at a time; nil is silent
}

// New builds the Upgrader for this running binary: this platform, this
// executable, the public release feed.
func New(current string) (Upgrader, error) {
	exe, err := os.Executable()
	if err != nil {
		return Upgrader{}, fmt.Errorf("cannot locate the running binary: %w", err)
	}
	// Resolve symlinks so a link farm (~/.local/bin/mem -> /opt/mem/bin/mem) has
	// the real file replaced, rather than the link overwritten with a copy.
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return Upgrader{
		Current:   current,
		ExecPath:  exe,
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
		API:       DefaultAPI,
		Downloads: DefaultDownloads,
	}, nil
}

// verifyTimeout bounds the `--version` run of the downloaded binary.
const verifyTimeout = 20 * time.Second

// Asset is the release archive for this platform. The name is the release
// workflow's, and the two must stay in step.
func (u Upgrader) Asset() string {
	return fmt.Sprintf("mem_%s_%s.tar.gz", u.GOOS, u.GOARCH)
}

// Supported reports whether a release is published for this platform. The set
// is release.yml's build matrix; Windows is deliberately absent (see the note
// in ci.yml).
func (u Upgrader) Supported() bool {
	switch u.GOOS + "/" + u.GOARCH {
	case "linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64":
		return true
	}
	return false
}

// Latest asks the release feed for the newest published tag.
func (u Upgrader) Latest(ctx context.Context) (string, error) {
	return Latest(ctx, u.Client, u.API)
}

func (u Upgrader) note(format string, a ...any) {
	if u.Note != nil {
		u.Note(fmt.Sprintf(format, a...))
	}
}

// Install downloads the release archive for tag, checks it against the
// release's checksums.txt, and replaces ExecPath with the binary inside it.
//
// Nothing is written until every check has passed, and the last check is
// running the downloaded binary. There is no second chance here: the tool that
// would fix a bad install is the one being replaced.
func (u Upgrader) Install(ctx context.Context, tag string) error {
	if !u.Supported() {
		return fmt.Errorf("no release is published for %s/%s\n"+
			"  mem is released for linux and darwin on amd64 and arm64\n"+
			"  build from source: go install github.com/moj4b/claude-mem/cmd/mem@latest",
			u.GOOS, u.GOARCH)
	}
	base := strings.TrimSuffix(u.Downloads, "/") + "/" + tag
	asset := u.Asset()

	u.note("downloading %s", asset)
	archive, err := get(ctx, u.Client, base+"/"+asset)
	if err != nil {
		return fmt.Errorf("could not download %s: %w\n  see %s for what is published", asset, err, ReleasesPage)
	}

	// Unlike install.sh, a missing checksums.txt is fatal rather than a warning.
	// A shell script running under a human's eye can degrade; a binary
	// overwriting itself unattended cannot.
	sums, err := get(ctx, u.Client, base+"/checksums.txt")
	if err != nil {
		return fmt.Errorf("could not download checksums.txt: %w\n  refusing to install an unverified binary", err)
	}
	want, err := wantedSum(sums, asset)
	if err != nil {
		return err
	}
	got := hex.EncodeToString(sha256Sum(archive))
	if got != want {
		return fmt.Errorf("checksum mismatch for %s\n  expected %s\n  got      %s", asset, want, got)
	}
	u.note("checksum ok")

	bin, err := extract(archive, "mem")
	if err != nil {
		return err
	}
	return u.replace(bin, tag)
}

func sha256Sum(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

// wantedSum finds asset's line in a sha256sum-format checksums.txt. The shipped
// format is `<hex>  <name>`, but the `./` and `*` prefixes other tools emit are
// tolerated: a change in how the release is built must fail loudly here, never
// silently skip verification.
func wantedSum(checksums []byte, asset string) (string, error) {
	for _, line := range strings.Split(string(checksums), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		name := strings.TrimPrefix(strings.TrimPrefix(f[len(f)-1], "*"), "./")
		if name == asset {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("%s is not listed in checksums.txt", asset)
}

// extract pulls one regular file out of a .tar.gz by name. Nothing is written
// to disk here and no path from the archive is ever used as a path, so a
// hostile member name has nowhere to go.
func extract(archive []byte, name string) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("release archive is not gzip: %w", err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("release archive holds no %q", name)
		}
		if err != nil {
			return nil, fmt.Errorf("release archive is not readable: %w", err)
		}
		if h.Typeflag != tar.TypeReg || strings.TrimPrefix(h.Name, "./") != name {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(tr, maxDownload+1))
		if err != nil {
			return nil, fmt.Errorf("release archive is not readable: %w", err)
		}
		if len(b) == 0 {
			return nil, fmt.Errorf("release archive holds an empty %q", name)
		}
		if len(b) > maxDownload {
			return nil, fmt.Errorf("%q in the release archive exceeds %d bytes", name, maxDownload)
		}
		return b, nil
	}
}

// replace writes bin beside the target and renames it over the top. The
// temporary lives in the target's own directory so the rename is within one
// filesystem, and the rename is what makes the swap atomic: a `mem` running
// from this path keeps its own inode, and no window exists where the file on
// disk is half-written.
func (u Upgrader) replace(bin []byte, tag string) error {
	target := u.ExecPath
	dir := filepath.Dir(target)

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".new-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w\n%s", dir, err, elsewhereHint(target))
	}
	name := tmp.Name()
	defer os.Remove(name) // a no-op once the rename below has moved it

	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return fmt.Errorf("cannot write %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot write %s: %w", name, err)
	}
	// Match the mode install.sh installs with, before running it below.
	if err := os.Chmod(name, 0o755); err != nil {
		return fmt.Errorf("cannot make %s executable: %w", name, err)
	}
	if err := verifyBinary(name, tag); err != nil {
		return err
	}
	if err := os.Rename(name, target); err != nil {
		return fmt.Errorf("cannot replace %s: %w\n%s", target, err, elsewhereHint(target))
	}
	return nil
}

// elsewhereHint is the two ways out of an unwritable install location.
func elsewhereHint(target string) string {
	return "  re-run with sudo, or install somewhere you own:\n" +
		"    MEM_INSTALL_DIR=$HOME/.local/bin curl -fsSL https://raw.githubusercontent.com/moj4b/claude-mem/main/install.sh | sh\n" +
		"  (current location: " + target + ")"
}

// verifyBinary runs the downloaded binary before it replaces anything. A
// truncated download, or an archive built for the wrong platform, would
// otherwise pass every check above — the checksum only proves the bytes are the
// ones published — and leave the user with no working `mem` to fix it with.
//
// The assertion is deliberately loose: `--version` must succeed and must
// mention the tag. Anything tighter would let an old binary dictate the output
// format of every future release.
func verifyBinary(path, tag string) error {
	ctx, cancel := context.WithTimeout(context.Background(), verifyTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	// The child is a `mem` too: keep it from checking for updates of its own.
	cmd.Env = append(os.Environ(), NoCheckEnv+"=1")
	out, err := cmd.CombinedOutput()
	got := strings.TrimSpace(string(out))
	if err != nil {
		return fmt.Errorf("the downloaded binary does not run: %w\n  it said: %s\n  nothing was replaced", err, got)
	}
	if !strings.Contains(got, tag) {
		return fmt.Errorf("the downloaded binary reports %q, which does not mention %s\n  nothing was replaced", got, tag)
	}
	return nil
}
