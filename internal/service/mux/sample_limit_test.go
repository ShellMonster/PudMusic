package mux

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/service/mux/muxtest"
)

// TestMuxRefusesASamplePastTheLimit pins the single-sample allocation boundary.
// A sample's size comes straight from the file's sample table, so a damaged or
// hostile file could otherwise ask the interleaver for an arbitrary buffer.
func TestMuxRefusesASamplePastTheLimit(t *testing.T) {
	dir := t.TempDir()
	// A little past the limit: the fixture stays cheap to build, and the
	// refusal must come from the size check rather than from running out of
	// memory.
	spV := muxtest.VideoSpecWithKeyframe(30, maxSampleBytes+4096)
	spA := muxtest.AudioSpec(1)
	video, audio := writePair(t, spV, spA)

	_, err := Mux(context.Background(), video, audio, filepath.Join(dir, "v.mp4"))
	if err == nil {
		t.Fatal("an over-limit sample was copied instead of refused")
	}
}

// TestMuxAcceptsASampleAtTheLimit keeps the boundary from rejecting legitimate
// large keyframes: a sample exactly at the limit is still valid.
func TestMuxAcceptsASampleAtTheLimit(t *testing.T) {
	dir := t.TempDir()
	spV := muxtest.VideoSpecWithKeyframe(30, maxSampleBytes)
	spA := muxtest.AudioSpec(1)
	video, audio := writePair(t, spV, spA)

	if _, err := Mux(context.Background(), video, audio, filepath.Join(dir, "v.mp4")); err != nil {
		t.Fatalf("a sample exactly at the limit was refused: %v", err)
	}
}
