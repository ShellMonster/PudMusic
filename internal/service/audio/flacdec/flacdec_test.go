package flacdec_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/audio/flacdec"

	"github.com/mewkiz/flac/frame"
	m4a "github.com/tphakala/go-m4a"
)

// Fixtures: a WAV whose samples are the oracle, the bare FLAC and the MP4-FLAC
// ffmpeg encoded from it, and the 24-bit MP4 the t-0015 audit probe used.
var stereoFixtures = []string{"stereo16.flac", "stereo16.mp4", "stereo24.flac", "stereo24.mp4"}

// TestInterleaving is the test the t-0013 reference implementation fails. The two
// channels hold different signals, so a decoder that hands the mixer all of the
// left channel followed by all of the right cannot pass it, and every sample is
// compared rather than an RMS of them.
func TestInterleaving(t *testing.T) {
	for _, c := range []struct{ wav, flac, mp4 string }{
		{"stereo16.wav", "stereo16.flac", "stereo16.mp4"},
		{"stereo24.wav", "stereo24.flac", "stereo24.mp4"},
	} {
		t.Run(c.flac, func(t *testing.T) {
			src := readWAV(t, filepath.Join("testdata", c.wav))
			want := make([]float32, len(src.samples))
			for i, v := range src.samples {
				want[i] = expected(v, src.bits)
			}
			for _, name := range []string{c.flac, c.mp4} {
				d := open(t, name)
				got := readAll(t, d)
				if n := len(got) / src.channels; n != len(want)/src.channels {
					t.Errorf("%s: decoded %d frames, want %d", name, n, len(want)/src.channels)
				}
				compare(t, name, d.Channels(), got, want)
			}
		})
	}
}

// TestLeftRightDiffer guards the fixture itself: if the two channels ever became
// the same signal, TestInterleaving would stop being able to fail.
func TestLeftRightDiffer(t *testing.T) {
	src := readWAV(t, "testdata/stereo16.wav")
	for i := 0; i < len(src.samples); i += src.channels {
		if src.samples[i] == src.samples[i+1] {
			t.Fatalf("channels are identical at frame %d; the fixture cannot catch interleaving", i/src.channels)
		}
	}
}

func TestMono(t *testing.T) {
	src := readWAV(t, "testdata/mono16.wav")
	want := make([]float32, len(src.samples))
	for i, v := range src.samples {
		want[i] = expected(v, src.bits)
	}
	for _, name := range []string{"mono16.flac", "mono16.mp4"} {
		d := open(t, name)
		if d.Channels() != 1 {
			t.Errorf("%s: %d channels, want 1", name, d.Channels())
		}
		compare(t, name, 1, readAll(t, d), want)
	}
}

// TestFormat covers the fields a player reads before playing anything: the
// sample rate drives the mixer, the channel count the layout, the bit depth the
// scale, and Len the progress bar.
func TestFormat(t *testing.T) {
	for _, c := range []struct{ wav, flac, mp4 string }{
		{"stereo16.wav", "stereo16.flac", "stereo16.mp4"},
		{"stereo24.wav", "stereo24.flac", "stereo24.mp4"},
		{"mono16.wav", "mono16.flac", "mono16.mp4"},
	} {
		src := readWAV(t, filepath.Join("testdata", c.wav))
		want := int64(len(src.samples) / src.channels)
		for _, name := range []string{c.flac, c.mp4} {
			d := open(t, name)
			if got := d.SampleRate(); got != src.rate {
				t.Errorf("%s: %d Hz, want %d", name, got, src.rate)
			}
			if got := d.Channels(); got != src.channels {
				t.Errorf("%s: %d channels, want %d", name, got, src.channels)
			}
			if got := d.BitsPerSample(); got != src.bits {
				t.Errorf("%s: %d bits, want %d", name, got, src.bits)
			}
			if got := d.Len(); got != want {
				t.Errorf("%s: Len %d, want %d", name, got, want)
			}
		}
	}
}

// TestSeekAccuracy seeks to frames spread across each fixture and checks the
// samples that come back, forward and backward, and that a seek from a seeked
// position lands just as well.
func TestSeekAccuracy(t *testing.T) {
	for _, name := range append(stereoFixtures, "mono16.flac", "mono16.mp4") {
		t.Run(name, func(t *testing.T) {
			src := readWAV(t, wavOf(name))
			d := open(t, name)
			ch := d.Channels()
			total := int64(len(src.samples) / ch)
			targets := []int64{
				1, 4095, 4096, 4097, total / 3, total / 2, total - 4609, total - 4097, total - 1,
			}
			// Descending, so every seek has to move backwards over ground the
			// decoder has already walked.
			for i := len(targets) - 1; i >= 0; i-- {
				target := targets[i]
				if err := d.SetPosition(target); err != nil {
					t.Fatalf("%s: seek to %d: %v", name, target, err)
				}
				const n = 8192
				got := readSome(t, d, n)
				want := make([]float32, 0, n*ch)
				for f := target; f < target+n && f < total; f++ {
					for c := 0; c < ch; c++ {
						want = append(want, expected(src.samples[f*int64(ch)+int64(c)], src.bits))
					}
				}
				compare(t, fmt.Sprintf("%s seek %d", name, target), ch, got, want)
			}
		})
	}
}

