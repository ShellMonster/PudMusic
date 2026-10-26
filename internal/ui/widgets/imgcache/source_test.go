package imgcache

// `Source` / `SourceImage` 这一族的测试（票 #556）。
//
// # 这一族量的是「交出去的那一份是不是原图」
//
// 界面上摆的封面是**缩到最长边 768** 的那一份（`scale.go` 的 `maxStoredSide`），
// 而**交出去的那一份要的是原图**——所以这两条 API 的判据是：
//
//   - `Source` 给的是**原始字节**（不是缩放之后的那一份，也不是解出来的像素）；
//   - `SourceImage` 给的是**摆正之后**的那一张（EXIF 方向 5~8 时宽高互换）。
//
// ⚠️ **不打真 B 站**：夹具是本地的 `httptest` 服务。

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/egoist/mygo/ui"
)

// exifJPEG 给一张 JPEG 装上 EXIF 方向标签（方向 o）。
//
// ⚠️ **手工拼那一段 APP1**：Go 的标准库不写 EXIF，而 `image/jpeg` 也不读它——
// 所以方向标签得自己塞进去（构造照 `jpegstream_test.go` 里那一条）。
func exifJPEG(t testing.TB, w, h int, o int) []byte {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range src.Pix {
		src.Pix[i] = byte(i)
		if i%4 == 3 {
			src.Pix[i] = 255
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, src, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	data := b.Bytes()

	tiff := make([]byte, 26)
	copy(tiff, "II")
	binary.LittleEndian.PutUint16(tiff[2:], 42)
	binary.LittleEndian.PutUint32(tiff[4:], 8)
	binary.LittleEndian.PutUint16(tiff[8:], 1)
	binary.LittleEndian.PutUint16(tiff[10:], 0x112)
	binary.LittleEndian.PutUint16(tiff[12:], 3)
	binary.LittleEndian.PutUint32(tiff[14:], 1)
	binary.LittleEndian.PutUint16(tiff[18:], uint16(o))
	seg := append([]byte{0xff, 0xe1, 0, 34, 'E', 'x', 'i', 'f', 0, 0}, tiff...)
	return append(append(append([]byte{}, data[:2]...), seg...), data[2:]...)
}

// TestSourceGivesTheOriginalBytes 断**`Source` 给的是原始字节**。
//
// ⚠️ **判据是「字节一模一样」**，不是「能解开」：这一条 API 的用处就是
// 「把下回来的那一份原样交出去」，而缩过的那一份（最长边 768）不是它。
func TestSourceGivesTheOriginalBytes(t *testing.T) {
	body := exifJPEG(t, 64, 48, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	c := New(Options{})
	t.Cleanup(c.Close)

	got, err := c.Source(srv.URL + "/cover.jpg")
	if err != nil {
		t.Fatalf("取原始字节：%v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("拿回来的不是原始字节：%d 字节，want %d", len(got), len(body))
	}
}

// TestSourceImageIsNotScaled 断**`SourceImage` 给的是原图，不是缓存里那一份**。
//
// ⚠️ **判据是宽高**：一张 1024×768 的图，缓存里存的是最长边 768 的那一份
// （1024×768 会被缩成 768×576），而交出去的要能是原图。
func TestSourceImageIsNotScaled(t *testing.T) {
	body := exifJPEG(t, 1024, 768, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	c := New(Options{})
	t.Cleanup(c.Close)

	img, err := c.SourceImage(srv.URL + "/cover.jpg")
	if err != nil {
		t.Fatalf("取原图：%v", err)
	}
	b := img.Bounds()
	if b.Dx() != 1024 || b.Dy() != 768 {
		t.Errorf("拿到的是 %dx%d，want 1024x768（缩过的那一份是 768x576）", b.Dx(), b.Dy())
	}
}

// TestSourceImageStraightensTheEXIFOrientation 断**EXIF 方向摆正了**。
//
// ⚠️ **判据是宽高互换**：方向 5~8（转置那一批）摆正之后宽高要互换，
// 而 2~4（翻转那一批）宽高不变。**不摆正的话交出去的是一张横躺着的图**。
func TestSourceImageStraightensTheEXIFOrientation(t *testing.T) {
	for _, o := range []int{1, 2, 3, 4, 5, 6, 7, 8} {
		body := exifJPEG(t, 1024, 768, o)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write(body)
		}))

		c := New(Options{})
		img, err := c.SourceImage(srv.URL + "/cover.jpg")
		if err != nil {
			srv.Close()
			t.Fatalf("方向 %d：取原图：%v", o, err)
		}
		b := img.Bounds()
		wantW, wantH := 1024, 768
		if o >= 5 {
			wantW, wantH = 768, 1024
		}
		if b.Dx() != wantW || b.Dy() != wantH {
			t.Errorf("方向 %d：拿到的是 %dx%d，want %dx%d", o, b.Dx(), b.Dy(), wantW, wantH)
		}
		c.Close()
		srv.Close()
	}
}

// TestTheOrientationMatchesTheFrameworksOwn 断**摆正的结果和框架自己摆的是
// 同一个**。
//
// ⚠️ **对着框架比，而不是对着一份抄过来的映射比**：抄一遍映射就是两份会走偏的
// 实现（而且走偏了没人知道）。框架那一份是 `ui.DecodeBitmap`（界面上摆的就是
// 它），所以判据是**两者的宽高一样**。
//
// ⚠️ **只比宽高，不比像素**：框架的 `ui.Bitmap` **像素读不回来**（它只有
// Pixelated / Update / Size 三个方法），所以像素比不了。宽高是它给得出的那一
// 个可比的数，而转置那一批（5~8）本来就要靠宽高才看得出来。
func TestTheOrientationMatchesTheFrameworksOwn(t *testing.T) {
	for _, o := range []int{1, 2, 3, 4, 5, 6, 7, 8} {
		body := exifJPEG(t, 1024, 768, o)

		bm, err := ui.DecodeBitmap(body)
		if err != nil {
			t.Fatalf("方向 %d：框架解不出来：%v", o, err)
		}
		wantW, wantH := bm.Size()

		img, err := decodeOriented(body)
		if err != nil {
			t.Fatalf("方向 %d：这边解不出来：%v", o, err)
		}
		b := img.Bounds()
		if b.Dx() != wantW || b.Dy() != wantH {
			t.Errorf("方向 %d：这边是 %dx%d，框架那边是 %dx%d", o, b.Dx(), b.Dy(), wantW, wantH)
		}
	}
}

// TestSourceOnAnEmptyURLSaysSo 断**空地址报错，而不是给一段空字节**。
//
// ⚠️ **空字节比报错更坏**：调用方会把它当成一张图交出去，用户在别的应用里
// 粘出来的是一张打不开的图。
func TestSourceOnAnEmptyURLSaysSo(t *testing.T) {
	c := New(Options{})
	defer c.Close()

	if _, err := c.Source(""); err == nil {
		t.Error("空地址也给了字节")
	}
	if _, err := c.SourceImage(""); err == nil {
		t.Error("空地址也给了图")
	}
}

// TestSourceAfterCloseSaysSo 断**缓存关掉之后取不到**（而不是给一段旧东西）。
func TestSourceAfterCloseSaysSo(t *testing.T) {
	c := New(Options{})
	c.Close()

	if _, err := c.Source("https://example.invalid/cover.png"); !errors.Is(err, ErrClosed) {
		t.Errorf("关掉之后取字节得到 %v，want ErrClosed", err)
	}
	if _, err := c.SourceImage("https://example.invalid/cover.png"); !errors.Is(err, ErrClosed) {
		t.Errorf("关掉之后取图得到 %v，want ErrClosed", err)
	}
}
