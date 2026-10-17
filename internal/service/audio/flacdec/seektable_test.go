package flacdec_test

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/audio/flacdec"

	"github.com/mewkiz/flac/frame"
)

// TestSeekTable covers the fast path a bare-FLAC seek takes when the file has a
// SEEKTABLE block: the encoder names the frame boundaries, so the seek is a file
// seek and nothing is decoded.
//
// ffmpeg does not write one, so the fixture is built here: the frames are walked
// to find their real boundaries, a table is written naming them, and the block is
// spliced in. Offsets in a seek table are relative to the first frame header, so
// growing the header does not invalidate them.
func TestSeekTable(t *testing.T) {
	data := readFile(t, "testdata/stereo16.flac")
	withTable := addSeekTable(t, data, 1.0)

	t.Run("seeks land where they say", func(t *testing.T) {
		checkSeeks(t, withTable)
	})

	t.Run("a wrong offset falls back to the index", func(t *testing.T) {
		broken := addSeekTable(t, data, 1.0)
		corruptSeekTable(t, broken, func(p []byte) { binary.BigEndian.PutUint64(p[8:], 1<<20) })
		checkSeeks(t, broken)
	})

	t.Run("a wrong sample number falls back to the index", func(t *testing.T) {
		broken := addSeekTable(t, data, 1.0)
		corruptSeekTable(t, broken, func(p []byte) { binary.BigEndian.PutUint64(p[:8], 999999) })
		checkSeeks(t, broken)
	})

	t.Run("a table of nothing but placeholders is ignored", func(t *testing.T) {
		broken := addSeekTable(t, data, 1.0)
		corruptSeekTable(t, broken, func(p []byte) { binary.BigEndian.PutUint64(p[:8], 0xFFFFFFFFFFFFFFFF) })
		d, err := flacdec.New(broken)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		checkSeeksOn(t, d, broken)
	})
}

// checkSeeks opens data and runs the shared seek checks on it.
func checkSeeks(t *testing.T, data []byte) {
	t.Helper()
	d, err := flacdec.New(data)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	checkSeeksOn(t, d, data)
}

// checkSeeksOn seeks to frames spread across a decoder and compares what comes
// back against the oracle, which is the WAV the fixture was encoded from.
func checkSeeksOn(t *testing.T, d *flacdec.Decoder, data []byte) {
	t.Helper()
	src := readWAV(t, "testdata/stereo16.wav")
	ch := d.Channels()
	total := int64(len(src.samples) / ch)
	for _, target := range []int64{1, 4096, 30000, total / 2, total - 20000, total - 4609, total - 1} {
		if err := d.SetPosition(target); err != nil {
			t.Fatalf("seek to %d: %v", target, err)
		}
		const n = 4096
		got := readSome(t, d, n)
		want := make([]float32, 0, n*ch)
		for f := target; f < target+n && f < total; f++ {
			for c := 0; c < ch; c++ {
				want = append(want, expected(src.samples[f*int64(ch)+int64(c)], src.bits))
			}
		}
		compare(t, "seek", ch, got, want)
	}
}

