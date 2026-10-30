//go:build !darwin && !linux && !freebsd && !netbsd && !openbsd

package perf

// openFDs reports false where the platform has no readable descriptor
// directory; the snapshot then says the count was not measured.
func openFDs() (int, bool) { return 0, false }
