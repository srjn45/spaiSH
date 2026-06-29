//go:build !linux

package cli

// enterCbreak is a no-op fallback on platforms without the Linux termios
// handling. Esc-to-interrupt is disabled there; Ctrl+C (SIGINT) still cancels a
// running turn.
func enterCbreak(fd int) (restore func() error, ok bool) {
	return nil, false
}
