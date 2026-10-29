package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// 这台假图床**按请求里的档位参数真的裁**——和 B 站图床一样。
//
// # 为什么要有这一条
//
// 之前它给出去的是**原图**（不管 URL 上写的是 `@768w_128h_1c` 还是
// `@2048w_280h_1c`，给的都是同一张）。于是：
//
//   - 「要的那一档比例对不对」在画面上**看不出来**——因为图没被裁过；
//   - 视觉审查核的是**同一张图**，改没改比例都长得一样。
//
// 而**比例正是这一票要修的东西**（票 #664：横幅被拉宽 19%，画面上看得见）。
//
// ⚠️ **所以这一条是「让夹具说实话」**：夹具不按档位裁，图床那一档的比例
// 就**根本没在画面上出现过**——那视觉审查核的是空气。
func TestTheFakeImageHostCropsLikeTheRealOne(t *testing.T) {
	// 一张 4000×400 的红→蓝横向渐变（**要足够宽**：源图不够宽的话，
	// 两种档位都会「取满宽」，裁出来的结果就一样——实测源图 1200 宽时两种
	// 档位裁出来**逐字节相同**）。
	src := image.NewRGBA(image.Rect(0, 0, 4000, 400))
	for y := range 400 {
		for x := range 4000 {
			t := float64(x) / 4000.0
			src.Set(x, y, color.RGBA{
				R: uint8(240 * (1 - t)),
				G: 30,
				B: uint8(240 * t),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}

	// 两个档位：比例 6.0 的和比例 7.31 的。
	// ⇒ 中心裁切取走的横向范围不同 ⇒ 画面上两端的颜色不同。
	edges := map[string][2]uint8{}
	for _, u := range []string{
		"https://i0.hdslb.com/bfs/archive/banner.png@768w_128h_1c.webp",
		"https://i0.hdslb.com/bfs/archive/banner.png@2048w_280h_1c.webp",
	} {
		out := cropLikeRealHost(t, buf.Bytes(), u)
		b := out.Bounds()
		edges[u] = [2]uint8{
			out.RGBAAt(2, b.Dy()/2).R,
			out.RGBAAt(b.Dx()-3, b.Dy()/2).R,
		}
	}

	a := edges["https://i0.hdslb.com/bfs/archive/banner.png@768w_128h_1c.webp"]
	b := edges["https://i0.hdslb.com/bfs/archive/banner.png@2048w_280h_1c.webp"]
	if a == b {
		t.Fatalf("两个不同档位裁出来的图**一样**（两端都是 %v）——"+
			"夹具没按档位裁，比例就**根本没在画面上出现过**，"+
			"那样的视觉审查核的是空气", a)
	}
	// 两端的 R 差要够大（可测）：比例不同 ⇒ 中心裁切取走的横向范围不同 ⇒
	// 渐变落在两端时「跨度」不一样。
	diff := int(a[0]) - int(a[1])
	diffB := int(b[0]) - int(b[1])
	if diff < 50 || diffB < 50 {
		t.Fatalf("两个档位的「两端 R 差」分别 %d 和 %d——太小了量不出来"+
			"（这一条是「让夹具能证明比例」的地基）", diff, diffB)
	}
	// 而两个档位的跨度**要不一样**（那正是比例的差别）。
	if diff == diffB {
		t.Fatalf("两个档位的「两端 R 差」都是 %d——裁切的效果一样，"+
			"夹具还是没按档位裁", diff)
	}
}

// cropLikeRealHost 按 URL 里的档位参数中心裁（`_1c`），返回裁好的图。
//
// ⚠️ **这是 B 站图床的行为**：`@<w>w_<h>h_1c` = 按 W×H 的比例**居中裁**。
func cropLikeRealHost(t *testing.T, data []byte, u string) *image.RGBA {
	t.Helper()
	w, h := cropDimsFromURL(u)
	if w <= 0 || h <= 0 {
		t.Fatalf("地址 %q 里读不出档位", u)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	target := float64(w) / float64(h)
	cw, ch := sw, sh
	if float64(sw)/float64(sh) > target {
		cw = int(float64(sh) * target)
	} else {
		ch = int(float64(sw) / target)
	}
	x0 := b.Min.X + (sw-cw)/2
	y0 := b.Min.Y + (sh-ch)/2
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			dst.Set(x, y, img.At(x0+x*cw/w, y0+y*ch/h))
		}
	}
	return dst
}

// cropDimsFromURL 从 `@<w>w_<h>h_1c` 里读出宽高（读不出就返回 0,0）。
func cropDimsFromURL(u string) (int, int) {
	at := strings.Index(u, "@")
	if at < 0 {
		return 0, 0
	}
	rest := u[at+1:]
	if end := strings.Index(rest, "."); end >= 0 {
		rest = rest[:end]
	}
	// 形如 `768w_128h_1c`
	var w, h int
	if _, err := fmt.Sscanf(rest, "%dw_%dh", &w, &h); err != nil {
		return 0, 0
	}
	return w, h
}
