package perf

import (
	"encoding/json"
	"io"
	"os"
	"runtime"
	"sync/atomic"
)

// DiagnosticsEnabled reports whether a diagnostic snapshot should be emitted.
//
// It is off by default: the snapshot is for a reproducible baseline run, not for
// every user session. The value is an environment switch so a measurement run
// can turn it on without rebuilding.
func DiagnosticsEnabled() bool {
	switch os.Getenv("BIU_PERF_DIAGNOSTICS") {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// RuntimeSnapshot is the process-level half of a diagnostic record.
//
// Every field is numeric or boolean on purpose: this record is written to logs
// and artifacts, and a free-text field is the easiest way for a credential, a
// signed URL or a danmaku body to leak out. Heap numbers describe the Go heap
// only; they are not the process footprint, which also covers Metal, AppKit and
// mapped files.
type RuntimeSnapshot struct {
	HeapAlloc   uint64 `json:"heap_alloc"`
	HeapSys     uint64 `json:"heap_sys"`
	HeapInuse   uint64 `json:"heap_inuse"`
	HeapObjects uint64 `json:"heap_objects"`
	Sys         uint64 `json:"sys"`
	Goroutines  int    `json:"goroutines"`
	NumGC       uint32 `json:"num_gc"`
	PauseTotal  int64  `json:"pause_total_ns"`

	// FDs is the open file descriptor count, or -1 when this platform has no
	// cheap way to answer. FDsMeasured says which of the two it is, so a reader
	// never confuses "no descriptors" with "not measured".
	FDs         int  `json:"fds"`
	FDsMeasured bool `json:"fds_measured"`
}

// SampleRuntime samples the live process facts of the current process.
func SampleRuntime() RuntimeSnapshot {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	s := RuntimeSnapshot{
		HeapAlloc:   m.HeapAlloc,
		HeapSys:     m.HeapSys,
		HeapInuse:   m.HeapInuse,
		HeapObjects: m.HeapObjects,
		Sys:         m.Sys,
		Goroutines:  runtime.NumGoroutine(),
		NumGC:       m.NumGC,
		PauseTotal:  int64(m.PauseTotalNs),
		FDs:         -1,
	}
	if n, ok := openFDs(); ok {
		s.FDs, s.FDsMeasured = n, true
	}
	return s
}

// Gauges are the byte gauges the UI and media layers update as resources come
// and go: a pool's live bytes, or a media temp file's current size. They are
// gauges rather than counters because the question is "how much is held now",
// and a counter could only answer "how much was ever allocated".
type Gauges struct {
	framePoolBytes atomic.Int64
	mediaTempBytes atomic.Int64
}

// processGauges is the process-wide collection the media layers write into, so a
// diagnostic snapshot taken anywhere in the process sees the same numbers.
var processGauges Gauges

// ProcessGauges returns the process-wide gauge collection.
//
// Packages that own a measurable resource (the video frame pool, a media temp
// file) update this one; TakeSnapshot reads it. Tests that need an isolated
// counter pass their own Gauges instead of touching this one.
func ProcessGauges() *Gauges { return &processGauges }

// AddFramePoolBytes moves the decoded-video frame pool gauge by delta.
func (g *Gauges) AddFramePoolBytes(delta int64) { g.framePoolBytes.Add(delta) }

// AddMediaTempBytes moves the media temp file gauge by delta.
func (g *Gauges) AddMediaTempBytes(delta int64) { g.mediaTempBytes.Add(delta) }

// GaugesSnapshot is a point-in-time copy of Gauges.
type GaugesSnapshot struct {
	FramePoolBytes int64 `json:"frame_pool_bytes"`
	MediaTempBytes int64 `json:"media_temp_bytes"`
}

// Snapshot returns a lock-free copy.
func (g *Gauges) Snapshot() GaugesSnapshot {
	return GaugesSnapshot{
		FramePoolBytes: g.framePoolBytes.Load(),
		MediaTempBytes: g.mediaTempBytes.Load(),
	}
}

// Snapshot is one diagnostic record: process facts, the optional counters, and
// the byte gauges, side by side so a measurement file is self-contained.
type Snapshot struct {
	Runtime RuntimeSnapshot `json:"runtime"`
	Metrics MetricsSnapshot `json:"metrics"`
	Gauges  GaugesSnapshot  `json:"gauges"`
}

// TakeSnapshot samples the process without touching a Metrics collection.
//
// The byte gauges come from the process-wide collection, so a frame pool that is
// live when the snapshot is taken shows up here without the caller having to know
// which subsystem owns it.
func TakeSnapshot() Snapshot {
	return Snapshot{Runtime: SampleRuntime(), Gauges: ProcessGauges().Snapshot()}
}

// WriteJSON writes the record as one JSON object.
func (s Snapshot) WriteJSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(s)
}
