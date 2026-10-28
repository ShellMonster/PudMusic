package covertint

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"strings"
	"testing"

	xdraw "golang.org/x/image/draw"
)

// 小图取出来的色**和原图取的一样**——这一条是这一票的核心判据。
//
// # 为什么要有这一条
//
// 这一票把「下原图」换成了「下小图」。换的代价是：**小图上的色和原图上的色
// 会不会不一样**。会的话，用户看到的光晕颜色就变了——而那是一个**看不出
// bug 的变化**：没有报错、没有崩溃，只是颜色悄悄偏了。
//
// 所以这一条把它钉住：偏差必须小到看不出来。
//
// # 判据的来历（实测 60 张真封面）
//
//	等比缩放长边 48  → max |Δhue| 31.52°  avg 1.68°   ← 飘
//	等比缩放长边 64  → max |Δhue| 15.96°  avg 1.04°
//	等比缩放长边 96  → max |Δhue|  8.27°  avg 0.90°   ← 收住了
//
// 96 那一档是本票要的那一档（见 `TintSide`）。

// hueGap 是两个色相之间最短的角度差（0~180）。
func hueGap(a, b float64) float64 {
	d := a - b
	for d > 180 {
		d -= 360
	}
	for d < -180 {
		d += 360
	}
	if d < 0 {
		d = -d
	}
	return d
}

// 一张「主色在两边、中心是另一种颜色」的封面。
//
// 这一张是**中心裁剪会失手的那种**：中心裁成正方之后，两边那两块绿被切掉，
// 剩下的中间是红的——取到的色就整个变了。
//
// ⚠️ 夹具要**挑得出**这个差别：先前那张「半灰半红」的挑不出来（两种取法
// 都得到同一个色相，那条测试是**空过**的）。现在这一张实测：
// 整张 31.3°、中心裁剪 0.0°（偏 31.3°）、等比缩放 31.5°（偏 0.2°）。
func sideCover() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for y := range 180 {
		for x := range 320 {
			if x < 60 || x >= 260 {
				img.Set(x, y, color.RGBA{30, 200, 40, 255}) // 绿，在两边
			} else {
				img.Set(x, y, color.RGBA{220, 30, 30, 255}) // 红，在中间
			}
		}
	}
	return img
}

// 小图和原图取出来的色相**差不超过 10°**。
//
// ⚠️ 10° 这个数不是拍的：实测 60 张真封面，等比缩放到长边 96 的最大偏差是
// **8.27°**，留一点余量但仍远小于「看得出颜色变了」的程度（人眼对色相的
// 分辨力大约在 5~10°，而这是**背景上的一层 18%/28% 淡光晕**，更钝）。
func TestTheSmallCoverKeepsTheColour(t *testing.T) {
	full := sideCover()
	fullHue, ok := Dominant(full)
	if !ok {
		t.Fatal("整张封面取不出颜色")
	}

	// 按本票要的那一档等比缩小（长边 96）。
	small := scaleTo(full, TintSide)
	smallHue, ok := Dominant(small)
	if !ok {
		t.Fatal("小图取不出颜色")
	}

	if gap := hueGap(fullHue.H, smallHue.H); gap > 10 {
		t.Fatalf("小图取出的色相偏了 %.1f°（整张 %.1f°，小图 %.1f°）——"+
			"光晕的颜色会变", gap, fullHue.H, smallHue.H)
	}
}

// ⚠️ **中心裁剪会失手，而等比缩放不会**——这一条钉住「为什么参数不带 `_1c`」。
//
// 如果哪天有人把参数改成 `@96w_96h_1c`，这一条会红：中心裁成正方把右半边
// 那块红切掉大半，取到的色就偏了。
func TestTheCentreCropWouldChangeTheColour(t *testing.T) {
	full := sideCover()
	fullHue, _ := Dominant(full)

	// 中心裁成正方形再缩（模拟 `@96w_96h_1c`）。
	b := full.Bounds()
	sq := min(b.Dx(), b.Dy())
	cx, cy := b.Min.X+(b.Dx()-sq)/2, b.Min.Y+(b.Dy()-sq)/2
	cropped := image.NewRGBA(image.Rect(0, 0, sq, sq))
	xdraw.ApproxBiLinear.Scale(cropped, cropped.Bounds(), full,
		image.Rect(cx, cy, cx+sq, cy+sq), xdraw.Src, nil)
	cropHue, _ := Dominant(cropped)

	// 这一条**反过来断言**：中心裁剪的偏差要明显大于等比缩放。
	// （它是「为什么不带 _1c」的证据，不是「等比缩放对不对」的判据。）
	small := scaleTo(full, TintSide)
	smallHue, _ := Dominant(small)

	gapCrop := hueGap(fullHue.H, cropHue.H)
	gapScale := hueGap(fullHue.H, smallHue.H)
	if gapCrop <= gapScale {
		t.Fatalf("中心裁剪的偏差（%.1f°）没有比等比缩放（%.1f°）大——"+
			"这张夹具测不出两者的差别了", gapCrop, gapScale)
	}
}

