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
	"time"

	"github.com/egoist/mygo/ui"
)

// exifJPEG 给一张 JPEG 装上 EXIF 方向标签（方向 o）。
//
// ⚠️ **手工拼那一段 APP1**：Go 的标准库不写 EXIF，而 `image/jpeg` 也不读它——
// 所以方向标签得自己塞进去（构造照 `jpegstream_test.go` 里那一条）。
//
// ⚠️ **像素是「每个坐标一个认得出来的颜色」**：这样**像素级**断言才断得出
// 「翻的是哪一边」「转的是哪一度」——只比宽高的话，方向 2（水平翻）和
// 方向 4（垂直翻）都是 80×60，方向 6（顺时针）和方向 8（逆时针）也都是
// 60×80，**全都分不出来**（那正是评审指出来的问题）。
//
// ⚠️ **颜色用坐标算，不用序号**：序号那一版（`byte(i)`）在横竖方向上不是
// 对称的，翻过来之后「同一个序号」落在**另一个坐标**上，于是「翻的是哪一边」
// 量不出来。坐标那一版（`r=x, g=y`）横着翻和竖着翻会落在**不同的颜色**上。
func exifJPEG(t testing.TB, w, h int, o int) []byte {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			// r 记 x、g 记 y、b 记一个常数：于是「x 变了还是 y 变了」看得出来。
			src.SetRGBA(x, y, colorRGBA{uint8(x % 256), uint8(y % 256), 40, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, src, &jpeg.Options{Quality: 100}); err != nil {
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

// colorRGBA 是 color.RGBA 的本地别名（免得再引一个包）。
type colorRGBA = struct{ R, G, B, A uint8 }

// ---- 像素级的比对 ----

// pixelAt 报一张图的 (x, y) 那个像素。
func pixelAt(img image.Image, x, y int) (r, g, b uint8) {
	c := img.At(x, y)
	cr, cg, cb, _ := c.RGBA()
	return uint8(cr >> 8), uint8(cg >> 8), uint8(cb >> 8)
}

// ---- 用例 ----

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
// ⚠️ **图必须比 `maxStoredSide`（768）大**：缓存里存的那一份是**缩到最长边
// 768** 的，而这一条要证明「交出去的是原图」——**图比 768 小的话，缩放那一档
// 压根不会碰它**，于是「原图」和「错误地缩过的那一份」是**同一张**，断言
// 恒真——**证不出任何东西**。
//
// ⚠️ **所以判据是「宽高是原图的宽高」**：1100×800 缩到 768 会变成
// 768×559——**不一样**，所以这一条才真的能抓住「缩了」。
//
// ⚠️ **图不能太大**：这一组跑在**验收命令里**（`go test -p 4` 全部包一起跑），
// 而编一张 1100×800 的 JPEG 要十几毫秒（实测 1024×768 是 15ms）——那是
// **抢在别人机器上**的 CPU。十几毫秒换一条真能证伪的断言，划算。
func TestSourceImageIsNotScaled(t *testing.T) {
	const w, h = 1100, 800
	body := exifJPEG(t, w, h, 1)
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
	if b.Dx() != w || b.Dy() != h {
		t.Errorf("拿到的是 %dx%d，want %dx%d"+
			"（缩到最长边 %d 的那一份是 %dx%d——所以这个尺寸**认得出**有没有缩）",
			b.Dx(), b.Dy(), w, h, maxStoredSide,
			maxStoredSide, maxStoredSide*h/w)
	}
}

// TestSourceImageIsNotTheStoredOne 断**交出去的那一份和缓存里存的那一份不是同一张**。
//
// ⚠️ **这一条是「不缩放」的另一半**：上一条断的是「尺寸是原图的尺寸」，而这一条
// 断的是「**缓存里真的存了缩过的那一份**」——于是两件事对上了：
// **交出去的和存着的不一样**，而交出去的那一份是原图。
//
// ⚠️ **为什么非要存一次不可**：`SourceImage` 走的是 `Source`（原始字节），
// **不经过内存缓存**——所以不去 `Get` 一下的话，内存缓存里根本没有那张图，
// 「不一样」就没有东西可比。
func TestSourceImageIsNotTheStoredOne(t *testing.T) {
	const w, h = 1100, 800
	body := exifJPEG(t, w, h, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	c := New(Options{})
	t.Cleanup(c.Close)

	url := srv.URL + "/cover.jpg"

	// 先让缓存**存下缩过的那一份**（界面上摆的就是它）。
	waitForBitmap(t, c, url)
	st := c.Get(url)
	if !st.Ready() {
		t.Fatalf("缓存里没有存下那一张：%+v", st)
	}
	storedW, storedH := st.Bitmap.Size()
	if storedW == w && storedH == h {
		t.Fatalf("缓存里存的是 %dx%d——**没缩**的话这一条就证不出「交出去的不一样」",
			storedW, storedH)
	}
	if storedW != maxStoredSide {
		t.Errorf("缓存里存的是 %d 宽，want %d（缩到最长边）", storedW, maxStoredSide)
	}

	// 再取「交出去的那一份」。
	img, err := c.SourceImage(url)
	if err != nil {
		t.Fatalf("取原图：%v", err)
	}
	b := img.Bounds()
	if b.Dx() != w || b.Dy() != h {
		t.Errorf("交出去的是 %dx%d，want %dx%d", b.Dx(), b.Dy(), w, h)
	}
}

// TestSourceImageStraightensTheEXIFOrientation 断**EXIF 方向摆正了，而且摆对了**。
//
// ⚠️ **判据是像素，不是宽高**：方向 2~4（翻转那一批）宽高**始终不变**，
// 方向 5~8（转置那一批）宽高**始终互换**——只比宽高的话，**翻错边、转错
// 方向全都测得出来**（这正是评审指出来的问题）。
//
// ⚠️ **像素级判据怎么定的**：夹具那一张图的像素是「r 记 x、g 记 y」（见
// `exifJPEG`），于是**摆正之后的那一张**，每个像素的 r 应当等于它**原来的**
// x（横着翻就是 x 变了、y 没变），g 应当等于原来的 y。
//
// ⚠️ **JPEG 是有损的**：所以像素是**约等于**（差 8 以内算一样），不是一模一样。
// 质量调到 100 就是为了让这个余量够用。
func TestSourceImageStraightensTheEXIFOrientation(t *testing.T) {
	const w, h = 80, 60
	for _, o := range []int{1, 2, 3, 4, 5, 6, 7, 8} {
		body := exifJPEG(t, w, h, o)
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
		wantW, wantH := w, h
		if o >= 5 {
			wantW, wantH = h, w
		}
		if b.Dx() != wantW || b.Dy() != wantH {
			t.Errorf("方向 %d：拿到的是 %dx%d，want %dx%d", o, b.Dx(), b.Dy(), wantW, wantH)
		}
		// 像素级：每一格都要落在「摆正之后该在的位置」。
		checkOrientedPixels(t, o, img, w, h)
		c.Close()
		srv.Close()
	}
}

// checkOrientedPixels 断**摆正之后每一个像素都在它该在的位置**。
//
// ⚠️ **判据是「源坐标 → 目标坐标」那张表**：把原图（w×h）的每一个坐标按
// 方向 o 映到目标坐标，然后看目标图那个坐标上的颜色**是不是原来那个坐标的
// 颜色**——像素错位、翻错边、转错方向都会立刻露出来。
func checkOrientedPixels(t *testing.T, o int, img image.Image, w, h int) {
	t.Helper()
	b := img.Bounds()
	for y := range b.Dy() {
		for x := range b.Dx() {
			sx, sy := orientedSource(o, x, y, w, h)
			wantR, wantG := uint8(sx%256), uint8(sy%256)
			gotR, gotG, _ := pixelAt(img, x, y)
			const slack = 8
			if absDiff(gotR, wantR) > slack || absDiff(gotG, wantG) > slack {
				t.Errorf("方向 %d：目标 (%d,%d) 的颜色是 (%d,%d)，want (%d,%d)"+
					"（源坐标该是 (%d,%d)）",
					o, x, y, gotR, gotG, wantR, wantG, sx, sy)
				return
			}
		}
	}
}

// orientedSource 是「目标坐标 → 源坐标」的那张表。
//
// ⚠️ **它和 `orientImage` 里那一张是同一个映射**（照 `mygo/ui/bitmap.go` 的
// `orient`）：目标 (x,y) 取源图的 (sx,sy)。
func orientedSource(o, x, y, w, h int) (sx, sy int) {
	switch o {
	case 2:
		sx, sy = w-1-x, y
	case 3:
		sx, sy = w-1-x, h-1-y
	case 4:
		sx, sy = x, h-1-y
	case 5:
		sx, sy = y, x
	case 6:
		sx, sy = y, h-1-x
	case 7:
		sx, sy = w-1-y, h-1-x
	case 8:
		sx, sy = w-1-y, x
	default:
		sx, sy = x, y
	}
	return sx, sy
}

// absDiff 报两个数差多少（绝对值）。
func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// TestTheOrientationMatchesTheFrameworksOwn 断**摆正的结果和框架自己摆的是
// 同一个**。
//
// ⚠️ **对着框架比，而不是对着一份抄过来的映射比**：抄一遍映射就是两份会走偏的
// 实现（而且走偏了没人知道）。框架那一份是 `ui.DecodeBitmap`（界面上摆的就是
// 它）。
//
// ⚠️ **比的是像素，不是宽高**：框架的 `ui.Bitmap` **像素读不回来**（它只有
// Pixelated / Update / Size 三个方法）——只比宽高的话，方向 2（水平翻）和
// 方向 4（垂直翻）都是 80×60，方向 6（顺时针）和方向 8（逆时针）也都是
// 60×80，**全都分不出来**（那正是评审指出来的问题：翻错边、转错方向全绿）。
//
// ⚠️ **框架的像素怎么读回来**：**把它画出来**。`ui.Render` 在无头环境里
// 画一帧（软件光栅化），读那张画布的像素就是框架摆正之后的那一张——
// 而 `Pixelated()` 让它一个像素对一个像素（不做插值），所以画布上的像素
// **就是**框架那一张的像素。
//
// ⚠️ **像素级比的是「框架那份」和「这边那份」**：两边都是各自独立算出来的
// （框架在 `mygo/ui/bitmap.go` 的 `orient`，这边在 `scale.go` 的
// `orientImage`），所以这一条量的是「两边的映射是不是同一个」——而不是
// 「两边是不是都抄了同一张表」。
func TestTheOrientationMatchesTheFrameworksOwn(t *testing.T) {
	const w, h = 80, 60
	for _, o := range []int{1, 2, 3, 4, 5, 6, 7, 8} {
		body := exifJPEG(t, w, h, o)

		frame, fw, fh := frameworkPixels(t, body)
		img, err := decodeOriented(body)
		if err != nil {
			t.Fatalf("方向 %d：这边解不出来：%v", o, err)
		}
		b := img.Bounds()
		if b.Dx() != fw || b.Dy() != fh {
			t.Errorf("方向 %d：这边是 %dx%d，框架那边是 %dx%d", o, b.Dx(), b.Dy(), fw, fh)
			continue
		}
		// 像素级：框架摆正之后的那一张和这边摆正的那一张，**逐格一样**。
		if x, y, fr, fg, fb, or, og, ob, ok := firstPixelDiff(frame, img, fw, fh); !ok {
			t.Errorf("方向 %d：(%d,%d) 框架那边是 (%d,%d,%d)，这边是 (%d,%d,%d)"+
				"（翻错边 / 转错方向了）",
				o, x, y, fr, fg, fb, or, og, ob)
		}
	}
}

// frameworkPixels 拿框架自己摆正之后的那一张的**像素**。
//
// ⚠️ **靠「画出来」读回来**：框架的 `ui.Bitmap` 只有 Pixelated / Update /
// Size 三个方法，像素**读不回来**——所以只能画一帧（`ui.Render`，无头软件
// 光栅化）再读画布。
//
// ⚠️ **`Pixelated()` 是关键**：没有它，缩放是双线性插值出来的，画布上的像素
// 就不是原图的那些像素了（而有它，一个像素对一个像素）。
func frameworkPixels(t testing.TB, body []byte) (*image.RGBA, int, int) {
	t.Helper()
	bm, err := ui.DecodeBitmap(body)
	if err != nil {
		t.Fatalf("框架解不出来：%v", err)
	}
	w, h := bm.Size()
	bm.Pixelated()
	frame := ui.Render(func(c *ui.Context) {
		ui.Image(c, bm).Size(float32(w), float32(h))
	}, w, h, 1)
	return frame, w, h
}

// firstPixelDiff 比两张图，报告**第一处不一样**的像素。
//
// ⚠️ **余量是 2**：两边都是同一份 JPEG 解出来的 RGBA（不透明），理论上逐格
// 一模一样；留两格是为了躲开不同平台的取整差。
//
// ⚠️ **返回的那一组数是「第一次不一样的」**：报错时只报一处——一处就够了，
// 而报一整屏的差别没人看得下去。
func firstPixelDiff(a, b image.Image, w, h int) (x, y int, ar, ag, ab, br, bg, bb uint8, same bool) {
	const slack = 2
	for yy := range h {
		for xx := range w {
			ar, ag, ab = pixelAt(a, xx, yy)
			br, bg, bb = pixelAt(b, xx, yy)
			if absDiff(ar, br) > slack || absDiff(ag, bg) > slack || absDiff(ab, bb) > slack {
				return xx, yy, ar, ag, ab, br, bg, bb, false
			}
		}
	}
	return 0, 0, 0, 0, 0, 0, 0, 0, true
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

// waitForBitmap 等缓存里存下那一张（界面上摆的就是它）。
func waitForBitmap(t *testing.T, c *Cache, url string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if c.Get(url).Ready() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("等了 10s，那张图还没进缓存")
}
