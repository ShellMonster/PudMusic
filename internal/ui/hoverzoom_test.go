// 这一族验**三处 hover 放大**（票 #179 的 G45）。
//
//	UP 空间 / 合集网格那张封面	上游 `user-profile/grid-card/index.tsx:26`
//	动态流作者栏那个头像		上游 `dynamic-feed/user.tsx:46`
//	顶栏那个头像			上游 `layout/navbar/user/index.tsx:178`
//
// 三处都是 `hover:scale-105`——放大到 **105%**，网格那一处还带
// `transition-transform duration-300`。
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

// assertGrew 断「那一格在指针移上去之后变大了」。
//
// before 是没放大时量到的盒子，after 是放大后量到的。want 是放大到的倍数。
//
// ⚠️ **留一像素的余量**：那一格是浮点数排版的结果（48 × 1.05 = 50.4），逐位相等
// 不是「对」的证据。
func assertGrew(t *testing.T, before, after mygoui.Rect, want float32, what string) {
	t.Helper()
	if after.W < before.W*want-1 || after.H < before.H*want-1 {
		t.Errorf("%s：指针移上去之后它从 %.1f×%.1f 变成 %.1f×%.1f，"+
			"want 放大到 %.0f%%（上游的 `hover:scale-105`）",
			what, before.W, before.H, after.W, after.H, want*100)
	}
}

// TestTheCollectionCoverGrowsOnHover 钉住 **UP 空间 / 合集那张封面**放大到 105%。
//
// 上游是 `<Image className="h-full w-full transition-transform duration-300
// group-hover:scale-105">`（`user-profile/grid-card/index.tsx:26`）。
//
// ⚠️ **放大的是框里那张图**：外面那个框是 `overflow-hidden` 的，所以画面上是
// 「图在框里涨大」，**框不动**。
//
// ⚠️ **这一处的判据是像素，不是盒子的大小**：那张图在框里，框又是裁着的，于是
// 无头 Tester 量到的永远是**框**的尺寸（`Tester.Find` 给回的是**变换后**的盒子
// 再和**裁剪区**取交集，见 `mygo/ui/runtime.go` 的 `visualRect`）——它不放大。
// 所以这里量的是**画面上那一块的内容变了没有**：图在框里放大 5%，左上角那块
// 深色就往里挪，框子左上角的像素从深色变成浅色。
func TestTheCollectionCoverGrowsOnHover(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "有收藏夹的 UP", 0, 1)
	cover := uiServePNG(b, "cover.png", uiCoverPNG(t, 48, 64))
	b.on("/x/v3/fav/folder/created/list", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []any{
			map[string]any{"id": 100, "title": "点我", "cover": cover, "media_count": 8},
		}})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	sp.tab = upTabCollection
	tt.Frame()
	waitFrames(t, tt, "收藏夹加载完", func() bool { return sp.fav.started })
	tt.Frame()

	// 封面要**真的下来了**才量得到（占位放大 5% 画面上一点不变）。
	waitFrames(t, tt, "封面下来了", func() bool { return s.images.Get(cover).Ready() })
	tt.Frame()

	before, ok := tt.Find(cover)
	if !ok {
		t.Fatalf("画面上找不到那张封面，文字有 %q", tt.Texts())
	}
	beforeImg := tt.Image()

	// 指针移到那一行上，推满 300 毫秒。
	tt.Move(before.X+before.W/2, before.Y+before.H/2)
	tt.Frame()
	tt.Advance(hoverZoomTime)
	tt.Frame()

	after, ok := tt.Find(cover)
	if !ok {
		t.Fatalf("指针移上去之后那张封面不见了，文字有 %q", tt.Texts())
	}
	// 框**不动**：放大的是框里那张图（上游那个 `overflow-hidden` 的框也不动）。
	if after.W != before.W || after.H != before.H {
		t.Errorf("那张封面的框从 %.1f×%.1f 变成 %.1f×%.1f——放大的是**框里那张图**，"+
			"框不该动（上游 `grid-card/index.tsx:22` 的框是 `overflow-hidden` 的）",
			before.W, before.H, after.W, after.H)
	}
	// 框里那一块**变了**：图涨大了 5%，左上角那块深色往里挪了。
	n := uiChangedPixels(beforeImg, tt.Image(), after)
	if n < 20 {
		t.Errorf("指针移上去之后那张封面里只有 %d 个像素变了——"+
			"`group-hover:scale-105` 没画出来", n)
	}
}

// TestTheAuthorAvatarGrowsOnHover 钉住 **动态流作者栏那个头像**放大到 105%。
//
// 上游是 `<Avatar className="h-9 w-9 shrink-0 rounded-full transition-transform
// hover:scale-105">`（`dynamic-feed/user.tsx:46`）——放大的是**整个圆**。
func TestTheAuthorAvatarGrowsOnHover(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "我自己", 0, 1)
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"total": 1, "list": []any{
			map[string]any{"mid": 11, "uname": "第一个 UP", "face": "https://f/author-11.jpg"},
		}})
	})
	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(dynamicFeedPath)
	tt.Frame()
	f := s.dynamicFeedOf()
	waitFrames(t, tt, "人到了", func() bool { return len(f.authors) == 1 })
	tt.Frame()

	before, ok := tt.Find("https://f/author-11.jpg")
	if !ok {
		t.Fatalf("画面上找不到作者栏那个头像，文字有 %q", tt.Texts())
	}

	tt.Move(before.X+before.W/2, before.Y+before.H/2)
	tt.Frame()
	tt.Advance(hoverZoomTime)
	tt.Frame()

	after, ok := tt.Find("https://f/author-11.jpg")
	if !ok {
		t.Fatalf("指针移上去之后那个头像不见了，文字有 %q", tt.Texts())
	}
	assertGrew(t, before, after, hoverZoomScale, "作者栏那个头像")
}

// TestTheNavbarAvatarGrowsOnHover 钉住 **顶栏那个头像**放大到 105%。
//
// 上游是 `<Avatar ... className="mr-4 cursor-pointer transition-transform
// hover:scale-105" />`（`layout/navbar/user/index.tsx:178`）。
func TestTheNavbarAvatarGrowsOnHover(t *testing.T) {
	_, tt := newShell(t)
	tt.Frame()

	before, ok := tt.Find(avatarLabel)
	if !ok {
		t.Fatalf("画面上找不到顶栏那个头像，文字有 %q", tt.Texts())
	}

	tt.Move(before.X+before.W/2, before.Y+before.H/2)
	tt.Frame()
	tt.Advance(hoverZoomTime)
	tt.Frame()

	after, ok := tt.Find(avatarLabel)
	if !ok {
		t.Fatalf("指针移上去之后顶栏那个头像不见了，文字有 %q", tt.Texts())
	}
	assertGrew(t, before, after, hoverZoomScale, "顶栏那个头像")
}
