package term

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestIsTTYFalseForBuffersAndRegularFiles(t *testing.T) {
	if IsTTY(&bytes.Buffer{}) {
		t.Error("IsTTY(buffer) = true, want false")
	}
	f, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if IsTTY(f) {
		t.Error("IsTTY(regular file) = true, want false")
	}
}

func TestWidthPrefersCOLUMNSThenFallsBackTo80(t *testing.T) {
	t.Setenv("COLUMNS", "120")
	if got := Width(); got != 120 {
		t.Errorf("Width() with COLUMNS=120 = %d, want 120", got)
	}
	t.Setenv("COLUMNS", "")
	if got := Width(); got != 80 {
		t.Errorf("Width() with COLUMNS unset = %d, want 80", got)
	}
	t.Setenv("COLUMNS", "not-a-number")
	if got := Width(); got != 80 {
		t.Errorf("Width() with junk COLUMNS = %d, want 80", got)
	}
}

func TestNoColorDisablesColour(t *testing.T) {
	// Any non-empty NO_COLOR disables colour (§II.2), even on a TTY.
	t.Setenv("NO_COLOR", "1")
	if UseColor(&bytes.Buffer{}) {
		t.Error("useColor with NO_COLOR=1 = true, want false")
	}
}

func TestColourOffWhenNotATTY(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if UseColor(&bytes.Buffer{}) {
		t.Error("UseColor(buffer) = true, want false — colour is TTY-only")
	}
}

func TestTruncateAddsEllipsisOnlyWhenTooLong(t *testing.T) {
	if got := Truncate("hello", 10); got != "hello" {
		t.Errorf("Truncate(hello,10) = %q, want %q", got, "hello")
	}
	if got := Truncate("hello world", 8); got != "hello w…" {
		t.Errorf("Truncate(hello world,8) = %q, want %q", got, "hello w…")
	}
	// Counts runes, not bytes — descriptions contain — and → (§I.4).
	if got := Truncate("a→b→c→d", 5); got != "a→b→…" {
		t.Errorf("truncate on multibyte = %q, want %q", got, "a→b→…")
	}
}

func TestIsTTYFileOnARegularFileAndOnNil(t *testing.T) {
	// The stdin-side check: a redirected stdin is a regular file, never a
	// terminal, which is what makes `mem rm` refuse rather than assume yes.
	f, err := os.CreateTemp(t.TempDir(), "stdin-*")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if IsTTYFile(f) {
		t.Error("IsTTYFile = true for a regular file, want false")
	}
	if IsTTYFile(nil) {
		t.Error("IsTTYFile = true for nil, want false")
	}
}
