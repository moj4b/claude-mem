package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exec drives the CLI at its outermost seam: argv in, streams and exit code out.
func exec(args ...string) (stdout, stderr string, code int) {
	var o, e bytes.Buffer
	code = Run(args, &o, &e)
	return o.String(), e.String(), code
}

// writeSettingsFile creates a .claude settings file, for the tests that check
// resolution layers are honoured — or deliberately ignored — from the CLI.
func writeSettingsFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEveryAcceptedCommandHasAHandler(t *testing.T) {
	for cmd := range commands {
		if handlers[cmd] == nil {
			t.Errorf("command %q is accepted by parseArgs but has no handler", cmd)
		}
	}
	for cmd := range handlers {
		if !commands[cmd] {
			t.Errorf("handler %q is unreachable — parseArgs rejects it", cmd)
		}
	}
}

func TestScopeFlagsAreMutuallyExclusive(t *testing.T) {
	const want = "--dir, --all and --project cannot be combined"
	for _, args := range [][]string{
		{"--all", "--project", "courier", "list"},
		{"--all", "--dir", "/tmp", "list"},
		{"--dir", "/tmp", "--project", "courier", "list"},
	} {
		out, errOut, code := exec(args...)
		if code != 2 {
			t.Errorf("%v: exit code = %d, want 2", args, code)
		}
		if out != "" {
			t.Errorf("%v: stdout = %q, want empty", args, out)
		}
		if !strings.Contains(errOut, want) {
			t.Errorf("%v: stderr = %q, want it to contain %q", args, errOut, want)
		}
	}
}

func TestReadRejectsAll(t *testing.T) {
	// read must never widen (§II.0); --all is a usage error, not a silent widen.
	_, errOut, code := exec("--all", "read", "user_prefs")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut, "read cannot be combined with --all") {
		t.Errorf("stderr = %q, want it to explain read never widens", errOut)
	}
}

func TestFlagsAcceptedOnEitherSideOfSubcommand(t *testing.T) {
	before, err := parseArgs([]string{"--dir", "/x", "list"})
	if err != nil {
		t.Fatalf("flags before subcommand: %v", err)
	}
	after, err := parseArgs([]string{"list", "--dir", "/x"})
	if err != nil {
		t.Fatalf("flags after subcommand: %v", err)
	}
	if before.cmd != "list" || before.dir != "/x" {
		t.Errorf("before = %+v, want cmd=list dir=/x", before)
	}
	if after.cmd != before.cmd || after.dir != before.dir {
		t.Errorf("after = %+v, want identical to before = %+v", after, before)
	}
}

func TestBareMemIsList(t *testing.T) {
	o, err := parseArgs(nil)
	if err != nil {
		t.Fatalf("parseArgs(nil): %v", err)
	}
	if o.cmd != "list" {
		t.Errorf("cmd = %q, want %q", o.cmd, "list")
	}
}

func TestMissingOperandIsUsageError(t *testing.T) {
	for _, tc := range []struct{ cmd, want string }{
		{"read", "usage: mem read <name>"},
		{"show", "usage: mem show <name>"},
		{"search", "usage: mem search <query>"},
	} {
		out, errOut, code := exec(tc.cmd)
		if code != 2 {
			t.Errorf("%s: exit code = %d, want 2", tc.cmd, code)
		}
		if out != "" {
			t.Errorf("%s: stdout = %q, want empty", tc.cmd, out)
		}
		if !strings.Contains(errOut, tc.want) {
			t.Errorf("%s: stderr = %q, want it to contain %q", tc.cmd, errOut, tc.want)
		}
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	out, errOut, code := exec("lst")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "unknown command 'lst'") {
		t.Errorf("stderr = %q, want it to name the command", errOut)
	}
	if !strings.Contains(errOut, "mem --help") {
		t.Errorf("stderr = %q, want it to point at --help", errOut)
	}
}

func TestVersionGoesToStdoutAndExitsZero(t *testing.T) {
	out, errOut, code := exec("--version")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.HasPrefix(out, "mem ") {
		t.Errorf("stdout = %q, want a version line starting %q", out, "mem ")
	}
	if errOut != "" {
		t.Errorf("stderr = %q, want empty", errOut)
	}
}

func TestHelpGoesToStdoutAndExitsZero(t *testing.T) {
	out, errOut, code := exec("--help")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "mem read <name>") {
		t.Errorf("stdout missing usage text, got:\n%s", out)
	}
	if errOut != "" {
		t.Errorf("stderr = %q, want empty", errOut)
	}
}
