// 这一族验**hover 放大**（票 #179 的 G45，以及票 #467 把它接到网格卡那一族）。
//
// # 原来那三处
//
//	UP 空间 / 合集网格那张封面	上游 `user-profile/grid-card/index.tsx:26`
//	动态流作者栏那个头像		上游 `dynamic-feed/user.tsx:46`
//	顶栏那个头像			上游 `layout/navbar/user/index.tsx:178`
//
// 三处都是 `hover:scale-105`——放大到 **105%**，网格那一处还带
// `transition-transform duration-300`。
//
// # 现在还有一处（同一个值）
//
// **网格卡 / 歌单卡 / 新歌速递 / 历史记录 / 稍后再看那几档的封面**：那一层
// `hoverZoom` 住在 `internal/ui/widgets/motion`（票 #467 把它从
// `internal/ui` 下沉下去，因为封面是 `widgets/hover` 那个包画的）。
//
// # 判据是「画出来的那一格变大了」
//
// ⚠️ **量得到是因为 `Tester.Find` 给回的是变换后的盒子**（`mygo/ui/runtime.go` 的
// `visualRect`），而 `Scale` 恰好不改布局——所以这一条量的是**画出来的大小**，
// 不是布局盒（`Bounds()` 那个）。
//
// ⚠️ **必须推钟**：Tester 的钟站着不动（`mygo/ui/headless.go`），不推的话那
// 300 毫秒不走，而「没走」的那一帧量出来是 1.0 倍——于是量到的不是「没放大」
// 而是「放大还没开始」。
package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// uiCoverPNG 造一张**有花纹**的 PNG，给封面用。
//
// ⚠️ **不能是纯色**：放大的是**那张图**，而一块纯色放大 5% 在画面上**一个像素
// 都不变**——量它等于量一块纯色。左上角那一块深色是「花纹」：图在框里放大 5%
// 的时候它会**往里挪**，框子左上角那个像素就从深色变成浅色。
func uiCoverPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{R: 200, G: 120, B: 60, A: 255})
		}
	}
	// 左上角一块深色。
	for y := range h / 4 {
		for x := range w / 3 {
			img.SetRGBA(x, y, color.RGBA{R: 20, G: 20, B: 20, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// changedPixels 数两张图在 box 里不一样的像素有几个。
func uiChangedPixels(a, b *image.RGBA, box mygoui.Rect) int {
	n := 0
	for y := int(box.Y); y < int(box.Y+box.H); y++ {
		for x := int(box.X); x < int(box.X+box.W); x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				n++
			}
		}
	}
	return n
}

// uiServePNG 让假 B 站顺带发一张图。
func uiServePNG(b *uiExplore, name string, data []byte) string {
	b.on("/"+name, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data)
	})
	return b.URL + "/" + name
}

// TestTheHoverZoomCurveIsNotTheUpstreamOne 钉住「我们这条曲线**不是**上游那条」。
//
// ⚠️ **这一条是修一句说谎的注释**：DESIGN.md §五 与 `hoverzoom.go` 的注释曾写着
// 「上游那条 `cubic-bezier(0.4, 0, 0.2, 1)` 也就是 `ui.EaseInOut`，视觉上没有
// 区别」——**那是假的**：两条曲线差得很远（实算见下）。
//
// 照着那句注释去「精确复刻上游」，观感会明显不一样；反过来，以为现在已经等于
// 上游、以后就不再过问，也不对。
//
// 实算（同一个 mygo v0.6.3）：
//
//	t      ui.EaseInOut    上游 cubic-bezier(0.4, 0, 0.2, 1)
//	0.25       0.0625                   0.2366
//	0.50       0.5000                   0.7756
//	0.75       0.9375                   0.9594
//
// 我们的 hover 放大走的是**左边那一列**（`ui.EaseInOut`）——见 `hoverZoom`。
//
// ⚠️ **这一条量不到元素本身**：那一格放大 5%，两条曲线在半程上差的那 1.5% 只有
// 28 像素的头像上 0.4 个像素——量不出来。所以它钉的是**曲线本身**（也就是文档
// 里引用的那几个数），元素那一侧由 `TestTheNavbarAvatarGrowsOnHover` 那一族钉。
func TestTheHoverZoomCurveIsNotTheUpstreamOne(t *testing.T) {
	upstream := mygoui.CubicBezier(0.4, 0, 0.2, 1)
	for _, p := range []struct {
		at        float32
		easeInOut float32
		upstream  float32
	}{
		{0.25, 0.0625, 0.2366},
		{0.50, 0.5000, 0.7756},
		{0.75, 0.9375, 0.9594},
	} {
		if got := mygoui.EaseInOut(p.at); got != p.easeInOut {
			t.Errorf("ui.EaseInOut(%.2f) = %.4f，want %.4f", p.at, got, p.easeInOut)
		}
		if got := upstream(p.at); got < p.upstream-0.002 || got > p.upstream+0.002 {
			t.Errorf("上游那条 cubic-bezier(0.4,0,0.2,1) 在 %.2f 处是 %.4f，want %.4f",
				p.at, got, p.upstream)
		}
		if mygoui.EaseInOut(p.at) == upstream(p.at) {
			t.Errorf("两条曲线在 %.2f 处相等了——文档里写着它们差得很远（%.4f vs %.4f）",
				p.at, mygoui.EaseInOut(p.at), upstream(p.at))
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 网格卡那一族（票 #467）
// ─────────────────────────────────────────────────────────────────────────────