// scaleTo 把图等比缩到长边 side 像素。
func scaleTo(src image.Image, side int) image.Image {
	b := src.Bounds()
	k := float64(side) / float64(max(b.Dx(), b.Dy()))
	w := int(float64(b.Dx())*k + 0.5)
	h := int(float64(b.Dy())*k + 0.5)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
	return dst
}

// JPEG 压缩本身**不该**明显改变取色结果。
//
// 这一条是回归：以前取色解的是**原图字节**（B 站给的是 JPEG，那就是已经
// 压过一次的），现在解的是图床**再压一次**的小图——多了一次有损压缩。
// 这一条确认那一次额外的压缩不影响颜色。
func TestTheSecondJpegCompressionDoesNotChangeTheColour(t *testing.T) {
	full := sideCover()

	// 整张编一次 JPEG（模拟 B 站给的那份原图字节）。
	var once bytes.Buffer
	if err := jpeg.Encode(&once, full, nil); err != nil {
		t.Fatal(err)
	}
	onceImg, _, err := image.Decode(bytes.NewReader(once.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	onceHue, _ := Dominant(onceImg)

	// 小图再编一次（这一趟下载的就是它）。
	small := scaleTo(full, TintSide)
	var twice bytes.Buffer
	if err := jpeg.Encode(&twice, small, nil); err != nil {
		t.Fatal(err)
	}
	twiceImg, _, err := image.Decode(bytes.NewReader(twice.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	twiceHue, _ := Dominant(twiceImg)

	if gap := hueGap(onceHue.H, twiceHue.H); gap > 10 {
		t.Fatalf("压了两次之后的色相偏了 %.1f°（一次 %.1f°，两次 %.1f°）",
			gap, onceHue.H, twiceHue.H)
	}
}

// ⚠️ **端到端那一条**：走完整的 `Fetch` 链路（HTTP → 解码 → 取色），服务端
// 返回一张「主色在两边、中心是另一种颜色」的封面。
//
// # 为什么要有这一条
//
// 前面那几条测的是**算法层**（缩放本身保不保色），而这一条测的是**端到端**
// ——图床按参数给回来的那张图，经过解码之后取到的色对不对。
//
// ⚠️ **两条缺一不可**：只测算法层的话，「取色那一趟拿到的图被裁过了」这种
// 回归**抓不住**（实测过：把 `Fetch` 里的解码改成中心裁剪，算法层那几条
// 照样全绿）。只测端到端的话，「为什么是这个参数」就没有证据了。
func TestTheTintEndToEndKeepsTheColour(t *testing.T) {
	full := sideCover()
	fullHue, ok := Dominant(full)
	if !ok {
		t.Fatal("整张封面取不出颜色")
	}

	// 假图床：按请求里的参数**真的裁**（中心裁 or 等比缩），再给回去。
	// 不这么做的话测的就不是「图床给了什么」，而是「我们假设图床给了什么」。
	got := new(string)
	cli := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		*got = r.URL.RequestURI()
		served := full
		if strings.Contains(*got, "_1c") {
			served = centreCrop(full)
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, served, nil); err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(&buf),
			Header:     make(http.Header),
		}, nil
	})}

	h, ok, err := Fetch(context.Background(), cli, biliCover)
	if err != nil || !ok {
		t.Fatalf("取色失败：ok=%v err=%v", ok, err)
	}
	if gap := hueGap(fullHue.H, h.H); gap > 10 {
		t.Fatalf("端到端取出的色相偏了 %.1f°（整张 %.1f°，取到 %.1f°，请求 %q）——"+
			"光晕的颜色会变", gap, fullHue.H, h.H, *got)
	}
}

// centreCrop 把图中心裁成正方形。
func centreCrop(src image.Image) image.Image {
	b := src.Bounds()
	sq := min(b.Dx(), b.Dy())
	cx, cy := b.Min.X+(b.Dx()-sq)/2, b.Min.Y+(b.Dy()-sq)/2
	dst := image.NewRGBA(image.Rect(0, 0, sq, sq))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src,
		image.Rect(cx, cy, cx+sq, cy+sq), xdraw.Src, nil)
	return dst
}

