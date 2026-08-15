package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLatestReadsTheTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent; GitHub rejects those")
		}
		// The real feed carries far more than this; the parse must ignore it.
		w.Write([]byte(`{"url":"...","tag_name":"v9.9.9","name":"v9.9.9","assets":[{"name":"checksums.txt"}]}`))
	}))
	defer srv.Close()

	got, err := Latest(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got != "v9.9.9" {
		t.Errorf("Latest = %q, want v9.9.9", got)
	}
}

func TestLatestFailsLoudly(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"not found":  func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) },
		"rate limit": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) },
		"not json":   func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>nope</html>")) },
		"no tag":     func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"name":"v1.0.0"}`)) },
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			if got, err := Latest(context.Background(), srv.Client(), srv.URL); err == nil {
				t.Errorf("Latest = %q, want an error", got)
			}
		})
	}
}

func TestLatestHonoursTheContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Latest(ctx, srv.Client(), srv.URL); err == nil {
		t.Error("a hung feed returned no error; the background check would never exit")
	}
}

func TestStateRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "update-check.json")
	want := State{CheckedAt: time.Now().Truncate(time.Second), Latest: "v0.2.0"}
	if err := WriteState(path, want); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	got := ReadState(path)
	if got.Latest != want.Latest || !got.CheckedAt.Equal(want.CheckedAt) {
		t.Errorf("ReadState = %+v, want %+v", got, want)
	}
}

func TestReadStateDegradesInsteadOfFailing(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.json")
	if got := ReadState(missing); got != (State{}) {
		t.Errorf("missing cache = %+v, want the zero State", got)
	}

	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ReadState(corrupt); got != (State{}) {
		t.Errorf("corrupt cache = %+v, want the zero State", got)
	}
}

func TestStateFresh(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name  string
		at    time.Time
		fresh bool
	}{
		{"just checked", now, true},
		{"an hour ago", now.Add(-time.Hour), true},
		{"a day and a minute ago", now.Add(-Interval - time.Minute), false},
		{"never checked", time.Time{}, false},
		// A timestamp from the future is a clock that moved, or a copied cache
		// file. Treating it as fresh would silence the notice forever.
		{"tomorrow", now.Add(time.Hour), false},
	} {
		if got := (State{CheckedAt: tc.at}).Fresh(now); got != tc.fresh {
			t.Errorf("%s: Fresh = %v, want %v", tc.name, got, tc.fresh)
		}
	}
}

func TestStatePathIsUnderTheCacheDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("HOME", dir) // macOS ignores XDG and uses ~/Library/Caches

	got, err := StatePath()
	if err != nil {
		t.Fatalf("StatePath: %v", err)
	}
	if !strings.HasPrefix(got, dir) {
		t.Errorf("StatePath = %q, want it under the cache dir %q", got, dir)
	}
	if filepath.Base(got) != "update-check.json" || filepath.Base(filepath.Dir(got)) != "mem" {
		t.Errorf("StatePath = %q, want .../mem/update-check.json", got)
	}
}

func TestDetach(t *testing.T) {
	if err := Detach("/nonexistent/mem", "__update-check"); err == nil {
		t.Error("Detach on a missing binary returned no error")
	}
	// A real detached process: it must start and be released, not waited on.
	true_, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no `true` on PATH")
	}
	if err := Detach(true_); err != nil {
		t.Errorf("Detach: %v", err)
	}
}