// addSeekTable returns data with a SEEKTABLE block spliced in after STREAMINFO,
// naming one point every seconds of audio and padding the rest with placeholders,
// which is what the flac command line does.
func addSeekTable(t *testing.T, data []byte, seconds float64) []byte {
	t.Helper()
	head, frames := splitBareFLAC(t, data)

	type point struct{ sample, offset uint64 }
	var points []point
	br := bufio.NewReader(bytes.NewReader(frames))
	var sample uint64
	for {
		off := int64(len(frames) - br.Buffered())
		f, err := frame.Parse(br)
		if err != nil {
			break
		}
		points = append(points, point{sample: f.SampleNumber(), offset: uint64(off)})
		sample += uint64(len(f.Subframes[0].Samples))
	}
	if len(points) < 2 {
		t.Fatalf("only %d frames in the fixture", len(points))
	}

	// One point per interval of audio. Every point is 18 bytes, so the block is a
	// whole number of them without any padding, which is what the format wants.
	step := uint64(44100 * seconds)
	var body []byte
	next := uint64(0)
	for _, p := range points {
		if p.sample < next {
			continue
		}
		var pt [18]byte
		binary.BigEndian.PutUint64(pt[:8], p.sample)
		binary.BigEndian.PutUint64(pt[8:16], p.offset)
		binary.BigEndian.PutUint16(pt[16:], uint16(4608))
		body = append(body, pt[:]...)
		next = p.sample + step
	}
	if len(body) == 0 {
		t.Fatal("the seek table came out empty")
	}

	var out []byte
	out = append(out, "fLaC"...)
	out = append(out, metaBlock(false, 0, head.streamInfo)...) // STREAMINFO
	out = append(out, metaBlock(true, 3, body)...)             // SEEKTABLE, the last block
	out = append(out, frames...)
	return out
}

// metaBlock writes a metadata block: the last-block flag and type in one byte,
// the length in three, then the body.
func metaBlock(last bool, typ byte, body []byte) []byte {
	b := []byte{typ}
	if last {
		b[0] |= 0x80
	}
	var n [3]byte
	n[0] = byte(len(body) >> 16)
	n[1] = byte(len(body) >> 8)
	n[2] = byte(len(body))
	b = append(b, n[:]...)
	return append(b, body...)
}

type bareHeader struct {
	streamInfo []byte
}

// splitBareFLAC separates a bare FLAC stream into its STREAMINFO block and its
// frames, dropping the blocks that only carry tags.
func splitBareFLAC(t *testing.T, data []byte) (bareHeader, []byte) {
	t.Helper()
	if string(data[:4]) != "fLaC" {
		t.Fatal("not a bare FLAC stream")
	}
	var h bareHeader
	pos := 4
	for {
		last := data[pos]&0x80 != 0
		typ := data[pos] & 0x7f
		n := int(data[pos+1])<<16 | int(data[pos+2])<<8 | int(data[pos+3])
		if typ == 0 && h.streamInfo == nil {
			h.streamInfo = data[pos+4 : pos+4+n]
		}
		pos += 4 + n
		if last {
			return h, data[pos:]
		}
	}
}

// corruptSeekTable rewrites the first seek point of a stream built by
// addSeekTable.
func corruptSeekTable(t *testing.T, data []byte, damage func(point []byte)) {
	t.Helper()
	// STREAMINFO, then the SEEKTABLE the builder wrote.
	pos := 4
	siLen := int(data[pos+1])<<16 | int(data[pos+2])<<8 | int(data[pos+3])
	pos += 4 + siLen
	typ := data[pos] & 0x7f
	if typ != 3 {
		t.Fatalf("expected a seek table block, got type %d", typ)
	}
	n := int(data[pos+1])<<16 | int(data[pos+2])<<8 | int(data[pos+3])
	if n < 18 {
		t.Fatalf("seek table of %d bytes", n)
	}
	damage(data[pos+4 : pos+4+18])
}

// TestSeekTableIgnoredIsStillCorrect checks that a stream whose seek table is
// unusable still decodes and seeks correctly, because the index is always there
// behind it.
func TestSeekTableIgnoredIsStillCorrect(t *testing.T) {
	// A seek table that names a point far past the end of the audio is the case
	// real files have: the flac command line writes one for a stream it later
	// truncated.
	data := readFile(t, "testdata/stereo16.flac")
	broken := addSeekTable(t, data, 1.0)
	corruptSeekTable(t, broken, func(p []byte) {
		binary.BigEndian.PutUint64(p[8:], 1<<20) // past the end of the file
	})
	d, err := flacdec.New(broken)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.SetPosition(100000); err != nil {
		t.Errorf("seek to 100000: %v", err)
	}
	if n, err := readAllErr(d); errors.Is(err, io.EOF) && n == 0 {
		t.Errorf("seek landed on nothing")
	}
}