// TestSeekEdges covers the boundary of SetPosition: the start, the exact end,
// past the end, before the start, and reading after seeking to the end.
func TestSeekEdges(t *testing.T) {
	for _, name := range append(stereoFixtures, "mono16.flac", "mono16.mp4") {
		t.Run(name, func(t *testing.T) {
			d := open(t, name)
			total := d.Len()
			if total < 2 {
				t.Fatalf("%s: Len %d", name, total)
			}
			if err := d.SetPosition(0); err != nil {
				t.Errorf("seek to 0: %v", err)
			}
			if n, err := d.Read(make([]float32, 64)); n == 0 || err != nil {
				t.Errorf("read after seek to 0: n=%d err=%v", n, err)
			}
			// The end itself is a legal position: it reads as ended, not as an
			// error, which is what MyGo's own WAV decoder does.
			if err := d.SetPosition(total); err != nil {
				t.Errorf("seek to Len (%d): %v", total, err)
			}
			n, err := d.Read(make([]float32, 64))
			if n != 0 || err != io.EOF {
				t.Errorf("read after seek to Len: n=%d err=%v, want 0 and io.EOF", n, err)
			}
			if _, err := d.Read(make([]float32, 64)); err != io.EOF {
				t.Errorf("second read after seek to Len: %v, want io.EOF", err)
			}
			if err := d.SetPosition(total + 1); !errors.Is(err, flacdec.ErrSeekRange) {
				t.Errorf("seek past Len: %v, want ErrSeekRange", err)
			}
			if err := d.SetPosition(-1); !errors.Is(err, flacdec.ErrSeekRange) {
				t.Errorf("seek before 0: %v, want ErrSeekRange", err)
			}
			// A refused seek leaves the decoder where it was, so it still ends.
			if _, err := d.Read(make([]float32, 64)); err != io.EOF {
				t.Errorf("read after a refused seek: %v, want io.EOF", err)
			}
			// Back to the start works, which is the case a progress bar drags into.
			if err := d.SetPosition(0); err != nil {
				t.Errorf("seek back to 0: %v", err)
			}
			if n, err := d.Read(make([]float32, 64)); n == 0 || err != nil {
				t.Errorf("read after seeking back: n=%d err=%v", n, err)
			}
		})
	}
}

// TestEOF pins the end of a whole decode: the last frames are delivered and the
// read after them reports io.EOF rather than repeating them.
func TestEOF(t *testing.T) {
	for _, name := range append(stereoFixtures, "mono16.flac", "mono16.mp4") {
		d := open(t, name)
		ch := d.Channels()
		if n := len(readAll(t, d)) / ch; int64(n) != d.Len() {
			t.Errorf("%s: decoded %d frames, Len says %d", name, n, d.Len())
		}
		n, err := d.Read(make([]float32, 512*ch))
		if n != 0 || err != io.EOF {
			t.Errorf("%s: read past the end: n=%d err=%v, want 0 and io.EOF", name, n, err)
		}
		if _, err := d.Read(make([]float32, 512*ch)); err != io.EOF {
			t.Errorf("%s: second read past the end: %v, want io.EOF", name, err)
		}
	}
}

// TestTruncated cuts each fixture short at a range of byte offsets and requires
// an error rather than a clean end: a partial download must never play as if it
// were the whole track.
func TestTruncated(t *testing.T) {
	for _, name := range append(stereoFixtures, "mono16.flac", "mono16.mp4") {
		t.Run(name, func(t *testing.T) {
			full := readFile(t, filepath.Join("testdata", name))
			for _, cut := range []float64{0.1, 0.3, 0.5, 0.75, 0.9, 0.99, 0.999} {
				n := int(float64(len(full)) * cut)
				d, err := flacdec.New(full[:n])
				if err != nil {
					continue // refusing to open a cut file is a fine answer
				}
				_, err = readAllErr(d)
				if err == nil || err == io.EOF {
					t.Errorf("%s cut to %d of %d bytes: ended cleanly (%v), want an error",
						name, n, len(full), err)
				}
				d.Close()
			}
		})
	}
}

