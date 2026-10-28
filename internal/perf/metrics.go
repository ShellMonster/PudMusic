// Package perf contains small, dependency-free seams used by performance tests.
//
// The counters deliberately describe observable work rather than implementation
// names: request concurrency, bytes written, bitmap updates, resource balance,
// goroutine balance and lock wait time. Production code may adopt these seams in
// later performance tickets without changing the meaning of the measurements.
package perf

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics is a concurrency-safe performance counter collection.
type Metrics struct {
	requests      atomic.Int64
	active        atomic.Int64
	maxConcurrent atomic.Int64
	diskBytes     atomic.Int64
	bitmapUpdates atomic.Int64
	decoderOpens  atomic.Int64
	decoderCloses atomic.Int64
	tempCreated   atomic.Int64
	tempRemoved   atomic.Int64
	goroutines    atomic.Int64
	goroutinesEnd atomic.Int64
	lockWaits     atomic.Int64
	lockNanos     atomic.Int64
}

// MetricsSnapshot is a point-in-time copy of Metrics.
type MetricsSnapshot struct {
	Requests           int64
	ActiveRequests     int64
	MaxConcurrent      int64
	DiskBytes          int64
	BitmapUpdates      int64
	DecoderOpens       int64
	DecoderCloses      int64
	TempFilesCreated   int64
	TempFilesRemoved   int64
	GoroutinesStarted  int64
	GoroutinesFinished int64
	LockWaits          int64
	LockWaitNanos      int64
}

// NewMetrics returns an empty counter collection.
func NewMetrics() *Metrics { return &Metrics{} }

// BeginRequest records a request and returns its completion callback.
func (m *Metrics) BeginRequest() func() {
	m.requests.Add(1)
	active := m.active.Add(1)
	for {
		old := m.maxConcurrent.Load()
		if active <= old || m.maxConcurrent.CompareAndSwap(old, active) {
			break
		}
	}
	return func() { m.active.Add(-1) }
}

func (m *Metrics) RecordDiskWrite(bytes int64)    { m.diskBytes.Add(bytes) }
func (m *Metrics) RecordBitmapUpdate()            { m.bitmapUpdates.Add(1) }
func (m *Metrics) RecordDecoderOpen()             { m.decoderOpens.Add(1) }
func (m *Metrics) RecordDecoderClose()            { m.decoderCloses.Add(1) }
func (m *Metrics) RecordTempFileCreated()         { m.tempCreated.Add(1) }
func (m *Metrics) RecordTempFileRemoved()         { m.tempRemoved.Add(1) }
func (m *Metrics) RecordGoroutineStarted()        { m.goroutines.Add(1) }
func (m *Metrics) RecordGoroutineFinished()       { m.goroutinesEnd.Add(1) }
func (m *Metrics) RecordLockWait(d time.Duration) { m.lockWaits.Add(1); m.lockNanos.Add(int64(d)) }

// Snapshot returns a lock-free copy suitable for assertions and benchmark output.
func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		Requests:           m.requests.Load(),
		ActiveRequests:     m.active.Load(),
		MaxConcurrent:      m.maxConcurrent.Load(),
		DiskBytes:          m.diskBytes.Load(),
		BitmapUpdates:      m.bitmapUpdates.Load(),
		DecoderOpens:       m.decoderOpens.Load(),
		DecoderCloses:      m.decoderCloses.Load(),
		TempFilesCreated:   m.tempCreated.Load(),
		TempFilesRemoved:   m.tempRemoved.Load(),
		GoroutinesStarted:  m.goroutines.Load(),
		GoroutinesFinished: m.goroutinesEnd.Load(),
		LockWaits:          m.lockWaits.Load(),
		LockWaitNanos:      m.lockNanos.Load(),
	}
}

// ManualClock is a deterministic timer seam for debounce and lifecycle tests.
type ManualClock struct {
	mu     sync.Mutex
	timers []*manualTimer
}

type manualTimer struct {
	remaining time.Duration
	fn        func()
	stopped   bool
}

func NewManualClock() *ManualClock { return &ManualClock{} }

// After registers fn and returns a cancellation callback.
func (c *ManualClock) After(d time.Duration, fn func()) func() {
	c.mu.Lock()
	t := &manualTimer{remaining: d, fn: fn}
	c.timers = append(c.timers, t)
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		t.stopped = true
		c.mu.Unlock()
	}
}

// Advance runs timers whose deadlines are reached. Callbacks run outside the lock.
func (c *ManualClock) Advance(d time.Duration) {
	c.mu.Lock()
	var ready []*manualTimer
	keep := c.timers[:0]
	for _, t := range c.timers {
		switch {
		case t.stopped:
		case t.remaining <= d:
			ready = append(ready, t)
		default:
			t.remaining -= d
			keep = append(keep, t)
		}
	}
	c.timers = keep
	c.mu.Unlock()
	for _, t := range ready {
		t.fn()
	}
}

// RangeFixture is a local HTTP fixture for byte-range clients.
type RangeFixture struct {
	server *http.Server
	url    string
	data   []byte
	mu     sync.Mutex
	active int
	max    int
	count  int
}

func NewRangeFixture(data []byte) *RangeFixture {
	f := &RangeFixture{data: append([]byte(nil), data...)}
	mux := http.NewServeMux()
	mux.HandleFunc("/range", f.serveHTTP)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	f.server = &http.Server{Handler: mux}
	f.url = "http://" + listener.Addr().String() + "/range"
	go func() { _ = f.server.Serve(listener) }()
	return f
}

func (f *RangeFixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.count++
	f.active++
	if f.active > f.max {
		f.max = f.active
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()

	from, to, ok := parseRange(r.Header.Get("Range"), len(f.data))
	if !ok {
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, to-1, len(f.data)))
	w.Header().Set("Content-Length", strconv.Itoa(to-from))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(f.data[from:to])
}

func parseRange(header string, size int) (int, int, bool) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false
	}
	parts := strings.Split(strings.TrimPrefix(header, "bytes="), "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	from, err1 := strconv.Atoi(parts[0])
	to, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || from < 0 || to < from || to >= size {
		return 0, 0, false
	}
	return from, to + 1, true
}

func (f *RangeFixture) URL() string { return f.url }
func (f *RangeFixture) Requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.count
}
func (f *RangeFixture) MaxConcurrency() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.max
}
func (f *RangeFixture) Close() error { return f.server.Close() }
