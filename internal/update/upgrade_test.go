package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A release is a tar.gz per platform plus a checksums.txt, exactly as
// release.yml publishes them. These helpers build one so the tests can drive
// the real download-verify-swap path end to end, against a local server.

func targz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, body := range files {
		mode := int64(0o644)
		if name == "mem" {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg,
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

// script is a stand-in for the release binary: it is a real executable that
// answers `--version` the way the release smoke test requires.
func script(version string) []byte {
	return []byte("#!/bin/sh\necho \"mem " + version + "\"\n")
}

// checksums renders the sha256sum format release.yml produces: two spaces
// between the hash and the name.
func checksums(assets map[string][]byte) []byte {
	var b strings.Builder
	for name, body := range assets {
		sum := sha256.Sum256(body)
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	return []byte(b.String())
}

// releaseServer serves the feed at /api and assets at /download/<tag>/<name>.
// Anything not registered 404s, which is what a missing asset really does.
func releaseServer(t *testing.T, tag string, assets map[string][]byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q}`, tag)
	})
	for name, body := range assets {
		body := body
		mux.HandleFunc("/download/"+tag+"/"+name, func(w http.ResponseWriter, r *http.Request) {
			w.Write(body)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// installed sets up an existing `mem` on disk and an Upgrader pointed at it and
// at a served release. tarFiles is what the archive holds; corrupt swaps the
// published checksum for a wrong one.
func installed(t *testing.T, tag string, tarFiles map[string][]byte, corrupt bool) (Upgrader, string, *httptest.Server) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no release is published for Windows")
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "mem")
	if err := os.WriteFile(target, script("v0.1.0"), 0o755); err != nil {
		t.Fatal(err)
	}

	asset := fmt.Sprintf("mem_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archive := targz(t, tarFiles)
	assets := map[string][]byte{asset: archive}
	sums := checksums(assets)
	if corrupt {
		sums = checksums(map[string][]byte{asset: []byte("different bytes entirely")})
	}
	assets["checksums.txt"] = sums

	srv := releaseServer(t, tag, assets)
	u := Upgrader{
		Current:   "v0.1.0",
		ExecPath:  target,
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
		API:       srv.URL + "/api",
		Downloads: srv.URL + "/download",
		Client:    srv.Client(),
	}
	return u, target, srv
}

// runVersion executes an installed binary the way a user would.
func runVersion(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("%s --version: %v (%s)", path, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestInstallReplacesTheBinary(t *testing.T) {
	files := map[string][]byte{"mem": script("v0.2.0"), "LICENSE": []byte("MIT"), "README.md": []byte("# mem")}
	u, target, _ := installed(t, "v0.2.0", files, false)

	if got := runVersion(t, target); got != "mem v0.1.0" {
		t.Fatalf("before: %q, want mem v0.1.0", got)
	}
	if err := u.Install(context.Background(), "v0.2.0"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got := runVersion(t, target); got != "mem v0.2.0" {
		t.Errorf("after: %q, want mem v0.2.0", got)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755 — the replacement must stay executable", info.Mode().Perm())
	}
	// Only the binary is installed: nothing else from the archive is written.
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "mem" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("install directory holds %v, want just [mem] — no leftovers, no LICENSE", names)
	}
}

// Every failure mode shares one requirement: the working binary already on
// disk survives untouched. There is no second chance, because the tool that
// would fix a broken install is the one being replaced.
func TestInstallNeverLeavesABrokenBinary(t *testing.T) {
	good := script("v0.2.0")
	for _, tc := range []struct {
		name    string
		files   map[string][]byte
		corrupt bool
		tag     string
		want    string
	}{
		{
			name: "checksum mismatch", files: map[string][]byte{"mem": good},
			corrupt: true, tag: "v0.2.0", want: "checksum mismatch",
		},
		{
			name: "archive holds no binary", files: map[string][]byte{"README.md": []byte("# mem")},
			tag: "v0.2.0", want: `holds no "mem"`,
		},
		{
			name: "binary does not run", files: map[string][]byte{"mem": []byte("\x00not an executable")},
			tag: "v0.2.0", want: "does not run",
		},
		{
			name: "binary is the wrong version", files: map[string][]byte{"mem": script("v0.1.5")},
			tag: "v0.2.0", want: "does not mention v0.2.0",
		},
		{
			name: "empty binary", files: map[string][]byte{"mem": {}},
			tag: "v0.2.0", want: "empty",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, target, _ := installed(t, tc.tag, tc.files, tc.corrupt)
			err := u.Install(context.Background(), tc.tag)
			if err == nil {
				t.Fatal("Install succeeded, want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
			if got := runVersion(t, target); got != "mem v0.1.0" {
				t.Errorf("installed binary = %q, want the original mem v0.1.0", got)
			}
			// A half-written temporary must never be left behind either.
			entries, err := os.ReadDir(filepath.Dir(target))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Errorf("install directory holds %d entries, want only the original binary", len(entries))
			}
		})
	}
}

func TestInstallRefusesWithoutChecksums(t *testing.T) {
	// Unlike install.sh, which warns and carries on under a human's eye, an
	// unattended self-replacement must stop.
	u, target, _ := installed(t, "v0.2.0", map[string][]byte{"mem": script("v0.2.0")}, false)
	u.Downloads = u.Downloads + "/nowhere" // checksums.txt and the asset both 404

	err := u.Install(context.Background(), "v0.2.0")
	if err == nil {
		t.Fatal("Install succeeded, want an error")
	}
	if got := runVersion(t, target); got != "mem v0.1.0" {
		t.Errorf("installed binary = %q, want the original", got)
	}
}

func TestInstallRefusesAnUnsupportedPlatform(t *testing.T) {
	u := Upgrader{GOOS: "windows", GOARCH: "amd64", Current: "v0.1.0"}
	if u.Supported() {
		t.Fatal("windows/amd64 reported as supported")
	}
	err := u.Install(context.Background(), "v0.2.0")
	if err == nil || !strings.Contains(err.Error(), "windows/amd64") {
		t.Errorf("error = %v, want it to name the platform", err)
	}
}

func TestSupportedMatchesTheReleaseMatrix(t *testing.T) {
	for _, p := range []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64"} {
		os, arch, _ := strings.Cut(p, "/")
		if !(Upgrader{GOOS: os, GOARCH: arch}).Supported() {
			t.Errorf("%s is built by release.yml but reported unsupported", p)
		}
	}
	for _, p := range []string{"windows/amd64", "linux/386", "freebsd/amd64", "darwin/ppc64"} {
		os, arch, _ := strings.Cut(p, "/")
		if (Upgrader{GOOS: os, GOARCH: arch}).Supported() {
			t.Errorf("%s is not released but reported supported", p)
		}
	}
}

func TestAssetNameMatchesTheReleaseWorkflow(t *testing.T) {
	got := (Upgrader{GOOS: "linux", GOARCH: "amd64"}).Asset()
	if got != "mem_linux_amd64.tar.gz" {
		t.Errorf("Asset = %q, want mem_linux_amd64.tar.gz", got)
	}
}

func TestWantedSum(t *testing.T) {
	// The exact bytes published for v0.1.0 — the format this must keep reading.
	real := []byte(`0556334ca1830b30cf17534b0c957a3afb5c97c13c54dda03c9ba02a55376264  mem_darwin_amd64.tar.gz
ca355da8f59c56b7d8d1f79cec6725988e97c5db1385535a46e27fdceffce88e  mem_darwin_arm64.tar.gz
02ea7bb044d2b7c914d0a67ad05cfb63b09f038c4a1dca29918fc5acd6c28ec1  mem_linux_amd64.tar.gz
4a5b35061936a818e2e14af03e24b5241644262f243f487839b9324e4285eb57  mem_linux_arm64.tar.gz
`)
	got, err := wantedSum(real, "mem_linux_amd64.tar.gz")
	if err != nil {
		t.Fatalf("wantedSum: %v", err)
	}
	if got != "02ea7bb044d2b7c914d0a67ad05cfb63b09f038c4a1dca29918fc5acd6c28ec1" {
		t.Errorf("wantedSum = %q, want the linux/amd64 line", got)
	}

	// The `./` and `*` prefixes other checksum tools emit are read too.
	for _, line := range []string{"abc123 ./mem_linux_amd64.tar.gz", "abc123 *mem_linux_amd64.tar.gz"} {
		if got, err := wantedSum([]byte(line), "mem_linux_amd64.tar.gz"); err != nil || got != "abc123" {
			t.Errorf("wantedSum(%q) = %q, %v", line, got, err)
		}
	}

	// An asset that is not listed must be an error, never a skipped check.
	if _, err := wantedSum(real, "mem_linux_riscv64.tar.gz"); err == nil {
		t.Error("an unlisted asset returned no error — verification would be skipped")
	}
	if _, err := wantedSum(nil, "mem_linux_amd64.tar.gz"); err == nil {
		t.Error("an empty checksums.txt returned no error")
	}
}

func TestExtract(t *testing.T) {
	archive := targz(t, map[string][]byte{"mem": []byte("binary"), "LICENSE": []byte("MIT")})
	got, err := extract(archive, "mem")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if string(got) != "binary" {
		t.Errorf("extract = %q, want the binary", got)
	}
	if _, err := extract(archive, "nope"); err == nil {
		t.Error("extract of a missing member returned no error")
	}
	if _, err := extract([]byte("not a gzip stream"), "mem"); err == nil {
		t.Error("extract of a non-archive returned no error")
	}
}

func TestInstallReportsProgress(t *testing.T) {
	u, _, _ := installed(t, "v0.2.0", map[string][]byte{"mem": script("v0.2.0")}, false)
	var notes []string
	u.Note = func(s string) { notes = append(notes, s) }

	if err := u.Install(context.Background(), "v0.2.0"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	joined := strings.Join(notes, "\n")
	for _, want := range []string{u.Asset(), "checksum ok"} {
		if !strings.Contains(joined, want) {
			t.Errorf("progress = %q, want it to mention %q", joined, want)
		}
	}
}

func TestLatestThroughTheUpgrader(t *testing.T) {
	u, _, _ := installed(t, "v0.3.0", map[string][]byte{"mem": script("v0.3.0")}, false)
	got, err := u.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got != "v0.3.0" {
		t.Errorf("Latest = %q, want v0.3.0", got)
	}
}

func TestNewPointsAtTheRunningBinary(t *testing.T) {
	u, err := New("v0.1.0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if u.ExecPath == "" || !filepath.IsAbs(u.ExecPath) {
		t.Errorf("ExecPath = %q, want an absolute path", u.ExecPath)
	}
	if u.GOOS != runtime.GOOS || u.GOARCH != runtime.GOARCH {
		t.Errorf("platform = %s/%s, want %s/%s", u.GOOS, u.GOARCH, runtime.GOOS, runtime.GOARCH)
	}
	if u.API != DefaultAPI || u.Downloads != DefaultDownloads {
		t.Error("New did not default to the public release endpoints")
	}
}
