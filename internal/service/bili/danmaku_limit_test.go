package bili

import (
	"bytes"
	"compress/flate"
	"strings"
	"testing"
)

// TestInflateDanmakuRefusesAnOutputPastTheLimit pins the decompression bomb
// boundary: a tiny compressed body can expand to a huge XML document, and the
// parser would then hold both the compressed bytes and the expansion.
func TestInflateDanmakuRefusesAnOutputPastTheLimit(t *testing.T) {
	var raw bytes.Buffer
	zw, err := flate.NewWriter(&raw, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	// Highly compressible: well past the output limit from a small input.
	_, _ = zw.Write([]byte(strings.Repeat("A", maxDanmakuInflatedBytes+1024)))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if raw.Len() >= maxDanmakuInflatedBytes {
		t.Fatalf("test fixture is %d bytes compressed; it must be smaller than the limit to prove expansion matters", raw.Len())
	}

	if _, err := inflateDanmaku(raw.Bytes()); err == nil {
		t.Fatal("an over-limit danmaku body was inflated instead of refused")
	}
}

// TestInflateDanmakuAcceptsABodyAtTheLimit keeps the boundary from being so
// strict that a legitimately large danmaku document stops loading.
func TestInflateDanmakuAcceptsABodyAtTheLimit(t *testing.T) {
	body := []byte(strings.Repeat("A", maxDanmakuInflatedBytes))
	var raw bytes.Buffer
	zw, err := flate.NewWriter(&raw, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	out, err := inflateDanmaku(raw.Bytes())
	if err != nil {
		t.Fatalf("a body exactly at the limit was refused: %v", err)
	}
	if len(out) != len(body) {
		t.Fatalf("inflated %d bytes, want %d", len(out), len(body))
	}
}
