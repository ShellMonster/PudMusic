//go:build !darwin

package perf

// processFootprint reports false on platforms without a process-level memory
// reading: the snapshot then says the footprint was not measured rather than
// reporting zero, which a reader would take for "this process holds nothing".
func processFootprint() (resident, footprint int64, ok bool) { return 0, 0, false }
