//go:build linux

package cli

import "golang.org/x/sys/unix"

// enterCbreak switches the terminal at fd into "cbreak" mode: input is delivered
// byte-by-byte (ICANON off) without echo (ECHO off), but signal generation
// (ISIG) and output post-processing (OPOST) are left untouched. Keeping ISIG
// means Ctrl+C still raises SIGINT, and keeping OPOST means streamed agent
// output keeps its proper newline translation. It returns a restore function
// and true on success; on any failure (e.g. fd is not a terminal) it reports
// false so callers can degrade gracefully.
func enterCbreak(fd int) (restore func() error, ok bool) {
	old, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, false
	}
	raw := *old
	raw.Lflag &^= unix.ICANON | unix.ECHO
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &raw); err != nil {
		return nil, false
	}
	return func() error { return unix.IoctlSetTermios(fd, unix.TCSETS, old) }, true
}
