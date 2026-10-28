package flacdec_test

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/service/audio/flacdec"

	"github.com/mewkiz/flac/frame"
)

// The oracle for every test is the WAV the fixture was encoded from. FLAC is
// lossless, so the samples in the WAV are exactly the samples the decoder has to
// hand back; comparing against them needs no second decoder to agree with.

type wavFile struct {
	rate     int
	channels int
	bits     int
	samples  []int32 // interleaved
}

func readWAV(t *testing.T, path string) wavFile {
	t.Helper()
	data := readFile(t, path)
	if string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		t.Fatalf("%s: not a RIFF/WAVE file", path)
	}
	var w wavFile
	var dataLen uint32
	pos := 12
	for pos+8 <= len(data) {
		id := string(data[pos : pos+4])
		n := binary.LittleEndian.Uint32(data[pos+4:])
		body := data[pos+8 : pos+8+int(n)]
		switch id {
		case "fmt ":
			w.channels = int(binary.LittleEndian.Uint16(body[2:]))
			w.rate = int(binary.LittleEndian.Uint32(body[4:]))
			w.bits = int(binary.LittleEndian.Uint16(body[14:]))
		case "data":
			dataLen = n
			// samples holds one entry per sample per channel, so the count is the
			// data size over the width of one sample, not of one frame.
			width := w.bits / 8
			w.samples = make([]int32, n/uint32(width))
			for i := range w.samples {
				w.samples[i] = wavSample(body[i*width:], w.bits)
			}
		}
		pos += 8 + int(n) + int(n&1)
	}
	if uint32(len(w.samples))*uint32(w.bits/8) != dataLen {
		t.Fatalf("%s: %d samples for %d bytes of data", path, len(w.samples), dataLen)
	}
	return w
}

func wavSample(b []byte, bits int) int32 {
	switch bits {
	case 8:
		return int32(b[0]) - 128
	case 16:
		return int32(int16(binary.LittleEndian.Uint16(b)))
	case 24:
		v := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16
		return int32(v<<8) >> 8 // sign extend
	default:
		panic("unsupported width")
	}
}

// expected is the float32 a decoder must produce for sample v: v divided by half
// the range, which is the convention MyGo's own WAV decoder uses, so a WAV and a
// FLAC of the same recording land on the same float32.
func expected(v int32, bits int) float32 {
	return float32(v) / float32(int64(1)<<uint(bits-1))
}

func open(t *testing.T, name string) *flacdec.Decoder {
	t.Helper()
	d, err := flacdec.OpenFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// readAll decodes everything the decoder will give.
//
// 这一份**留**下样本：真正逐样本比对解码正确性的用例走它。
func readAll(t *testing.T, d *flacdec.Decoder) []float32 {
	t.Helper()
	ch := d.Channels()
	buf := make([]float32, 4096*ch)
	var out []float32
	for {
		n, err := d.Read(buf)
		out = append(out, buf[:n]...)
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("decode ended with %v, want io.EOF", err)
		}
		if n == 0 {
			t.Fatal("decode made no progress")
		}
	}
}

// readSome decodes at most frames frames, for the seek tests.
func readSome(t *testing.T, d *flacdec.Decoder, frames int) []float32 {
	t.Helper()
	ch := d.Channels()
	buf := make([]float32, 1024*ch)
	var out []float32
	for len(out)/ch < frames {
		n, err := d.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			if err != io.EOF {
				t.Fatalf("decode: %v", err)
			}
			break
		}
		if n == 0 {
			t.Fatal("decode made no progress")
		}
	}
	return out
}

// readAllErr 把解码器读到完，报读出来的**帧数**和最后一个错误。
//
// # 为什么它不留样本
//
// 留样本这一族里最贵的是截断那几个用例：同一个 fixture 的**每一个**帧边界都要
// 新开一个解码器把那个前缀整个解一遍（stereo16 四十三个边界，平均每个解二十来
// 帧），而它们要的只是「解到底之后是什么错」。照 `readAll` 那样把样本 append
// 下来，「攒样本」这一件事自己就是 O(n²)：光追加就写掉上百万个 float32，每次
// 扩容还要把已有的整段拷一遍。`-race` 下这一笔和解码本身一个量级——而它一个字
// 都验不了。
//
// 帧数够那些调用点用：判空（「一帧都没出」）和判错，两样都只需要它。
func readAllErr(d *flacdec.Decoder) (frames int, err error) {
	ch := d.Channels()
	buf := make([]float32, 4096*ch)
	for {
		n, err := d.Read(buf)
		frames += n / ch
		if err != nil {
			return frames, err
		}
		if n == 0 {
			return frames, io.ErrNoProgress
		}
	}
}

// frameOffsets returns where each frame of a bare FLAC stream starts, by parsing
// the file with the frame parser directly, without the decoder under test.
//
// The last entry is where parsing ran out, which is the end of the stream rather
// than a frame boundary; a caller cutting at it cuts the whole track in half at
// the end of the last complete frame.
func frameOffsets(t *testing.T, data []byte) []int64 {
	t.Helper()
	if string(data[:4]) != "fLaC" {
		t.Fatal("not a bare FLAC stream")
	}
	r := bytes.NewReader(data)
	br := bufio.NewReader(r)
	// Where the stream has got to is what the underlying reader has handed over
	// less what is still sitting in the buffer. The buffer reads ahead by up to its
	// own size, so subtracting the buffered count from the reader's own offset is
	// the only way to get a file offset out of a buffered reader.
	at := func() int64 { return r.Size() - int64(r.Len()) - int64(br.Buffered()) }
	if _, err := br.Discard(4); err != nil {
		t.Fatal(err)
	}
	for {
		h, err := br.Peek(4)
		if err != nil {
			t.Fatal(err)
		}
		last := h[0]&0x80 != 0
		n := int(h[1])<<16 | int(h[2])<<8 | int(h[3])
		if _, err := br.Discard(4 + n); err != nil {
			t.Fatal(err)
		}
		if last {
			break
		}
	}
	var offsets []int64
	for {
		offsets = append(offsets, at())
		if _, err := frame.Parse(br); err != nil {
			return offsets
		}
	}
}

// mediaOffset returns an offset inside a file's media data, where a flipped byte
// damages audio rather than metadata.
func mediaOffset(t *testing.T, data []byte) int {
	t.Helper()
	if string(data[:4]) == "fLaC" {
		return firstFrame(t, data) + 40
	}
	// In an MP4 the media data is the mdat box; FFmpeg puts it after the header.
	i := bytes.Index(data[:min(len(data), 1<<20)], []byte("mdat"))
	if i < 0 {
		// A file whose mdat comes late still has one somewhere; take the middle.
		return len(data) / 2
	}
	return i + 8 + 40
}
