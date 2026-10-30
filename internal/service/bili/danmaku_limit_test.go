package bili

import (
	"bytes"
	"compress/flate"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/perf"
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

// deflatedFixturePaddedTo returns a valid deflate stream padded with trailing
// bytes until it is exactly n bytes long. flate stops at the end of the stream
// and never reads the padding, so this is a legitimate n-byte input.
func deflatedFixturePaddedTo(t *testing.T, n int) []byte {
	t.Helper()
	var raw bytes.Buffer
	zw, err := flate.NewWriter(&raw, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write([]byte(`<i><d p="1.0,1,25,16777215,0,0,0,1,0">x</d></i>`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if raw.Len() > n {
		t.Fatalf("deflate fixture is %d bytes, larger than the %d-byte target", raw.Len(), n)
	}
	out := make([]byte, n)
	copy(out, raw.Bytes())
	return out
}

// TestInflateDanmakuAcceptsACompressedBodyAtTheLimit pins the input side of the
// boundary: the compressed body itself is bounded, because inflating a huge
// response means holding both the compressed bytes and the expansion.
func TestInflateDanmakuAcceptsACompressedBodyAtTheLimit(t *testing.T) {
	raw := deflatedFixturePaddedTo(t, maxDanmakuCompressedBytes)
	out, err := inflateDanmaku(raw)
	if err != nil {
		t.Fatalf("a compressed body exactly at the limit was refused: %v", err)
	}
	if !bytes.Contains(out, []byte("<d p=")) {
		t.Fatalf("inflated output does not contain the fixture's danmaku: %q", out)
	}
}

// TestInflateDanmakuRefusesACompressedBodyPastTheLimit is the other half: one
// byte over the input limit is refused with a recognizable error.
func TestInflateDanmakuRefusesACompressedBodyPastTheLimit(t *testing.T) {
	raw := deflatedFixturePaddedTo(t, maxDanmakuCompressedBytes+1)
	if _, err := inflateDanmaku(raw); err == nil {
		t.Fatal("an over-limit compressed body was inflated instead of refused")
	} else if !strings.Contains(err.Error(), "压缩") {
		t.Fatalf("the refusal does not say what was too large: %v", err)
	}
}

// TestInflateDanmakuRecordsItsByteGauges pins that both sides of the expansion
// reach the diagnostic snapshot: a danmaku body can be the single largest
// network payload the process handles, and the snapshot has to show it.
func TestInflateDanmakuRecordsItsByteGauges(t *testing.T) {
	before := perf.ProcessGauges().Snapshot()

	raw := deflatedFixturePaddedTo(t, 4096)
	out, err := inflateDanmaku(raw)
	if err != nil {
		t.Fatal(err)
	}

	after := perf.ProcessGauges().Snapshot()
	if after.DanmakuCompressedBytes < int64(len(raw)) {
		t.Errorf("compressed gauge = %d, want at least the %d-byte body",
			after.DanmakuCompressedBytes, len(raw))
	}
	if after.DanmakuInflatedBytes < int64(len(out)) {
		t.Errorf("inflated gauge = %d, want at least the %d-byte output",
			after.DanmakuInflatedBytes, len(out))
	}
	if after.DanmakuCompressedBytes < before.DanmakuCompressedBytes ||
		after.DanmakuInflatedBytes < before.DanmakuInflatedBytes {
		t.Errorf("gauges went backwards: before=%+v after=%+v", before, after)
	}
}
