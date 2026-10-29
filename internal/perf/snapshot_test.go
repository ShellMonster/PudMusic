package perf

import (
	"bytes"
	"encoding/json"
	"runtime"
	"testing"
)

func TestRuntimeSnapshotReportsLiveProcessFacts(t *testing.T) {
	got := SampleRuntime()
	if got.HeapAlloc <= 0 || got.HeapSys <= 0 {
		t.Fatalf("heap counters not live: %+v", got)
	}
	if got.Goroutines < 1 {
		t.Fatalf("goroutines = %d, want at least the test goroutine", got.Goroutines)
	}
	if got.FDs < 0 {
		t.Fatalf("file descriptors = %d, want a count or the not-measured marker", got.FDs)
	}
	if !got.FDsMeasured {
		t.Log("file descriptor count is unavailable on this platform; the snapshot says so explicitly")
	}
}

// TestProcessFootprintIsMeasuredOnlyWhereThePlatformCanMeasureIt pins the
// measured/unmeasured split. A zero with FootprintMeasured=false means "not
// read", never "this process holds no memory"; on darwin the numbers must be
// live and larger than zero.
func TestProcessFootprintIsMeasuredOnlyWhereThePlatformCanMeasureIt(t *testing.T) {
	got := SampleRuntime()
	if runtime.GOOS != "darwin" {
		if got.FootprintMeasured {
			t.Fatalf("footprint reported as measured on %s: %+v", runtime.GOOS, got)
		}
		if got.ResidentBytes != 0 || got.FootprintBytes != 0 {
			t.Fatalf("unmeasured footprint carries numbers: %+v", got)
		}
		return
	}
	if !got.FootprintMeasured {
		t.Fatal("process footprint is not measured on darwin")
	}
	if got.FootprintBytes <= 0 || got.ResidentBytes <= 0 {
		t.Fatalf("measured footprint is not live: %+v", got)
	}
}

func TestGaugesBalanceByteAccounting(t *testing.T) {
	var g Gauges
	g.AddFramePoolBytes(3 << 20)
	g.AddMediaTempBytes(5 << 20)
	g.AddFramePoolBytes(-1 << 20)
	g.AddImageCacheBytes(7 << 20)
	g.AddImageCacheBytes(-2 << 20)

	got := g.Snapshot()
	if got.FramePoolBytes != 2<<20 {
		t.Fatalf("frame pool bytes = %d, want %d", got.FramePoolBytes, 2<<20)
	}
	if got.MediaTempBytes != 5<<20 {
		t.Fatalf("media temp bytes = %d, want %d", got.MediaTempBytes, 5<<20)
	}
	if got.ImageCacheBytes != 5<<20 {
		t.Fatalf("image cache bytes = %d, want %d", got.ImageCacheBytes, 5<<20)
	}
}

// TestGaugesKeepOnlyTheLargestObservation pins the high-water marks: they
// describe the biggest thing the process has handled, so a smaller observation
// must never lower them.
func TestGaugesKeepOnlyTheLargestObservation(t *testing.T) {
	var g Gauges
	g.NoteMP4BoxBytes(1 << 20)
	g.NoteMP4BoxBytes(4 << 20)
	g.NoteMP4BoxBytes(2 << 20)
	g.NoteMP4SampleBytes(64 << 10)
	g.NoteMP4SampleBytes(32 << 10)
	g.NoteDanmakuCompressedBytes(300 << 10)
	g.NoteDanmakuCompressedBytes(100 << 10)
	g.NoteDanmakuInflatedBytes(8 << 20)
	g.NoteDanmakuInflatedBytes(1 << 20)

	got := g.Snapshot()
	if got.MP4MaxBoxBytes != 4<<20 {
		t.Errorf("mp4 max box = %d, want %d", got.MP4MaxBoxBytes, 4<<20)
	}
	if got.MP4MaxSampleBytes != 64<<10 {
		t.Errorf("mp4 max sample = %d, want %d", got.MP4MaxSampleBytes, 64<<10)
	}
	if got.DanmakuCompressedBytes != 300<<10 {
		t.Errorf("danmaku compressed = %d, want %d", got.DanmakuCompressedBytes, 300<<10)
	}
	if got.DanmakuInflatedBytes != 8<<20 {
		t.Errorf("danmaku inflated = %d, want %d", got.DanmakuInflatedBytes, 8<<20)
	}
}

// TestTakeSnapshotCarriesMetricsOnlyWhenGiven pins both halves of the optional
// Metrics argument: no argument keeps the zero record (existing callers), and a
// given collection lands in the snapshot.
func TestTakeSnapshotCarriesMetricsOnlyWhenGiven(t *testing.T) {
	if got := TakeSnapshot().Metrics; got != (MetricsSnapshot{}) {
		t.Fatalf("snapshot without metrics carries counters: %+v", got)
	}

	m := NewMetrics()
	m.RecordDiskWrite(4096)
	m.RecordBitmapUpdate()
	if got := TakeSnapshot(m).Metrics; got.DiskBytes != 4096 || got.BitmapUpdates != 1 {
		t.Fatalf("snapshot does not carry the given metrics: %+v", got)
	}
	// A nil collection is the same as none: it must not panic and must not
	// invent counters.
	if got := TakeSnapshot(nil).Metrics; got != (MetricsSnapshot{}) {
		t.Fatalf("snapshot with a nil Metrics carries counters: %+v", got)
	}
}

func TestSnapshotIsJSONAndCarriesNoFreeText(t *testing.T) {
	s := TakeSnapshot()
	var buf bytes.Buffer
	if err := s.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("snapshot is not JSON: %v\n%s", err, buf.String())
	}
	for _, key := range []string{"runtime", "metrics", "gauges"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("snapshot lacks %q: %s", key, buf.String())
		}
	}
	// The diagnostic record is numeric only: a free-text field would be the
	// easiest way for a credential or a danmaku body to leak into an artifact.
	runtimeFields, _ := decoded["runtime"].(map[string]any)
	for key, v := range runtimeFields {
		switch v.(type) {
		case float64, bool:
		default:
			t.Fatalf("runtime field %q is not numeric or boolean: %T", key, v)
		}
	}
	gauges, _ := decoded["gauges"].(map[string]any)
	for _, key := range []string{
		"frame_pool_bytes", "media_temp_bytes", "image_cache_bytes",
		"mp4_max_box_bytes", "mp4_max_sample_bytes",
		"danmaku_compressed_bytes", "danmaku_inflated_bytes",
	} {
		if _, ok := gauges[key]; !ok {
			t.Fatalf("gauges lacks %q: %s", key, buf.String())
		}
	}
	for _, key := range []string{"resident_bytes", "footprint_bytes", "footprint_measured"} {
		if _, ok := runtimeFields[key]; !ok {
			t.Fatalf("runtime lacks %q: %s", key, buf.String())
		}
	}
}

func TestDiagnosticsAreOffUnlessExplicitlyEnabled(t *testing.T) {
	t.Setenv("BIU_PERF_DIAGNOSTICS", "")
	if DiagnosticsEnabled() {
		t.Fatal("diagnostics are enabled without the environment variable")
	}
	t.Setenv("BIU_PERF_DIAGNOSTICS", "1")
	if !DiagnosticsEnabled() {
		t.Fatal("diagnostics are disabled with BIU_PERF_DIAGNOSTICS=1")
	}
}
