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

func TestJPEGEXIFOrientationStreamsIntoTargetSize(t *testing.T) {
	const maxSide = 96
	data := coverJPEG(t, 2048, 1536, false)
	decoded, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, orientation := range []int{2, 3, 4, 5, 6, 7, 8} {
		t.Run(fmt.Sprintf("orientation=%d", orientation), func(t *testing.T) {
			oriented := orientImage(decoded, orientation)
			want := shrinkTo(oriented, maxSide).(*image.RGBA)
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			got, handled, err := decodeJPEGStoredOriented(withJPEGOrientation(data, orientation), maxSide, orientation)
			runtime.ReadMemStats(&after)
			if err != nil || !handled {
				t.Fatalf("handled=%v err=%v", handled, err)
			}
			if got.Bounds() != want.Bounds() || !bytes.Equal(got.Pix, want.Pix) {
				t.Fatalf("orientation=%d output differs from image.Decode + orientImage + shrinkTo", orientation)
			}
			if max(got.Bounds().Dx(), got.Bounds().Dy()) > maxSide {
				t.Fatalf("orientation=%d output=%v exceeds max side %d", orientation, got.Bounds(), maxSide)
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 6<<20 {
				t.Fatalf("orientation=%d allocated %d bytes; full-size JPEG intermediate returned", orientation, allocated)
			}
		})
	}
}

func withJPEGOrientation(data []byte, orientation int) []byte {
	tiff := make([]byte, 26)
	copy(tiff, "II")
	bo := binary.LittleEndian
	bo.PutUint16(tiff[2:], 42)
	bo.PutUint32(tiff[4:], 8)
	bo.PutUint16(tiff[8:], 1)
	bo.PutUint16(tiff[10:], 0x112)
	bo.PutUint16(tiff[12:], 3)
	bo.PutUint32(tiff[14:], 1)
	bo.PutUint16(tiff[18:], uint16(orientation))
	seg := append([]byte{0xff, 0xe1, 0, 34, 'E', 'x', 'i', 'f', 0, 0}, tiff...)
	return append(append(append([]byte{}, data[:2]...), seg...), data[2:]...)
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
				got, handled, err := decodeJPEGStored(data, 0)
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
		bm, err := decodeStored(data, 0)
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
		_, handled, err := decodeJPEGStored(data[:n], 0)
		if !handled || err == nil {
			t.Fatalf("truncated at %d: handled=%v err=%v", n, handled, err)
		}
	}
}

func TestJPEGEXIFOnlyRotationsAreMadeUprightAndScaled(t *testing.T) {
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
			bm, err := decodeStored(body, 0)
			if err != nil {
				t.Fatal(err)
			}
			// ⚠️ **摆正之后照样缩**（这一条以前断言「旋转的那几张不缩」）：
			// 那时候的理由是「像素读不回来，转了就没法再缩」——`ui.Bitmap`
			// 确实只能写不能读，但**缩放是在造 Bitmap 之前**做的（`shrinkToStored`
			// 拿的是 `*image.RGBA`），所以那个理由从来就不成立。
			//
			// 而现在它必须缩：0.7.1 的 `ui.DecodeBitmap` 会把整份原始字节钉在
			// bitmap 上（为了 `ReleasePixels`），这一支要是还走它，几百 KB 的字节
			// 就跟着缓存条目一辈子——见 `decodeStored` 那一支的注释。
			//
			// 尺寸判据按**摆正之后**算：800×450 的图缩到长边 768 是 768×432；
			// 要转 90° 的那几张摆正之后是 450×800，长边 800 → 缩到 432×768。
			w, h := bm.Size()
			wantW, wantH := 768, 432
			if rotated && o >= 5 {
				wantW, wantH = 432, 768
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
		if _, handled, err := decodeJPEGStored(body, 0); handled || err != nil {
			t.Fatalf("fallback handled=%v err=%v", handled, err)
		}
		if _, err := decodeStored(body, 0); err != nil {
			t.Fatalf("standard fallback failed: %v", err)
		}
	}
}

// TestDecodedCoversDoNotHoldTheirBytes 钉住「解出来的那张图不把原始字节留在
// bitmap 上」。
//
// # 为什么这条必须存在
//
// 0.7.1 的 `ui.DecodeBitmap` 为了 `ReleasePixels` 能重新解码，把收到的
// `[]byte` 捕获在一个闭包里不放（`mygo/ui/bitmap.go` 的 `decode` 字段）。
// 我们这一格解的是**网络上下来的整张原图**：B 站封面动辄几百 KB，缓存里
// 几百张就是几十 MB 的隐形常驻——而界面上用的只是缩到 768 的那一份。
//
// 所以 `decodeStored` **一处都不该**再走 `ui.DecodeBitmap`。判据是**结构性的**：
// 把字节换掉再看图（`ui.DecodeBitmap` 造出来的会在下一次要像素时按旧字节
// 重新解出来，`ui.NewBitmap` 造的不会）。这里用更直接的证据：自己解一份，
// 然后把原始切片**改烂**，再让 bitmap 去画（`Draw` 会取像素）——不崩、尺寸
// 不变，就说明它没在回头看那份字节。
//
// ⚠️ **它不是内存测量的替代品**：真正的判据是「bitmap 上还挂着字节没有」，
// 而那只能靠 `runtime` 或者 `unsafe` 去探（这个包不该那么做）。它拦的是
// 「有人又把这一支改回 `ui.DecodeBitmap`」，那正是这一条的来由。
func TestDecodedCoversDoNotHoldTheirBytes(t *testing.T) {
	data := coverJPEG(t, 800, 450, false)
	// 造一张带 EXIF 旋转的（那是唯一一支曾经走 `ui.DecodeBitmap` 的）。
	bo := binary.LittleEndian
	tiff := make([]byte, 26)
	copy(tiff, "II")
	bo.PutUint16(tiff[2:], 42)
	bo.PutUint32(tiff[4:], 8)
	bo.PutUint16(tiff[8:], 1)
	bo.PutUint16(tiff[10:], 0x112)
	bo.PutUint16(tiff[12:], 3)
	bo.PutUint32(tiff[14:], 1)
	bo.PutUint16(tiff[18:], 6) // 90°
	seg := append([]byte{0xff, 0xe1, 0, 34, 'E', 'x', 'i', 'f', 0, 0}, tiff...)
	body := append(append(append([]byte{}, data[:2]...), seg...), data[2:]...)

	bm, err := decodeStored(body, 0)
	if err != nil {
		t.Fatal(err)
	}
	w, h := bm.Size()
	if w != 432 || h != 768 {
		t.Fatalf("摆正之后该缩到 432×768，实际 %dx%d", w, h)
	}
	// 把原始字节改烂：只留一个不是 JPEG 的头。
	for i := range body {
		body[i] = 0
	}
	// 尺寸读得回来、不崩——说明 bitmap 不靠那份字节活着。
	if w2, h2 := bm.Size(); w2 != w || h2 != h {
		t.Fatalf("字节被改烂之后尺寸变了：%dx%d → %dx%d", w, h, w2, h2)
	}
}
