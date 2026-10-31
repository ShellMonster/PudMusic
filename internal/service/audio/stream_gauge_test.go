package audio

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/perf"
)

// TestStreamSourceTempGaugeFollowsTheFileAndClose pins the media-temp gauge:
// the diagnostic snapshot must report the bytes a stream holds on disk, and
// Close must return the gauge to zero.
func TestStreamSourceTempGaugeFollowsTheFileAndClose(t *testing.T) {
	data := payload(256 << 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)

	var g perf.Gauges
	src, err := NewStreamSource(StreamSourceOptions{
		URLs:   []string{srv.URL},
		Client: srv.Client(),
		Gauges: &g,
	})
	if err != nil {
		t.Fatal(err)
	}

	waitForFilled(t, src, int64(len(data)))
	if got := g.Snapshot().MediaTempBytes; got != int64(len(data)) {
		t.Fatalf("media temp bytes = %d, want %d", got, len(data))
	}

	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
	if got := g.Snapshot().MediaTempBytes; got != 0 {
		t.Fatalf("after close: media temp bytes = %d, want 0", got)
	}
}

// TestStreamSourceTempGaugeDoesNotOutliveAFailedStream keeps the gauge honest
// when the stream dies before the promised length arrives.
func TestStreamSourceTempGaugeDoesNotOutliveAFailedStream(t *testing.T) {
	data := payload(64 << 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = w.Write(data[:1024])
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(srv.Close)

	var g perf.Gauges
	src, err := NewStreamSource(StreamSourceOptions{
		URLs:       []string{srv.URL},
		Client:     srv.Client(),
		Gauges:     &g,
		MaxRefresh: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })

	waitForFilled(t, src, 1024)
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
	if got := g.Snapshot().MediaTempBytes; got != 0 {
		t.Fatalf("after close of a failed stream: media temp bytes = %d, want 0", got)
	}
}

func TestStreamSourceTempGaugeIncludesTailAndRestartClearsIt(t *testing.T) {
	data := readMP4Fixture(t, "stereo16.mp4")
	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		from := rangeStart(r)
		if from > 0 {
			serveBytes(w, r, data, len(data))
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = w.Write(data[:64<<10])
		w.(http.Flusher).Flush()
		<-hold
	}))
	t.Cleanup(func() { close(hold); srv.Close() })

	var g perf.Gauges
	src, err := NewStreamSource(StreamSourceOptions{
		URLs:   []string{srv.URL},
		Client: srv.Client(),
		Gauges: &g,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })

	waitForTailComplete(t, src, int64(len(data)))
	if got := g.Snapshot().MediaTempBytes; got <= src.Filled() {
		t.Fatalf("media temp bytes = %d, want tail bytes included beyond sequential %d", got, src.Filled())
	}

	if err := src.restart(); err != nil {
		t.Fatal(err)
	}
	if got := g.Snapshot().MediaTempBytes; got != 0 {
		t.Fatalf("after restart: media temp bytes = %d, want 0", got)
	}
}

func TestStreamSourceTempGaugeCountsTailOverlapOnce(t *testing.T) {
	data := readMP4Fixture(t, "stereo16.mp4")
	release := make(chan struct{})
	var releaseOnce sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rangeStart(r) > 0 {
			serveBytes(w, r, data, len(data))
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = w.Write(data[:64<<10])
		w.(http.Flusher).Flush()
		<-release
		_, _ = w.Write(data[64<<10:])
	}))
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }); srv.Close() })

	var g perf.Gauges
	src, err := NewStreamSource(StreamSourceOptions{
		URLs:   []string{srv.URL},
		Client: srv.Client(),
		Gauges: &g,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })

	waitForTailComplete(t, src, int64(len(data)))
	src.mu.Lock()
	filled, tailStart, tailGot := src.filled, src.tailStart, src.tailGot
	src.mu.Unlock()
	want := filled + tailGot
	if tailStart < filled {
		want -= filled - tailStart
	}
	if got := g.Snapshot().MediaTempBytes; got != want {
		t.Fatalf("while sequential download overlaps tail: media temp bytes = %d, want union %d (filled=%d tail=[%d,%d))", got, want, filled, tailStart, tailStart+tailGot)
	}

	releaseOnce.Do(func() { close(release) })
	if _, err := io.ReadAll(src); err != nil {
		t.Fatal(err)
	}
	if got := g.Snapshot().MediaTempBytes; got != int64(len(data)) {
		t.Fatalf("after sequential download overlaps tail: media temp bytes = %d, want %d", got, len(data))
	}
}

func TestStreamSourceTempGaugeIgnoresFailedTailFetch(t *testing.T) {
	data := readMP4Fixture(t, "stereo16.mp4")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rangeStart(r) > 0 {
			http.Error(w, "tail unavailable", http.StatusServiceUnavailable)
			return
		}
		serveBytes(w, r, data, len(data))
	}))
	t.Cleanup(srv.Close)

	var g perf.Gauges
	src, err := NewStreamSource(StreamSourceOptions{
		URLs:   []string{srv.URL},
		Client: srv.Client(),
		Gauges: &g,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })

	if _, err := io.ReadAll(src); err != nil {
		t.Fatalf("tail 失败不应影响主下载：%v", err)
	}
	if got := g.Snapshot().MediaTempBytes; got != int64(len(data)) {
		t.Fatalf("tail 失败后 media temp bytes = %d, want %d", got, len(data))
	}
}

func waitForFilled(t *testing.T, src *StreamSource, want int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		src.mu.Lock()
		filled := src.filled
		src.mu.Unlock()
		if filled >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("stream never filled to %d bytes", want)
}

func waitForTailComplete(t *testing.T, src *StreamSource, total int64) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		src.mu.Lock()
		start, got := src.tailStart, src.tailGot
		src.mu.Unlock()
		if start >= 0 && got == total-start {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("尾部 Range 请求一直没有完整落盘")
}
