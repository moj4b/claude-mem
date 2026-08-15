package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	osexec "os/exec" // `exec` is this package's test helper for running the CLI
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moj4b/claude-mem/internal/update"
)

// A fake release, served locally: the tests below drive `mem upgrade` over the
// same path a real one takes — feed, archive, checksums, swap — with nothing
// stubbed out but the addresses.

// stubVersion makes the running binary claim to be v, the way a release build
// does through its linker flag.
func stubVersion(t *testing.T, v string) {
	t.Helper()
	prev := version
	version = v
	t.Cleanup(func() { version = prev })
}

// stubStatePath moves the update-check cache into the test's own directory, so
// no test ever reads or writes the user's real one.
func stubStatePath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "update-check.json")
	prev := updateStatePath
	updateStatePath = func() (string, error) { return path, nil }
	t.Cleanup(func() { updateStatePath = prev })
	return path
}

// release is a served fake release plus the binary it would replace.
type release struct {
	target    string // the `mem` on disk that `upgrade` will overwrite
	downloads atomic.Int32
}

// memScript stands in for the release binary: a real executable that answers
// `--version` the way the release smoke test requires.
func memScript(v string) []byte { return []byte("#!/bin/sh\necho \"mem " + v + "\"\n") }

// serveRelease publishes tag for this platform and points the upgrade path at
// it. installed is the version already on disk.
func serveRelease(t *testing.T, tag, installed string) *release {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no release is published for Windows")
	}
	stubStatePath(t)

	rel := &release{target: filepath.Join(t.TempDir(), "mem")}
	if err := os.WriteFile(rel.target, memScript(installed), 0o755); err != nil {
		t.Fatal(err)
	}

	asset := fmt.Sprintf("mem_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archive := tarGz(t, map[string][]byte{"mem": memScript(tag), "LICENSE": []byte("MIT")})
	sum := sha256.Sum256(archive)

	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q}`, tag)
	})
	mux.HandleFunc("/download/"+tag+"/"+asset, func(w http.ResponseWriter, r *http.Request) {
		rel.downloads.Add(1)
		w.Write(archive)
	})
	mux.HandleFunc("/download/"+tag+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	pointUpgraderAt(t, srv, rel.target)
	return rel
}

// pointUpgraderAt redirects both endpoints and the binary to replace.
func pointUpgraderAt(t *testing.T, srv *httptest.Server, target string) {
	t.Helper()
	prev := newUpgrader
	newUpgrader = func(current string, progress io.Writer) (update.Upgrader, error) {
		return update.Upgrader{
			Current: current, ExecPath: target,
			GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
			API: srv.URL + "/api", Downloads: srv.URL + "/download",
			Client: srv.Client(),
			Note:   func(s string) { fmt.Fprintln(progress, s) },
		}, nil
	}
	t.Cleanup(func() { newUpgrader = prev })
}

func tarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// installedVersion runs the binary on disk, which is the only honest way to ask
// what `upgrade` actually left there.
func installedVersion(t *testing.T, path string) string {
	t.Helper()
	out, err := osexec.Command(path, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("%s --version: %v (%s)", path, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestUpgradeInstallsTheLatestRelease(t *testing.T) {
	stubVersion(t, "v0.1.0")
	rel := serveRelease(t, "v0.2.0", "v0.1.0")

	out, errOut, code := exec("upgrade")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut)
	}
	if !strings.Contains(out, "upgraded to mem v0.2.0") {
		t.Errorf("stdout = %q, want it to report the new version", out)
	}
	if !strings.Contains(out, rel.target) {
		t.Errorf("stdout = %q, want it to name what was replaced", out)
	}
	// Progress is diagnostics, so it belongs on stderr (§II.2).
	if !strings.Contains(errOut, "checksum ok") {
		t.Errorf("stderr = %q, want the progress notes", errOut)
	}
	if got := installedVersion(t, rel.target); got != "mem v0.2.0" {
		t.Errorf("installed binary = %q, want mem v0.2.0", got)
	}
}

func TestUpgradeIsAlsoSpelledUpdate(t *testing.T) {
	stubVersion(t, "v0.1.0")
	rel := serveRelease(t, "v0.2.0", "v0.1.0")

	if _, errOut, code := exec("update"); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut)
	}
	if got := installedVersion(t, rel.target); got != "mem v0.2.0" {
		t.Errorf("installed binary = %q, want mem v0.2.0", got)
	}
}

func TestUpgradeStopsWhenAlreadyCurrent(t *testing.T) {
	stubVersion(t, "v0.2.0")
	rel := serveRelease(t, "v0.2.0", "v0.2.0")

	out, _, code := exec("upgrade")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "already the latest release") {
		t.Errorf("stdout = %q, want it to say there is nothing to do", out)
	}
	if n := rel.downloads.Load(); n != 0 {
		t.Errorf("downloaded the archive %d times, want 0 — there was nothing to install", n)
	}
}

func TestUpgradeWillNotDowngrade(t *testing.T) {
	// A build from a tag that is not published yet must not be walked backwards.
	stubVersion(t, "v0.3.0")
	rel := serveRelease(t, "v0.2.0", "v0.3.0")

	out, _, code := exec("upgrade")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "newer than the latest release") {
		t.Errorf("stdout = %q, want it to say the build is ahead of the feed", out)
	}
	if n := rel.downloads.Load(); n != 0 {
		t.Errorf("downloaded the archive %d times, want 0", n)
	}
}

func TestUpgradeCheckInstallsNothing(t *testing.T) {
	stubVersion(t, "v0.1.0")
	rel := serveRelease(t, "v0.2.0", "v0.1.0")

	out, _, code := exec("upgrade", "--check")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	for _, want := range []string{"v0.2.0", "v0.1.0", "mem upgrade"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout = %q, want it to mention %q", out, want)
		}
	}
	if n := rel.downloads.Load(); n != 0 {
		t.Errorf("downloaded the archive %d times, want 0 — --check installs nothing", n)
	}
	if got := installedVersion(t, rel.target); got != "mem v0.1.0" {
		t.Errorf("installed binary = %q, want it untouched", got)
	}
}

func TestUpgradeCheckOnACurrentBuild(t *testing.T) {
	stubVersion(t, "v0.2.0")
	serveRelease(t, "v0.2.0", "v0.2.0")

	out, _, code := exec("upgrade", "--check")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "mem v0.2.0 is the latest release") {
		t.Errorf("stdout = %q, want the up-to-date answer", out)
	}
}

func TestUpgradeChecksTheFeedForAnUnstampedBuild(t *testing.T) {
	// `go build` produces no version. There is nothing to compare, but the user
	// still asked to be on the latest release — so install it and say why.
	stubVersion(t, "(devel)")
	rel := serveRelease(t, "v0.2.0", "v0.1.0")

	out, errOut, code := exec("upgrade")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut)
	}
	if !strings.Contains(errOut, "nothing to compare") {
		t.Errorf("stderr = %q, want it to explain the unstamped build", errOut)
	}
	if !strings.Contains(out, "upgraded to mem v0.2.0") {
		t.Errorf("stdout = %q, want the install to have happened anyway", out)
	}
	if got := installedVersion(t, rel.target); got != "mem v0.2.0" {
		t.Errorf("installed binary = %q, want mem v0.2.0", got)
	}
}

func TestUpgradeFailsWhenTheFeedIsUnreachable(t *testing.T) {
	stubVersion(t, "v0.1.0")
	stubStatePath(t)
	target := filepath.Join(t.TempDir(), "mem")
	if err := os.WriteFile(target, memScript("v0.1.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	pointUpgraderAt(t, srv, target)

	out, errOut, code := exec("upgrade")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty — a failed upgrade has no payload", out)
	}
	if !strings.Contains(errOut, "could not reach the release feed") {
		t.Errorf("stderr = %q, want it to name the problem", errOut)
	}
	if !strings.Contains(errOut, update.ReleasesPage) {
		t.Errorf("stderr = %q, want it to point at the releases page", errOut)
	}
	if got := installedVersion(t, target); got != "mem v0.1.0" {
		t.Errorf("installed binary = %q, want it untouched", got)
	}
}

func TestUpgradeRecordsWhatItLearned(t *testing.T) {
	// Asking by hand is a check: it must reset the notice's clock, so the next
	// command neither repeats stale news nor starts a background fetch.
	stubVersion(t, "v0.1.0")
	serveRelease(t, "v0.2.0", "v0.1.0")
	path, _ := updateStatePath()

	if _, errOut, code := exec("upgrade", "--check"); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut)
	}
	st := update.ReadState(path)
	if st.Latest != "v0.2.0" {
		t.Errorf("cached latest = %q, want v0.2.0", st.Latest)
	}
	if !st.Fresh(time.Now()) {
		t.Error("cache is stale right after a check")
	}
}

func TestUpdateCheckWritesTheCacheSilently(t *testing.T) {
	path := stubStatePath(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
	}))
	defer srv.Close()
	prev := updateAPI
	updateAPI = srv.URL
	t.Cleanup(func() { updateAPI = prev })

	out, errOut, code := exec(updateCheckCmd)
	if code != 0 || out != "" || errOut != "" {
		t.Errorf("__update-check wrote %q/%q and exited %d; it runs detached and must be silent", out, errOut, code)
	}
	if got := update.ReadState(path); got.Latest != "v9.9.9" || !got.Fresh(time.Now()) {
		t.Errorf("cache = %+v, want a fresh v9.9.9", got)
	}
}

func TestUpdateCheckBacksOffButRemembersWhenTheFeedFails(t *testing.T) {
	path := stubStatePath(t)
	if err := update.WriteState(path, update.State{
		CheckedAt: time.Now().Add(-48 * time.Hour), Latest: "v0.2.0",
	}); err != nil {
		t.Fatal(err)
	}
	prev := updateAPI
	updateAPI = "http://127.0.0.1:1/nothing-listens-here"
	t.Cleanup(func() { updateAPI = prev })

	if _, _, code := exec(updateCheckCmd); code != 0 {
		t.Errorf("exit code = %d, want 0 — a failed check is not an error anyone reads", code)
	}
	got := update.ReadState(path)
	if !got.Fresh(time.Now()) {
		t.Error("a failed check left the cache stale; every command would spawn another")
	}
	if got.Latest != "v0.2.0" {
		t.Errorf("cached latest = %q, want the last known v0.2.0 kept", got.Latest)
	}
}

func TestUpdateNoticeLine(t *testing.T) {
	got := updateNotice("v0.1.0", "v0.2.0", false)
	for _, want := range []string{"v0.2.0", "v0.1.0", "mem upgrade"} {
		if !strings.Contains(got, want) {
			t.Errorf("notice = %q, want it to mention %q", got, want)
		}
	}
	if strings.Contains(got, "\n") {
		t.Errorf("notice = %q, want a single line — it prints after every command", got)
	}
	for _, tc := range [][2]string{
		{"v0.2.0", "v0.2.0"}, // current
		{"v0.2.0", "v0.1.0"}, // ahead of the feed
		{"(devel)", "v0.2.0"},
		{"v0.1.0", ""}, // never checked
	} {
		if got := updateNotice(tc[0], tc[1], false); got != "" {
			t.Errorf("updateNotice(%q, %q) = %q, want silence", tc[0], tc[1], got)
		}
	}
}

func TestNotifyAllowed(t *testing.T) {
	stubVersion(t, "v0.1.0")
	for _, tc := range []struct {
		cmd  string
		tty  bool
		want bool
	}{
		{"list", true, true},
		{"read", true, true},
		{"--version", true, true},
		{"--help", true, true},
		// A pipe, a script, or CI: the payload and nothing else (§II.2).
		{"list", false, false},
		// TAB completion must never write anything but candidates (§II.10).
		{"__complete", true, false},
		// `completion`'s output is eval'd from a shell rc file.
		{"completion", true, false},
		// These say their own piece, from a fresher answer than the cache.
		{"upgrade", true, false},
		{"update", true, false},
		{updateCheckCmd, true, false},
	} {
		if got := notifyAllowed(tc.cmd, tc.tty); got != tc.want {
			t.Errorf("notifyAllowed(%q, tty=%v) = %v, want %v", tc.cmd, tc.tty, got, tc.want)
		}
	}

	t.Run("opted out", func(t *testing.T) {
		t.Setenv(update.NoCheckEnv, "1")
		if notifyAllowed("list", true) {
			t.Errorf("%s is set but the notice still speaks", update.NoCheckEnv)
		}
	})

	t.Run("unstamped build", func(t *testing.T) {
		// A local `go build` has no place in the release order, so there is
		// nothing truthful to say about it.
		stubVersion(t, "(devel 4d1a2b3c5e6f)")
		if notifyAllowed("list", true) {
			t.Error("a devel build was told it is out of date")
		}
	})
}

func TestNoNoticeOnANonTerminal(t *testing.T) {
	// The seam every other test relies on: Run writes to buffers, so nothing in
	// the suite can print a notice or spawn a background check.
	stubVersion(t, "v0.1.0")
	path := stubStatePath(t)
	if err := update.WriteState(path, update.State{CheckedAt: time.Now(), Latest: "v9.9.9"}); err != nil {
		t.Fatal(err)
	}
	_, errOut, _ := exec("--version")
	if strings.Contains(errOut, "v9.9.9") {
		t.Errorf("stderr = %q, want no notice when stderr is not a terminal", errOut)
	}
}

func TestUsageDocumentsUpgrade(t *testing.T) {
	out, _, code := exec("--help")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, want := range []string{"mem upgrade", "--check"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage does not mention %q:\n%s", want, out)
		}
	}
}

func TestCompleteOffersUpgradeFlags(t *testing.T) {
	got := complete(t, "upgrade", "")
	if len(got) != 1 || got[0] != "--check" {
		t.Errorf("complete(upgrade) = %v, want [--check]", got)
	}
}
