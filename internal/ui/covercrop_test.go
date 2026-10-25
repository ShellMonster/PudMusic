// 封面裁剪的纯逻辑测试。
//
// 这一组**不开窗口**：全是几何和像素，断言的是数，不是画面。理由见
// covercrop.go 的包注释——裁剪算错了的症状在界面上看不出来（框在那儿、
// 上传也成功、只是传上去的图不对），所以只能在这儿钉。
//
// B 站是假的，这里也不联网。

package ui

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// ── 几何 ───────────────────────────────────────────────────────────────────

// TestTheDefaultCropIsTenTenEightyEighty 默认裁剪框是 10/10/80/80。
//
// 照抄 Biu（`components/image-upload/crop-modal.tsx:28-34`）。对一张 16:9 的图
// 这四个数原样成立——而收藏夹封面本来就是 16:9 的，所以这是绝大多数情况。
func TestTheDefaultCropIsTenTenEightyEighty(t *testing.T) {
	got := fitCrop(DefaultCrop, 1920, 1080)
	if got != DefaultCrop {
		t.Errorf("默认裁剪框过一遍 fitCrop = %+v，want %+v（原样）", got, DefaultCrop)
	}
	// 再钉一遍默认值本身——改 `DefaultCrop` 而忘了改这条期望的话，
	// 上面的 fitCrop 也会跟着一起变对，钉不住。
	if DefaultCrop != (cropBox{X: 10, Y: 10, W: 80, H: 80}) {
		t.Errorf("DefaultCrop = %+v，want {10 10 80 80}", DefaultCrop)
	}
}

// TestCropIsAlwaysSixteenByNine 框永远是 16:9，不管图是什么比例。
//
// 🔴 这就是「固定比例」那条验收。判定用**原图像素**上的比值，不是台子上的
// （台子本来就是 16:9，在台子上量永远是 16:9，量不出任何东西）。
func TestCropIsAlwaysSixteenByNine(t *testing.T) {
	for _, img := range [][2]int{{1920, 1080}, {800, 600}, {600, 1200}, {4000, 3000}, {300, 2000}} {
		for _, w := range []float32{10, 33, 50, 80, 100} {
			b := fitCrop(cropBox{X: 0, Y: 0, W: w}, float32(img[0]), float32(img[1]))
			cw, ch := b.W/100*float32(img[0]), b.H/100*float32(img[1])
			// 极端细长的图上高度会被夹到整张图（见 cropWH），那时比例不是
			// 16:9，跳过那一档——那种图没有可裁的。
			if ch >= float32(img[1])-0.5 {
				continue
			}
			if ratio := cw / ch; absF32(ratio-cropAspect) > 0.01 {
				t.Errorf("%dx%d 的图、宽 %v%%：裁出来 %vx%v，比例 %v，want %v",
					img[0], img[1], w, cw, ch, ratio, cropAspect)
			}
		}
	}
}

// TestCropStaysInsideTheImage 框整个在图里。
//
// 拖出黑边的那一段会被编进 JPEG 里，而 B 站那边收到的是一张边上有一条纯黑
// 的图——用户看到的是「封面右边有块黑」。
func TestCropStaysInsideTheImage(t *testing.T) {
	for _, img := range [][2]int{{1920, 1080}, {800, 600}, {600, 1200}} {
		for _, in := range []cropBox{
			{X: 95, Y: 95, W: 80, H: 80}, // 右下角往外挂
			{X: -30, Y: -30, W: 50, H: 50},
			{X: 50, Y: 90, W: 40, H: 40},
		} {
			b := fitCrop(in, float32(img[0]), float32(img[1]))
			if b.X < 0 || b.Y < 0 || b.X+b.W > 100.001 || b.Y+b.H > 100.001 {
				t.Errorf("%dx%d 的图上 %+v 变成了 %+v，出了图", img[0], img[1], in, b)
			}
		}
	}
}

// TestMovingTheBoxFollowsThePointer 框跟着手指走，而且不出图。
//
// ⚠️ 位移量要按图在台子上的缩放换算：不换算的话在一张 4000 宽的图上框会
// 「跑得比手指快好几倍」，用户往左拖框往右跑——那种表现比拖不动更难解释。
func TestMovingTheBoxFollowsThePointer(t *testing.T) {
	iw, ih := float32(1920), float32(1080)
	fx, _, fw, _ := fitImage(iw, ih)

	// 台子上往右挪 32 点（16:9 的图铺满台子，所以 32 点 = 32/320 = 10%）。
	got := moveCrop(cropBox{X: 10, Y: 10, W: 80, H: 80}, 32, 0, iw, ih)
	if absF32(got.X-20) > 0.01 {
		t.Errorf("往右挪 32 点之后 X = %v，want 20（10%% + 32/%v * 100）", got.X, fw)
	}
	_ = fx

	// 往右挪到头就停在贴边，不会拖出图。
	edge := moveCrop(cropBox{X: 10, Y: 10, W: 80, H: 80}, 9999, 0, iw, ih)
	if absF32(edge.X+edge.W-100) > 0.01 {
		t.Errorf("拖到头之后右边在 %v，want 100", edge.X+edge.W)
	}
}