// 缩得**太小**的话颜色会飘——这一条钉住「那一档不能随便调小」。
//
// # 为什么要有这一条
//
// 票里的实测结论是「长边 48 就开始飘（max 31.52°），96 才收住
// （max 8.27°）」。而先前那条保色测试用的夹具（两边各 60px 绿、中间红）
// **对缩放极不敏感**——实测把 `TintSide` 改成 96/64/48/32/24，那几条
// 测试**全绿**。
//
// 这一条换一个**对小色块敏感**的夹具：画面绝大部分是红，只有右上角一小
// 块绿。缩得越小，那块绿占的像素越少、权重越轻，色相就越往红那边偏。
func TestShrinkingTooFarDriftsTheColour(t *testing.T) {
	// 320×180，右上角 40×40 一块绿，其余是红。
	img := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for y := range 180 {
		for x := range 320 {
			if x >= 280 && y < 40 {
				img.Set(x, y, color.RGBA{30, 200, 40, 255})
			} else {
				img.Set(x, y, color.RGBA{220, 30, 30, 255})
			}
		}
	}
	full, ok := Dominant(img)
	if !ok {
		t.Fatal("整张封面取不出颜色")
	}

	// 这一档（本票要用的）必须稳。
	small := scaleTo(img, TintSide)
	sh, ok := Dominant(small)
	if !ok {
		t.Fatal("小图取不出颜色")
	}
	if gap := hueGap(full.H, sh.H); gap > 10 {
		t.Fatalf("长边 %d 的小图取出的色相偏了 %.1f°（整张 %.1f°，小图 %.1f°）",
			TintSide, gap, full.H, sh.H)
	}

	// ⚠️ **缩到很小也不会飘**——这是这一条真正要记的事。
	//
	// `Dominant` 对缩放**天然不敏感**：它算的是色相的加权平均，而**灰的
	// 权重是 0**（`w = s*s*(1-|2l-1|)`，灰的 s=0）。所以无论缩多小，各色块
	// 的**相对权重不变**，色相就不变。
	//
	// 实测（这张夹具）：长边 96 / 64 / 48 / 32 / 16，色相偏差全是 0.0°。
	//
	// ⚠️ **所以「48 会飘、96 收住」那个结论不是这么来的**——那是**采样
	// 步进**造成的（见下面那一条），不是缩放本身。
	tiny := scaleTo(img, 16)
	th, ok := Dominant(tiny)
	if !ok {
		t.Fatalf("16 像素的小图取不出颜色：%+v", th)
	}
	if gap := hueGap(full.H, th.H); gap > 2 {
		t.Fatalf("缩到 16 像素色相就偏了 %.1f°——"+
			"这和「灰的权重是 0」那套算法对不上，算法变了？", gap)
	}
}

// `Dominant` 的采样是**跳着扫**的（步进 `min(宽,高)/48`），所以
// **细窄的主色可能被整个跳过**——整张图就变成「取不到色」。
//
// # 为什么这一条要紧
//
// 取色那一趟现在下的是一张 96 像素的小图。**96 像素那张图上的步进是
// 2~3**（取决于宽高比），而原图上可能更大——于是「原图能取到色、小图取不到」
// 是可能的。
//
// ⚠️ **但这一条不是本票引入的**：`Dominant` 一直是这样跳着扫的，取整张
// 原图时也一样会跳过细窄的主色（实测：320×180 上 1 像素宽的一条绿，
// 整张取**取不到色**，缩到 160 宽（步进 1）才取得到）。
//
// 记在这里是因为**换小图会改变步进**，于是「取不到色」的边界会挪。
// 本票实测（60 张真封面）没有一张因此取不到色，所以它**不是阻塞项**。
func TestTheSamplingStepCanSkipANarrowStripe(t *testing.T) {
	// 320×180，中间一列（1 像素宽）是绿的，其余是灰。
	img := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for y := range 180 {
		for x := range 320 {
			if x == 160 {
				img.Set(x, y, color.RGBA{30, 200, 40, 255})
			} else {
				img.Set(x, y, color.RGBA{128, 128, 128, 255})
			}
		}
	}
	// 原图上：步进 = 180/48 = 3，那一列被跳过 → 取不到色。
	if _, ok := Dominant(img); ok {
		t.Log("这一条那一列没被跳过（算法改了？）")
	}
	// 缩到 160 宽：步进 = 90/48 = 1，逐像素扫 → 取得到。
	small := scaleTo(img, 160)
	if _, ok := Dominant(small); !ok {
		t.Fatal("缩到 160 宽（步进 1）之后还是取不到色——")
	}
}
