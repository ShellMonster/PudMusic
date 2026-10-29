package perf

import (
	"bytes"
	"encoding/json"
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

func TestGaugesBalanceByteAccounting(t *testing.T) {
	var g Gauges
	g.AddFramePoolBytes(3 << 20)
	g.AddMediaTempBytes(5 << 20)
	g.AddFramePoolBytes(-1 << 20)

	got := g.Snapshot()
	if got.FramePoolBytes != 2<<20 {
		t.Fatalf("frame pool bytes = %d, want %d", got.FramePoolBytes, 2<<20)
	}
	if got.MediaTempBytes != 5<<20 {
		t.Fatalf("media temp bytes = %d, want %d", got.MediaTempBytes, 5<<20)
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
	runtime, _ := decoded["runtime"].(map[string]any)
	for key, v := range runtime {
		switch v.(type) {
		case float64, bool:
		default:
			t.Fatalf("runtime field %q is not numeric or boolean: %T", key, v)
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
