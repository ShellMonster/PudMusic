package imgcache

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"math/rand/v2"
	"runtime"
	"testing"

	xdraw "golang.org/x/image/draw"
)

func coverJPEG(t testing.TB, w, h int, gray bool) []byte {
	t.Helper()
	rng := rand.New(rand.NewPCG(275, 503))
	var src image.Image
	if gray {
		m := image.NewGray(image.Rect(0, 0, w, h))
		for i := range m.Pix {
			m.Pix[i] = byte(rng.Uint32())
		}
		src = m
	} else {
		m := image.NewRGBA(image.Rect(0, 0, w, h))
		for i := range m.Pix {
			m.Pix[i] = byte(rng.Uint32())
			if i%4 == 3 {
				m.Pix[i] = 255
			}
		}
		src = m
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, src, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestStreamingJPEGMatchesExistingPixels(t *testing.T) {
	for _, size := range []image.Point{{1920, 1080}, {2560, 1440}, {1081, 1921}, {769, 777}, {1024, 1}, {1, 1024}, {800, 17}} {
		for _, gray := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/gray=%v", size.X, size.Y, gray), func(t *testing.T) {
				data := coverJPEG(t, size.X, size.Y, gray)
				src, err := jpeg.Decode(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				want := image.NewRGBA(storedBounds(size.X, size.Y))
				xdraw.ApproxBiLinear.Scale(want, want.Rect, src, src.Bounds(), xdraw.Src, nil)
				got, handled, err := decodeJPEGStored(data)
				if err != nil || !handled {
					t.Fatalf("handled=%v err=%v", handled, err)
				}
				if got.Rect != want.Rect || !bytes.Equal(got.Pix, want.Pix) {
					t.Fatal("streaming JPEG pixels differ from image/jpeg + ApproxBiLinear")
				}
			})
		}
	}
}

func TestStoredJPEGDoesNotAllocateFullSizeImage(t *testing.T) {
	for _, size := range []image.Point{{1920, 1080}, {2560, 1440}} {
		data := coverJPEG(t, size.X, size.Y, false)
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		bm, err := decodeStored(data)
		runtime.ReadMemStats(&after)
		if err != nil {
			t.Fatal(err)
		}
		runtime.KeepAlive(bm)
		allocated := after.TotalAlloc - before.TotalAlloc
		t.Logf("%v allocated %d bytes", size, allocated)
		if allocated > 2<<20 {
			t.Fatalf("allocated %d bytes: full-size JPEG source returned", allocated)
		}
	}
}

func TestStreamingJPEGRejectsTruncation(t *testing.T) {
	data := coverJPEG(t, 800, 49, false)
	for _, n := range []int{len(data) / 2, len(data) - 100, len(data) - 2, len(data) - 1} {
		_, handled, err := decodeJPEGStored(data[:n])
		if !handled || err == nil {
			t.Fatalf("truncated at %d: handled=%v err=%v", n, handled, err)
		}
	}
}

func TestJPEGEXIFOnlyRotationsBypassScaling(t *testing.T) {
	data := coverJPEG(t, 800, 450, false)
	for _, bo := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for o := 0; o <= 9; o++ {
			tiff := make([]byte, 26)
			copy(tiff, "II")
			if bo == binary.BigEndian {
				copy(tiff, "MM")
			}
			bo.PutUint16(tiff[2:], 42)
			bo.PutUint32(tiff[4:], 8)
			bo.PutUint16(tiff[8:], 1)
			bo.PutUint16(tiff[10:], 0x112)
			bo.PutUint16(tiff[12:], 3)
			bo.PutUint32(tiff[14:], 1)
			bo.PutUint16(tiff[18:], uint16(o))
			seg := append([]byte{0xff, 0xe1, 0, 34, 'E', 'x', 'i', 'f', 0, 0}, tiff...)
			body := append(append(append([]byte{}, data[:2]...), seg...), data[2:]...)
			rotated := o >= 2 && o <= 8
			if hasExifOrientation(body) != rotated {
				t.Fatalf("orientation=%d", o)
			}
			bm, err := decodeStored(body)
			if err != nil {
				t.Fatal(err)
			}
			w, h := bm.Size()
			wantW, wantH := 768, 432
			if rotated {
				wantW, wantH = 800, 450
				if o >= 5 {
					wantW, wantH = 450, 800
				}
			}
			if w != wantW || h != wantH {
				t.Fatalf("orientation=%d size=%dx%d want %dx%d", o, w, h, wantW, wantH)
			}
		}
	}
	for _, data := range [][]byte{nil, []byte("II"), []byte("MMxxxxxx"), []byte("IIxx\xff\xff\xff\xff")} {
		if rotatedTIFF(data) {
			t.Fatal("malformed TIFF rotated")
		}
	}
}

func TestStreamingJPEGUnsupportedVariantsFallBack(t *testing.T) {
	data := coverJPEG(t, 800, 49, false)
	extended := bytes.Clone(data)
	sof := bytes.Index(extended, []byte{0xff, 0xc0})
	if sof < 0 {
		t.Fatal("missing SOF")
	}
	extended[sof+1] = 0xc1
	late := append(append(append([]byte{}, data[:len(data)-2]...), []byte{0xff, 0xee, 0, 14, 'A', 'd', 'o', 'b', 'e', 0, 100, 0, 0, 0, 0, 1}...), data[len(data)-2:]...)
	for _, body := range [][]byte{extended, late} {
		if _, handled, err := decodeJPEGStored(body); handled || err != nil {
			t.Fatalf("fallback handled=%v err=%v", handled, err)
		}
		if _, err := decodeStored(body); err != nil {
			t.Fatalf("standard fallback failed: %v", err)
		}
	}
}