// TestTruncatedAtFrameBoundary cuts a bare FLAC stream exactly between two
// frames, which a byte-level cut rarely hits. Every frame before the cut is
// intact, so nothing in the stream is malformed and only the sample count
// STREAMINFO declared can tell a short file from a whole one.
func TestTruncatedAtFrameBoundary(t *testing.T) {
	for _, name := range []string{"stereo16.flac", "stereo24.flac", "mono16.flac"} {
		t.Run(name, func(t *testing.T) {
			full := readFile(t, filepath.Join("testdata", name))
			bounds := frameOffsets(t, full)
			if len(bounds) < 3 {
				t.Fatalf("%s: only %d frames", name, len(bounds))
			}
			// bounds[0] cuts nothing, and the last entry is where parsing ran out,
			// which is the end of the stream rather than a frame boundary: cutting
			// there leaves the whole track, which must decode cleanly.
			for _, off := range bounds[1 : len(bounds)-1] {
				if off <= 0 || off >= int64(len(full)) {
					t.Fatalf("%s: frame boundary %d is outside the file", name, off)
				}
				d, err := flacdec.New(full[:off])
				if err != nil {
					t.Fatalf("%s cut at %d: %v", name, off, err)
				}
				_, err = readAllErr(d)
				// Nothing in the stream is malformed, so only the sample count
				// STREAMINFO declared can tell this from a whole track. That is the
				// error this test exists to pin: a clean io.EOF here would let a
				// half-downloaded track play as if it were finished.
				if !errors.Is(err, flacdec.ErrTruncated) {
					t.Errorf("%s cut at frame boundary %d: %v, want ErrTruncated", name, off, err)
				}
				d.Close()
			}
		})
	}
}

// TestDamagedFrame flips one byte inside a frame: the frame's CRC-16 has to catch
// it rather than the decoder passing noise on.
func TestDamagedFrame(t *testing.T) {
	for _, name := range append(stereoFixtures, "mono16.flac") {
		full := readFile(t, filepath.Join("testdata", name))
		off := mediaOffset(t, full)
		damaged := append([]byte{}, full...)
		damaged[off] ^= 0xff
		d, err := flacdec.New(damaged)
		if err != nil {
			continue // refusing to open is a fine answer too
		}
		if _, err := readAllErr(d); err == nil || err == io.EOF {
			t.Errorf("%s: a flipped byte at offset %d decoded cleanly: %v", name, off, err)
		}
		d.Close()
	}
}

// TestNotFLAC reports the formats this package is not for, rather than guessing.
// The contract is per-codec, not per-container: AAC in an MP4 is not FLAC in an
// MP4, and an ADTS .aac stream is a third thing again.
func TestNotFLAC(t *testing.T) {
	if _, err := flacdec.New(readFile(t, "testdata/stereo16.wav")); !errors.Is(err, flacdec.ErrFormat) {
		t.Errorf("WAV: %v, want ErrFormat", err)
	}
	if _, err := flacdec.New(buildAACMP4(t)); !errors.Is(err, flacdec.ErrFormat) {
		t.Errorf("AAC in MP4: %v, want ErrFormat", err)
	}
	if _, err := flacdec.New([]byte("OggS not flac at all........")); !errors.Is(err, flacdec.ErrFormat) {
		t.Errorf("Ogg: %v, want ErrFormat", err)
	}
	if _, err := flacdec.New(nil); !errors.Is(err, flacdec.ErrFormat) {
		t.Errorf("empty: %v, want ErrFormat", err)
	}
	// A bare FLAC stream with no STREAMINFO is not a FLAC stream.
	noInfo := append([]byte("fLaC"), 0x81, 0, 0, 34)
	if _, err := flacdec.New(noInfo); err == nil {
		t.Error("a truncated STREAMINFO block opened without complaint")
	}
}

// buildAACMP4 muxes a tiny silent AAC-LC track, the shape a Bilibili AAC stream
// has. go-m4a writes it.
func buildAACMP4(t *testing.T) []byte {
	t.Helper()
	cfg := m4a.WriterConfig{SampleRate: 48000, Channels: 2, ASC: []byte{0x11, 0x90}, EncoderDelay: m4a.NoEdit}
	data, err := m4a.InitSegment(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fw, err := m4a.NewFragmentWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := fw.WriteFrame(make([]byte, 1024)); err != nil {
			t.Fatal(err)
		}
		if data, err = fw.AppendSegment(data); err != nil {
			t.Fatal(err)
		}
	}
	return data
}

