//go:build !windows

package update

import "syscall"

// detachAttr puts the background check in a session of its own. Without it the
// child stays in its parent's process group, and closing the terminal a moment
// after `mem list` returns sends it SIGHUP before it can write the cache — so
// the check that costs the user nothing would also never finish.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
