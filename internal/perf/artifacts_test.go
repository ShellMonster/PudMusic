package perf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMeasureArtifactsReportsPerFileAndTotalBytes(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small")
	large := filepath.Join(dir, "large")
	if err := os.WriteFile(small, make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(large, make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := MeasureArtifacts(small, large)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("artifacts = %+v, want two entries", got)
	}
	if total := TotalBytes(got); total != 5120 {
		t.Fatalf("total bytes = %d, want 5120", total)
	}
	if got[0].Bytes != 1024 || got[1].Bytes != 4096 {
		t.Fatalf("per-file bytes = %+v", got)
	}
}

func TestMeasureArtifactsFailsOnMissingFile(t *testing.T) {
	if _, err := MeasureArtifacts(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("a missing artifact reported a size instead of an error")
	}
}

func TestMeasureArtifactsOfNothingIsEmpty(t *testing.T) {
	got, err := MeasureArtifacts()
	if err != nil || len(got) != 0 || TotalBytes(got) != 0 {
		t.Fatalf("empty measurement = %+v, %v", got, err)
	}
}
