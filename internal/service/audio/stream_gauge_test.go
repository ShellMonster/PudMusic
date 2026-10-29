package audio

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
