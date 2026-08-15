//go:build windows

package update

import "syscall"

// detachAttr has no work to do here: Windows has no sessions to leave, and no
// release is published for it anyway (see the note in ci.yml). It exists so
// `go install` still compiles on a platform the tool does not ship to.
func detachAttr() *syscall.SysProcAttr { return nil }
