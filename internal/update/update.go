// Package update is the self-update path of §II.12: which release is current,
// whether the running binary is behind it, and replacing that binary with a
// verified download.
//
// It is the only package in the tool that touches the network, and nothing on
// the read path calls into it. §II.2's speed rule is a promise about `mem list`
// and TAB completion, and it is kept by construction: the answer a command
// shows comes from a cache file, and the fetch that fills that cache happens in
// a detached background process.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// The two endpoints are variables rather than constants for one reason: an
// acceptance test can then build a binary with
//
//	-ldflags "-X github.com/moj4b/claude-mem/internal/update.DefaultAPI=http://127.0.0.1:PORT/api"
//
// and drive a real upgrade, in a real process, against a locally served
// release. Unit tests can inject an Upgrader; only a stamped binary can prove
// the shipped path replaces itself. Nothing in the tool ever assigns to them.
var (
	// DefaultAPI is GitHub's latest-release feed for this repository. It reports
	// the newest *published, non-prerelease* release, which is exactly what
	// `install.sh` would fetch from /releases/latest/download.
	DefaultAPI = "https://api.github.com/repos/moj4b/claude-mem/releases/latest"

	// DefaultDownloads is the release asset base; a tag and an asset name are
	// appended. Assets are not version-stamped (see the note in release.yml), so
	// only the directory carries the tag.
	DefaultDownloads = "https://github.com/moj4b/claude-mem/releases/download"
)

const (
	// ReleasesPage is where a human goes when any of this fails.
	ReleasesPage = "https://github.com/moj4b/claude-mem/releases"

	// NoCheckEnv switches the update notice off entirely, for anyone whose
	// `mem` is managed by something other than itself. Every `mem` this package
	// starts inherits it set, so no child of an update ever checks for one.
	NoCheckEnv = "MEM_NO_UPDATE_CHECK"

	// Interval is how long a check is good for. A day is long enough that the
	// tool is not a GitHub client, and short enough that a release is noticed
	// the day after it lands.
	Interval = 24 * time.Hour

	userAgent = "mem-cli"
)

// State is the update-check cache: what the last check found, and when. The
// foreground never fetches, so this file is the whole basis of the notice a
// command prints.
type State struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"` // "" when the last check failed
}

// Fresh reports whether the last check is still within Interval. A timestamp in
// the future — a clock that moved backwards, a copied cache file — counts as
// stale rather than as freshness that never expires.
func (s State) Fresh(now time.Time) bool {
	if s.CheckedAt.IsZero() {
		return false
	}
	d := now.Sub(s.CheckedAt)
	return d >= 0 && d < Interval
}

// StatePath is the cache file. os.UserCacheDir honours XDG_CACHE_HOME on Linux
// and ~/Library/Caches on macOS, so this lands where each platform expects a
// disposable file: deleting it costs one check, nothing more.
func StatePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mem", "update-check.json"), nil
}

// ReadState never fails. A missing, unreadable or corrupt cache is simply no
// cache — the same degrade-don't-crash rule the memory parsers follow (§II.2).
func ReadState(path string) State {
	b, err := os.ReadFile(path)
	if err != nil {
		return State{}
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}
	}
	return s
}

// WriteState replaces the cache atomically, so a command that reads it while a
// background check is writing sees the old file or the new one, never half of
// either.
func WriteState(path string, s State) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".update-check-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // a no-op once the rename below has moved it
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Detach starts a command and walks away: no terminal, no wait, no exit status
// anyone will ever read. It is how the once-a-day check runs without the
// command that scheduled it paying for the fetch.
func Detach(exe string, arg ...string) error {
	c := exec.Command(exe, arg...)
	// nil streams are /dev/null: the child outlives its parent and must not
	// write onto the user's terminal.
	c.Stdin, c.Stdout, c.Stderr = nil, nil, nil
	c.Env = append(os.Environ(), NoCheckEnv+"=1")
	c.SysProcAttr = detachAttr()
	if err := c.Start(); err != nil {
		return err
	}
	return c.Process.Release()
}

// Latest asks the release feed for the newest published tag.
func Latest(ctx context.Context, c *http.Client, api string) (string, error) {
	body, err := get(ctx, c, api)
	if err != nil {
		return "", err
	}
	var feed struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &feed); err != nil {
		return "", fmt.Errorf("release feed is not JSON: %w", err)
	}
	if feed.TagName == "" {
		return "", errors.New("release feed names no tag")
	}
	return feed.TagName, nil
}

// maxDownload bounds every response. A release archive is about a megabyte;
// this is a sanity limit, not a budget.
const maxDownload = 128 << 20

func get(ctx context.Context, c *http.Client, url string) ([]byte, error) {
	if c == nil {
		c = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// GitHub rejects requests without a User-Agent.
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	if len(b) > maxDownload {
		return nil, fmt.Errorf("%s: response exceeds %d bytes", url, maxDownload)
	}
	return b, nil
}