// TestOpenAtOffset covers a decoder reading a stream that does not start at 0.
func TestOpenAtOffset(t *testing.T) {
	data := readFile(t, "testdata/stereo16.flac")
	r := bytes.NewReader(append(make([]byte, 1000), data...))
	if _, err := r.Seek(1000, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	d, err := flacdec.Open(r)
	if err != nil {
		t.Fatalf("open at an offset: %v", err)
	}
	defer d.Close()
	if d.SampleRate() != 44100 || d.Channels() != 2 {
		t.Fatalf("opened at an offset but read as %d Hz, %d channels", d.SampleRate(), d.Channels())
	}
	if n, err := readAllErr(d); err != io.EOF || n == 0 {
		t.Fatalf("decode from an offset: %d frames, %v", n, err)
	}
}

// TestNewInMemory covers a track already downloaded into memory.
func TestNewInMemory(t *testing.T) {
	d, err := flacdec.New(readFile(t, "testdata/stereo24.flac"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if d.BitsPerSample() != 24 {
		t.Errorf("%d bits, want 24", d.BitsPerSample())
	}
	if _, err := readAllErr(d); err != io.EOF {
		t.Fatalf("decode from memory: %v", err)
	}
}

// TestReadLengths checks that Read fills whole frames and never reports a count
// its caller cannot use: MyGo's stream subtracts n % Channels.
func TestReadLengths(t *testing.T) {
	d := open(t, "stereo16.flac")
	ch := d.Channels()
	for _, size := range []int{0, 1, 2, 3, 7, 8, 4095, 4096, 8192} {
		buf := make([]float32, size)
		n, err := d.Read(buf)
		if err != nil && err != io.EOF {
			t.Fatalf("Read(%d): %v", size, err)
		}
		if n%ch != 0 {
			t.Errorf("Read(%d) returned %d samples, not a whole number of frames", size, n)
		}
		if n > size {
			t.Errorf("Read(%d) wrote %d samples", size, n)
		}
		if size == 0 && n != 0 {
			t.Errorf("Read(0) wrote %d samples", n)
		}
	}
}

// TestReferenceFixture decodes the fixture the t-0015 audit probe used, so the
// exact sample it complained about stays pinned: the reference implementation
// read sample 1 as -0.00059986115 where the interleaved value is
// -0.00036549568.
func TestReferenceFixture(t *testing.T) {
	d := open(t, "reference-24bit-mp4.m4a")
	if d.SampleRate() != 48000 || d.Channels() != 2 || d.BitsPerSample() != 24 {
		t.Fatalf("format: %d Hz, %d channels, %d bits", d.SampleRate(), d.Channels(), d.BitsPerSample())
	}
	got := make([]float32, 8)
	if _, err := d.Read(got); err != nil {
		t.Fatal(err)
	}
	want := oracleFirstFrame(t, "testdata/reference-24bit-mp4.m4a", 8)
	for i := range want {
		if got[i] != expected(want[i], 24) {
			t.Fatalf("sample %d: got %v, want %v (the order has to be L0,R0,L1,R1,...)",
				i, got[i], expected(want[i], 24))
		}
	}
	if got[0] == got[1] {
		t.Fatal("the first two samples are the same, so this fixture cannot catch interleaving")
	}
}

// oracleFirstFrame decodes the first access unit of an MP4-FLAC file with the
// container and frame parsers directly, so the expectation does not come from
// the decoder under test.
func oracleFirstFrame(t *testing.T, path string, n int) []int32 {
	t.Helper()
	rd, err := m4a.NewReader(bytes.NewReader(readFile(t, path)))
	if err != nil {
		t.Fatal(err)
	}
	au, err := rd.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	f, err := frame.Parse(bytes.NewReader(au))
	if err != nil {
		t.Fatal(err)
	}
	var out []int32
	for i := 0; len(out) < n; i++ {
		for c := range f.Subframes {
			out = append(out, f.Subframes[c].Samples[i])
		}
	}
	return out[:n]
}

// firstFrame returns where the first frame of a bare FLAC stream starts.
func firstFrame(t *testing.T, data []byte) int {
	t.Helper()
	if string(data[:4]) != "fLaC" {
		t.Fatal("not a bare FLAC stream")
	}
	i := 4
	for {
		last := data[i]&0x80 != 0
		n := int(data[i+1])<<16 | int(data[i+2])<<8 | int(data[i+3])
		i += 4 + n
		if last {
			return i
		}
	}
}

func wavOf(name string) string {
	return filepath.Join("testdata", strings.TrimSuffix(name, filepath.Ext(name))+".wav")
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// compare reports the first differing sample, which is what a stereo mix-up looks
// like: sample 1 holding the second left sample instead of the first right one.
func compare(t *testing.T, what string, ch int, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d samples, want %d", what, len(got), len(want))
	}
	for i := range want {
		if got[i] == want[i] {
			continue
		}
		// Report how far the first difference is from the frame start, which
		// distinguishes a swapped pair from a wholly wrong channel.
		t.Fatalf("%s: %d of %d samples differ; first at %d (frame %d, channel %d): got %v, want %v",
			what, countDiffs(got, want), len(want), i, i/ch, i%ch, got[i], want[i])
	}
}

func countDiffs(got, want []float32) int {
	n := 0
	for i := range want {
		if got[i] != want[i] {
			n++
		}
	}
	return n
}