// TestTheCornersBeatTheInside 把手优先于「框里面」。
//
// ⚠️ 判反的后果很具体：用户想缩框时命中的是「移动」，框跟着手走而不是变大——
// 而缩框是这个弹窗里最常用的动作，判反了它就基本没法用。
func TestTheCornersBeatTheInside(t *testing.T) {
	// 40,20,80,60 是一个框；左上角把手就在 (40,20)。
	if got := cornerAt(41, 21, 40, 20, 80, 60); got != cornerTL {
		t.Errorf("左上角上 = %v，want cornerTL", got)
	}
	if got := cornerAt(119, 79, 40, 20, 80, 60); got != cornerBR {
		t.Errorf("右下角上 = %v，want cornerBR", got)
	}
	// 正中间是「移动」。
	if got := cornerAt(80, 50, 40, 20, 80, 60); got != cornerMove {
		t.Errorf("框中间 = %v，want cornerMove", got)
	}
	// 框外什么也不是。
	if got := cornerAt(10, 10, 40, 20, 80, 60); got != cornerNone {
		t.Errorf("框外面 = %v，want cornerNone", got)
	}
}

// TestResizingFromACorner 拖角只改宽度，高度跟着 16:9 走。
//
// ⚠️ 从**下**边那两个角拉的时候下边缘要钉住不动，不然框会一边变宽一边往下长，
// 两个角同时动——用户想「只把左边拉出去」，看到的却是整个框在飘。
func TestResizingFromACorner(t *testing.T) {
	iw, ih := float32(1920), float32(1080)
	// 摆在中间而不是贴着上边：往下拉时框会长高，贴着上边的话会顶出图外，
	// 而顶出图外的那一段会被夹回来——那样测的就不是「下边钉住」而是「夹」。
	b := fitCrop(cropBox{X: 10, Y: 40, W: 40}, iw, ih) // 40×22.5
	left, _, boxW, _ := stageRect(b, iw, ih)

	// 从左下角往右拉一个框那么宽（40% → 80%）。
	got := resizeCrop(b, cornerBL, left+2*boxW, iw, ih)
	if absF32(got.W-80) > 0.01 {
		t.Errorf("往右拉之后宽度 = %v，want 80", got.W)
	}
	if absF32(got.X-b.X) > 0.5 {
		t.Errorf("左边动了：X = %v，want %v", got.X, b.X)
	}
	if absF32(got.Y+got.H-(b.Y+b.H)) > 0.5 {
		t.Errorf("下边缘动了：%v+%v vs %v+%v", got.Y, got.H, b.Y, b.H)
	}
	if cw, ch := got.W/100*iw, got.H/100*ih; absF32(cw/ch-cropAspect) > 0.01 {
		t.Errorf("拉完之后比例 = %v，want %v", cw/ch, cropAspect)
	}

	// 从右上角往左拉：右边缘钉住不动（宽度变小了，左边要往右让）。
	got2 := resizeCrop(b, cornerTR, left+boxW/2, iw, ih)
	if absF32(got2.X+got2.W-(b.X+b.W)) > 0.5 {
		t.Errorf("右边缘动了：%v+%v vs %v+%v", got2.X, got2.W, b.X, b.W)
	}
	if absF32(got2.Y-b.Y) > 0.5 {
		t.Errorf("上边缘动了：Y = %v，want %v", got2.Y, b.Y)
	}
}

// ── 像素 ───────────────────────────────────────────────────────────────────

// testImage 造一张纯色图（w×h）。每 100 列换一种颜色，这样裁完能看出裁到哪。
func testImage(t *testing.T, w, h int) *image.RGBA {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x / 100 * 40), G: uint8(y / 100 * 40), B: 200, A: 255})
		}
	}
	return img
}

// pngBytes 把一张图编成 PNG。
func pngBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// jpegSize 读出一个 data 地址里那张 JPEG 的尺寸。
func jpegSize(t *testing.T, dataURL string) (int, int) {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(decodeDataURL(t, dataURL)))
	if err != nil {
		t.Fatalf("编出来的不是一张能认的 JPEG：%v", err)
	}
	return cfg.Width, cfg.Height
}

