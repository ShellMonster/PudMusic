package mux

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/perf"
	"github.com/ShellMonster/PudMusic/internal/service/mux/muxtest"
)

// isolateGauges points this package's diagnostic gauges at a fresh collection
// for the duration of a test.
//
// The real ones are process-wide high-water marks: a test that read them would
// pass just because an earlier test in the package pushed them up. A private
// collection makes "did this run record it" an assertion instead of a hope.
func isolateGauges(t *testing.T) *perf.Gauges {
	t.Helper()
	prev := gauge
	g := &perf.Gauges{}
	gauge = g
	t.Cleanup(func() { gauge = prev })
	return g
}

// TestMuxRecordsTheLargestBoxItRead pins the box high-water mark: the snapshot
// has to say how large the biggest metadata box was, because that number is the
// one place an abnormal moov/moof shows up in memory accounting.
func TestMuxRecordsTheLargestBoxItRead(t *testing.T) {
	g := isolateGauges(t)

	dir := t.TempDir()
	spV := muxtest.VideoSpecWithKeyframe(30, 4096)
	spA := muxtest.AudioSpec(1)
	video, audio := writePair(t, spV, spA)
	if _, err := Mux(context.Background(), video, audio, filepath.Join(dir, "v.mp4")); err != nil {
		t.Fatal(err)
	}

	got := g.Snapshot().MP4MaxBoxBytes
	if got <= 0 {
		t.Fatal("mux 读完之后最大 box 还是 0——box 大小没有进快照")
	}
	// 这个数必须是文件里真实存在的那个 moov/moof 的大小：至少装得下采样表。
	if got < 512 {
		t.Fatalf("最大 box 是 %d 字节，比一个 moov 头还小——那个数不是这条流读出来的", got)
	}
}

// TestMuxRecordsTheLargestSampleItCopied pins the sample high-water mark: a
// keyframe larger than the copy buffer forces the interleaver to allocate a
// bigger one, and that is exactly what the snapshot's sample number describes.
func TestMuxRecordsTheLargestSampleItCopied(t *testing.T) {
	g := isolateGauges(t)

	// Bigger than the default copy buffer, so the growth path is the one that
	// ran. Everything else uses the fixture's small samples.
	const keyframe = 600 << 10
	dir := t.TempDir()
	spV := muxtest.VideoSpecWithKeyframe(30, keyframe)
	spA := muxtest.AudioSpec(1)
	video, audio := writePair(t, spV, spA)
	if _, err := Mux(context.Background(), video, audio, filepath.Join(dir, "v.mp4")); err != nil {
		t.Fatal(err)
	}

	got := g.Snapshot().MP4MaxSampleBytes
	if got < int64(keyframe) {
		t.Fatalf("最大采样是 %d 字节，而这一条流里有一个 %d 字节的关键帧", got, keyframe)
	}
	if want := int64(keyframe); got != want {
		t.Fatalf("最大采样是 %d 字节，want %d（这一条流里最大的就是那个关键帧）", got, want)
	}
}
