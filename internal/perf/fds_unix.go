//go:build darwin || linux || freebsd || netbsd || openbsd

package perf

import "os"

// openFDs counts this process's open file descriptors, or reports false where
// the platform has no readable descriptor directory.
func openFDs() (int, bool) {
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		return 0, false
	}
	// /dev/fd lists the directory handle used for the read itself.
	n := len(entries) - 1
	if n < 0 {
		n = 0
	}
	return n, true
}
