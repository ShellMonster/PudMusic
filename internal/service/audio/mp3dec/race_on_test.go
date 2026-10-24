//go:build race

package mp3dec_test

// underRace reports that this run has the race detector's instrumentation,
// which slows the decoder by one to two orders of magnitude. The speed check
// reports its numbers either way; only the threshold is dropped.
const underRace = true
