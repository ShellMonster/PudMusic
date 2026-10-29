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
// only; the process footprint also covers Metal, AppKit and mapped files, so it
// is read separately from the OS (see footprint_darwin.go).
type RuntimeSnapshot struct {
	HeapAlloc   uint64 `json:"heap_alloc"`
	HeapSys     uint64 `json:"heap_sys"`
	HeapInuse   uint64 `json:"heap_inuse"`
	HeapObjects uint64 `json:"heap_objects"`
	Sys         uint64 `json:"sys"`
	Goroutines  int    `json:"goroutines"`
	NumGC       uint32 `json:"num_gc"`
	PauseTotal  int64  `json:"pause_total_ns"`

	// ResidentBytes / FootprintBytes are this process's resident size and
	// phys_footprint (the number Activity Monitor's "Memory" column shows).
	// Both are zero unless FootprintMeasured is true: an unmeasured zero must
	// never be read as "this process holds no memory".
	ResidentBytes     int64 `json:"resident_bytes"`
	FootprintBytes    int64 `json:"footprint_bytes"`
	FootprintMeasured bool  `json:"footprint_measured"`

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
	if resident, footprint, ok := processFootprint(); ok {
		s.ResidentBytes, s.FootprintBytes, s.FootprintMeasured = resident, footprint, true
	}
	if n, ok := openFDs(); ok {
		s.FDs, s.FDsMeasured = n, true
	}
	return s
}

// Gauges are the byte gauges the UI and media layers update as resources come
// and go: a pool's live bytes, a media temp file's current size, the decoded
// cover cache. They are gauges rather than counters because the question is
// "how much is held now", and a counter could only answer "how much was ever
// allocated".
//
// A few entries are high-water marks instead of live sizes (the largest MP4 box
// or sample seen, the largest danmaku body inflated). Those describe one
// bounded event whose size matters when it happens, and a running maximum is
// the only way a snapshot taken later can still report it.
type Gauges struct {
	framePoolBytes  atomic.Int64
	mediaTempBytes  atomic.Int64
	imageCacheBytes atomic.Int64

	mp4MaxBoxBytes    atomic.Int64
	mp4MaxSampleBytes atomic.Int64

	danmakuCompressedBytes atomic.Int64
	danmakuInflatedBytes   atomic.Int64
}

// processGauges is the process-wide collection the media layers write into, so a
// diagnostic snapshot taken anywhere in the process sees the same numbers.
var processGauges Gauges

// ProcessGauges returns the process-wide gauge collection.
//
// Packages that own a measurable resource (the video frame pool, a media temp
// file, the cover cache) update this one; TakeSnapshot reads it. Tests that
// need an isolated counter pass their own Gauges instead of touching this one.
func ProcessGauges() *Gauges { return &processGauges }

// AddFramePoolBytes moves the decoded-video frame pool gauge by delta.
func (g *Gauges) AddFramePoolBytes(delta int64) { g.framePoolBytes.Add(delta) }

// AddMediaTempBytes moves the media temp file gauge by delta.
func (g *Gauges) AddMediaTempBytes(delta int64) { g.mediaTempBytes.Add(delta) }

// AddImageCacheBytes moves the decoded cover cache gauge by delta.
//
// The cache owns the number; it calls this whenever its byte account changes
// instead of perf importing the UI package (that would invert the dependency).
func (g *Gauges) AddImageCacheBytes(delta int64) { g.imageCacheBytes.Add(delta) }

// NoteMP4BoxBytes records the largest top-level MP4 box read into memory.
func (g *Gauges) NoteMP4BoxBytes(n int64) { noteMax(&g.mp4MaxBoxBytes, n) }

// NoteMP4SampleBytes records the largest single MP4 sample copied to the output.
func (g *Gauges) NoteMP4SampleBytes(n int64) { noteMax(&g.mp4MaxSampleBytes, n) }

// NoteDanmakuCompressedBytes records the largest compressed danmaku body seen.
func (g *Gauges) NoteDanmakuCompressedBytes(n int64) { noteMax(&g.danmakuCompressedBytes, n) }

// NoteDanmakuInflatedBytes records the largest inflated danmaku document seen.
func (g *Gauges) NoteDanmakuInflatedBytes(n int64) { noteMax(&g.danmakuInflatedBytes, n) }

// noteMax raises *v to n when n is larger. Never lowers it: a high-water mark
// that went back down would stop describing what the process actually saw.
func noteMax(v *atomic.Int64, n int64) {
	for {
		old := v.Load()
		if n <= old || v.CompareAndSwap(old, n) {
			return
		}
	}
}

// GaugesSnapshot is a point-in-time copy of Gauges.
type GaugesSnapshot struct {
	FramePoolBytes  int64 `json:"frame_pool_bytes"`
	MediaTempBytes  int64 `json:"media_temp_bytes"`
	ImageCacheBytes int64 `json:"image_cache_bytes"`

	MP4MaxBoxBytes    int64 `json:"mp4_max_box_bytes"`
	MP4MaxSampleBytes int64 `json:"mp4_max_sample_bytes"`

	DanmakuCompressedBytes int64 `json:"danmaku_compressed_bytes"`
	DanmakuInflatedBytes   int64 `json:"danmaku_inflated_bytes"`
}

// Snapshot returns a lock-free copy.
func (g *Gauges) Snapshot() GaugesSnapshot {
	return GaugesSnapshot{
		FramePoolBytes:         g.framePoolBytes.Load(),
		MediaTempBytes:         g.mediaTempBytes.Load(),
		ImageCacheBytes:        g.imageCacheBytes.Load(),
		MP4MaxBoxBytes:         g.mp4MaxBoxBytes.Load(),
		MP4MaxSampleBytes:      g.mp4MaxSampleBytes.Load(),
		DanmakuCompressedBytes: g.danmakuCompressedBytes.Load(),
		DanmakuInflatedBytes:   g.danmakuInflatedBytes.Load(),
	}
}

// Snapshot is one diagnostic record: process facts, the optional counters, and
// the byte gauges, side by side so a measurement file is self-contained.
type Snapshot struct {
	Runtime RuntimeSnapshot `json:"runtime"`
	Metrics MetricsSnapshot `json:"metrics"`
	Gauges  GaugesSnapshot  `json:"gauges"`
}

// TakeSnapshot samples the process. It accepts an optional Metrics collection:
// callers that have one pass it in so the record carries the counters, and
// callers that do not get the zero MetricsSnapshot they have always gotten.
//
// The byte gauges come from the process-wide collection, so a frame pool that is
// live when the snapshot is taken shows up here without the caller having to know
// which subsystem owns it.
func TakeSnapshot(metrics ...*Metrics) Snapshot {
	s := Snapshot{Runtime: SampleRuntime(), Gauges: ProcessGauges().Snapshot()}
	if len(metrics) > 0 && metrics[0] != nil {
		s.Metrics = metrics[0].Snapshot()
	}
	return s
}

// WriteJSON writes the record as one JSON object.
func (s Snapshot) WriteJSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(s)
}