func decodeDataURL(t *testing.T, dataURL string) []byte {
	t.Helper()
	_, b64, ok := strings.Cut(dataURL, ",")
	if !ok {
		t.Fatalf("没有逗号，不是 data 地址：%q", dataURL[:min(40, len(dataURL))])
	}
	out, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestTheOutputIsScaledByDPR 输出按 DPR 放大。
//
// 🔴 这是这一票的验收项之一。B 站的封面是在网页上按 CSS 像素摆的，而 CSS
// 像素在 Retina 上是两个物理像素：传一张按逻辑像素裁的图上去，用户在笔记本
// 上看着清楚、发到手机上就糊了。
func TestTheOutputIsScaledByDPR(t *testing.T) {
	src := testImage(t, 960, 540)             // 16:9
	half := cropBox{X: 0, Y: 0, W: 50, H: 50} // 裁一半 = 480×270

	w1, h1 := mustCrop(t, src, half, 1)
	w2, h2 := mustCrop(t, src, half, 2)
	w3, h3 := mustCrop(t, src, half, 3)
	if w1 != 480 || h1 != 270 {
		t.Errorf("DPR=1 裁出 %dx%d，want 480x270", w1, h1)
	}
	if w2 != 960 || h2 != 540 {
		t.Errorf("DPR=2 裁出 %dx%d，want 960x540", w2, h2)
	}
	if w3 != 1440 || h3 != 810 {
		t.Errorf("DPR=3 裁出 %dx%d，want 1440x810", w3, h3)
	}
}

// TestDPRIsClamped 荒谬的缩放比要夹住。
//
// Tester 里那台机器的缩放可以是任意值，而 DPR=8 的输出是 64 倍面积——
// 一张 1920×1080 会编成 15000×8000 的 JPEG，光分配就要几百 MB，用户按一下
// 「上传」应用就卡死。真实世界里只可能是 1 / 2 / 3。
func TestDPRIsClamped(t *testing.T) {
	src := testImage(t, 400, 300)
	for _, dpr := range []float32{0, -1, 0.5, 8, 100} {
		if _, err := cropToDataURL(src, cropBox{X: 0, Y: 0, W: 50}, dpr); err != nil {
			t.Errorf("DPR=%v 直接报错了：%v", dpr, err)
		}
	}
	w, _ := mustCrop(t, src, cropBox{X: 0, Y: 0, W: 50}, 100)
	if w > 3*200+2 {
		t.Errorf("DPR=100 裁出了 %d 宽，夹不住", w)
	}
}

// TestTheDataURLHasTheJPEGPrefix 前缀必须有，B 站从它认格式。
//
// 少了它 B 站把整段当成无意义的字符，然后**回一个打不开的地址**——不报错，
// 用户建收藏夹建得成，而封面是空的。
func TestTheDataURLHasTheJPEGPrefix(t *testing.T) {
	got, err := cropToDataURL(testImage(t, 320, 180), cropBox{X: 0, Y: 0, W: 50}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "data:image/jpeg;base64,") {
		t.Errorf("前缀 = %q", got[:min(30, len(got))])
	}
	// 而且它得真是一张能解码的 JPEG（别只是前缀对了）。
	raw := decodeDataURL(t, got)
	if _, err := jpeg.Decode(bytes.NewReader(raw)); err != nil {
		t.Errorf("编出来的不是 JPEG：%v", err)
	}
}

// TestDecodingRejectsWhatBiuRejects 挑文件和 Biu 一样：只收 jpeg / png，≤5MB。
func TestDecodingRejectsWhatBiuRejects(t *testing.T) {
	good := pngBytes(t, testImage(t, 40, 30))
	if _, err := decodeCover(good); err != nil {
		t.Fatalf("正常的 PNG 也解码不了：%v", err)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, testImage(t, 40, 30), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeCover(buf.Bytes()); err != nil {
		t.Errorf("正常的 JPEG 解不了：%v", err)
	}

	if _, err := decodeCover(nil); err == nil {
		t.Error("空文件也放行了")
	}
	if _, err := decodeCover([]byte("这不是一张图")); err == nil {
		t.Error("不是图的文件也放行了")
	}
	// ⚠️ 超大的那份要在**解码之前**就挡住：8000×8000 的照片解码完是 256MB。
	big := make([]byte, maxCoverBytes+1)
	copy(big, good)
	if _, err := decodeCover(big); err == nil {
		t.Error("超过 5MB 的也放行了")
	}
}

// TestCroppingDoesNotPanicOnAThinImage 极端形状不能 panic。
//
// 🔴 一条 `draw.Scaler` 的源矩形只要超出源图一个像素就**直接 panic**，而 view
// 里的 panic 会穿透 Cocoa 事件循环杀掉整个进程（见 shell_crash.go 的 Guard 注释）。
// 所以「夹到图里」那几行不是保险，是防崩。
func TestCroppingDoesNotPanicOnAThinImage(t *testing.T) {
	for _, sz := range [][2]int{{1, 1}, {3, 1}, {1, 3}, {2, 1000}, {1000, 2}} {
		src := testImage(t, sz[0], sz[1])
		for _, b := range []cropBox{DefaultCrop, {X: 99, Y: 99, W: 1}, {X: 0, Y: 0, W: 100}} {
			if _, err := cropToDataURL(src, b, 2); err != nil {
				// 报「太小」是可以的，崩掉不行。
				if !strings.Contains(err.Error(), "太小") && !strings.Contains(err.Error(), "空的") {
					t.Errorf("%dx%d 的图、%+v：%v", sz[0], sz[1], b, err)
				}
			}
		}
	}
}

func mustCrop(t *testing.T, src *image.RGBA, b cropBox, dpr float32) (int, int) {
	t.Helper()
	got, err := cropToDataURL(src, b, dpr)
	if err != nil {
		t.Fatal(err)
	}
	return jpegSize(t, got)
}
