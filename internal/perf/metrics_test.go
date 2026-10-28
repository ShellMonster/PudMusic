package perf

import (
	"context"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestMetricsTrackConcurrentRequestsAndResourceBalance(t *testing.T) {
	m := NewMetrics()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			done := m.BeginRequest()
			m.RecordDiskWrite(4096)
			m.RecordBitmapUpdate()
			m.RecordDecoderOpen()
			m.RecordTempFileCreated()
			m.RecordGoroutineStarted()
			m.RecordLockWait(2 * time.Microsecond)
			done()
			m.RecordDecoderClose()
			m.RecordTempFileRemoved()
			m.RecordGoroutineFinished()
		}()
	}
	wg.Wait()

	got := m.Snapshot()
	if got.Requests != 8 || got.MaxConcurrent < 1 || got.ActiveRequests != 0 {
		t.Fatalf("request metrics = %+v", got)
	}
	if got.DiskBytes != 8*4096 || got.BitmapUpdates != 8 {
		t.Fatalf("work metrics = %+v", got)
	}
	if got.DecoderOpens != got.DecoderCloses || got.TempFilesCreated != got.TempFilesRemoved || got.GoroutinesStarted != got.GoroutinesFinished {
		t.Fatalf("resource balance = %+v", got)
	}
	if got.LockWaits != 8 || got.LockWaitNanos != int64(8*2*time.Microsecond) {
		t.Fatalf("lock metrics = %+v", got)
	}
}

func TestManualClockRunsDueTimersWithoutSleeping(t *testing.T) {
	clock := NewManualClock()
	var called int
	cancel := clock.After(2*time.Second, func() { called++ })
	clock.Advance(time.Second)
	if called != 0 {
		t.Fatalf("timer fired early: %d", called)
	}
	clock.Advance(time.Second)
	if called != 1 {
		t.Fatalf("timer calls = %d, want 1", called)
	}
	cancel()
	clock.Advance(time.Hour)
	if called != 1 {
		t.Fatalf("cancelled timer fired: %d", called)
	}
}

func TestRangeFixtureServesRequestedBytesAndTracksConcurrency(t *testing.T) {
	fixture := NewRangeFixture([]byte("0123456789"))
	defer fixture.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fixture.URL(), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=2-5")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusPartialContent || string(body) != "2345" {
		t.Fatalf("response status=%d body=%q", resp.StatusCode, body)
	}
	if got := fixture.Requests(); got != 1 {
		t.Fatalf("requests=%d, want 1", got)
	}
	if got := fixture.MaxConcurrency(); got != 1 {
		t.Fatalf("max concurrency=%d, want 1", got)
	}
}
