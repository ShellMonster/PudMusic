package app_test

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/perf"
)

// TestPerformanceBaselineCrossLayer observes an application lifecycle together
// with a local range request. It is intentionally synthetic: the baseline must
// be reproducible without a Bilibili account or a real media endpoint.
func TestPerformanceBaselineCrossLayer(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	a.NoteMounted()
	a.NoteReady()
	a.NoteFirstFrame()

	metrics := perf.NewMetrics()
	fixture := perf.NewRangeFixture([]byte("synthetic-video-segment"))
	t.Cleanup(func() { _ = fixture.Close() })

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fixture.URL(), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=0-7")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusPartialContent || string(body) != "syntheti" {
		t.Fatalf("fixture response status=%d body=%q", resp.StatusCode, body)
	}

	done := metrics.BeginRequest()
	metrics.RecordDiskWrite(int64(len(body)))
	metrics.RecordTempFileCreated()
	metrics.RecordTempFileRemoved()
	done()

	st := a.StartupTiming()
	if st.Mount <= 0 || st.Ready <= 0 || st.Frame <= 0 {
		t.Fatalf("startup timing missing lifecycle stages: %+v", st)
	}
	if got := fixture.Requests(); got != 1 {
		t.Fatalf("fixture requests=%d, want 1", got)
	}
	if got := metrics.Snapshot(); got.ActiveRequests != 0 || got.TempFilesCreated != got.TempFilesRemoved {
		t.Fatalf("resource baseline not settled: %+v", got)
	}
	if st.Frame < st.Ready || st.Ready < st.Mount {
		t.Fatalf("startup timing not monotonic: %+v", st)
	}
}
