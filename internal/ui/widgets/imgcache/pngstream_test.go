package imgcache

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"testing"

	xdraw "golang.org/x/image/draw"
)

func streamPNG(t testing.TB, w, h int, alpha bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			a := byte(255)
			if alpha {
				a = byte(x*19 + y*7)
			}
			img.SetNRGBA(x, y, color.NRGBA{R: byte(x * 31), G: byte(y * 17), B: byte(x ^ y), A: a})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func assertPNGScale(t *testing.T, data []byte) {
	t.Helper()
	wantSrc, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	b := wantSrc.Bounds()
	want := image.NewRGBA(storedBounds(b.Dx(), b.Dy()))
	xdraw.ApproxBiLinear.Scale(want, want.Bounds(), wantSrc, b, xdraw.Src, nil)
	got, handled, err := decodePNGStored(data)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if got.Bounds() != want.Bounds() || !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("streaming output differs from standard decoder + existing scaler")
	}
}

func TestStreamingPNGMatchesExistingPixels(t *testing.T) {
	for _, size := range []image.Point{{1920, 1080}, {1081, 1921}, {769, 777}, {1024, 1}, {1, 1024}} {
		for _, alpha := range []bool{false, true} {
			assertPNGScale(t, streamPNG(t, size.X, size.Y, alpha))
		}
	}
}

func streamChunk(kind string, data []byte) []byte {
	out := make([]byte, len(data)+12)
	binary.BigEndian.PutUint32(out, uint32(len(data)))
	copy(out[4:8], kind)
	copy(out[8:], data)
	binary.BigEndian.PutUint32(out[len(out)-4:], crc32.ChecksumIEEE(out[4:len(out)-4]))
	return out
}

// Force each PNG filter, split the compressed stream into tiny IDATs, and use
// image/png as an independent oracle for both unfiltering and pixel conversion.
func TestStreamingPNGFiltersAndChunks(t *testing.T) {
	for filter := byte(0); filter < 5; filter++ {
		const w, h, bpp = 800, 9, 4
		raw := make([]byte, (w*bpp+1)*h)
		previous := make([]byte, w*bpp)
		for y := range h {
			row := make([]byte, w*bpp)
			for i := range row {
				row[i] = byte(i*7 + y*11)
			}
			o := y * (w*bpp + 1)
			raw[o] = filter
			for i, v := range row {
				a, b, c := 0, int(previous[i]), 0
				if i >= bpp {
					a, c = int(row[i-bpp]), int(previous[i-bpp])
				}
				predict := 0
				switch filter {
				case 1:
					predict = a
				case 2:
					predict = b
				case 3:
					predict = (a + b) / 2
				case 4:
					p := a + b - c
					distance := func(x int) int {
						if x < 0 {
							return -x
						}
						return x
					}
					predict = a
					if distance(p-b) < distance(p-predict) {
						predict = b
					}
					if distance(p-c) < distance(p-predict) {
						predict = c
					}
				}
				raw[o+1+i] = v - byte(predict)
			}
			previous = row
		}
		assertPNGScale(t, rawStreamPNG(w, h, raw, 7))
	}
}

func rawStreamPNG(w, h int, raw []byte, split int) []byte {
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header, uint32(w))
	binary.BigEndian.PutUint32(header[4:], uint32(h))
	header[8], header[9] = 8, 6
	out := append([]byte("\x89PNG\r\n\x1a\n"), streamChunk("IHDR", header)...)
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	for data := compressed.Bytes(); len(data) > 0; {
		n := min(len(data), split)
		out = append(out, streamChunk("IDAT", data[:n])...)
		data = data[n:]
	}
	return append(out, streamChunk("IEND", nil)...)
}

func TestStreamingPNGRejectsCorruption(t *testing.T) {
	valid := streamPNG(t, 800, 10, true)
	badCRC := bytes.Clone(valid)
	badCRC[len(badCRC)-1] ^= 1
	badFilter := make([]byte, (800*4+1)*10)
	badFilter[len(badFilter)-(800*4+1)] = 5 // final row must be validated too
	badZlib := rawStreamPNG(800, 10, make([]byte, (800*4+1)*10), 1<<20)
	end := 33 + 12 + int(binary.BigEndian.Uint32(badZlib[33:37]))
	badZlib[end-5] ^= 1 // corrupt zlib checksum, then repair the chunk CRC
	binary.BigEndian.PutUint32(badZlib[end-4:end], crc32.ChecksumIEEE(badZlib[37:end-4]))
	unknownCritical := append(bytes.Clone(valid[:33]), streamChunk("FAIL", nil)...)
	unknownCritical = append(unknownCritical, valid[33:]...)
	duplicateHeader := append(bytes.Clone(valid[:33]), valid[8:]...)
	for _, data := range [][]byte{
		valid[:len(valid)-1], badCRC, badZlib, unknownCritical, duplicateHeader,
		rawStreamPNG(800, 10, badFilter, 100),
		rawStreamPNG(800, 10, make([]byte, (800*4+1)*9), 100),
		rawStreamPNG(800, 10, make([]byte, (800*4+1)*11), 100),
	} {
		if _, handled, err := decodePNGStored(data); !handled || err == nil {
			t.Fatalf("corrupt PNG accepted: handled=%v err=%v", handled, err)
		}
		if _, err := decodeStored(data); err == nil {
			t.Fatal("cache must not publish corrupt image")
		}
	}
}

func TestStreamingPNGFallsBackForOtherImages(t *testing.T) {
	for _, img := range []image.Image{
		image.NewGray(image.Rect(0, 0, 800, 10)),
		image.NewNRGBA64(image.Rect(0, 0, 800, 10)),
		image.NewPaletted(image.Rect(0, 0, 800, 10), color.Palette{color.Black, color.White}),
		image.NewRGBA(image.Rect(0, 0, 16, 16)),
		image.NewRGBA(image.Rect(0, 0, 70000, 1)),
	} {
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			t.Fatal(err)
		}
		if _, handled, _ := decodePNGStored(b.Bytes()); handled {
			t.Fatal("unexpected streaming path")
		}
		if _, err := decodeStored(b.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := decodeStored(jpegBytes(t, 800, 450)); err != nil {
		t.Fatal(err)
	}
}

func TestStoredPNGDoesNotAllocateFullSizeImage(t *testing.T) {
	data := streamPNG(t, 1920, 1080, false)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	bm, err := decodeStored(data)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	w, h := bm.Size()
	if w != 768 || h != 432 {
		t.Fatalf("size = %dx%d", w, h)
	}
	// Output is 1.33 MB; allow ample room for zlib and two scanlines, but not
	// the former 8.3 MB original. Fixture creation is outside this interval.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 2<<20 {
		t.Fatalf("decode allocated %d bytes; full-sized source image returned", allocated)
	}
}

func BenchmarkStoredPNG(b *testing.B) {
	data := streamPNG(b, 1920, 1080, false)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := decodeStored(data); err != nil {
			b.Fatal(err)
		}
	}
}
