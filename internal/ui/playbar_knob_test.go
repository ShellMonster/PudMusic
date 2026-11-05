// 旋钮那一层的验收（GitHub #135 第二轮评审指出来的三处）。
//
// 这一族断的是**画面上那一颗旋钮**：它画在哪儿、按住的时候有没有更深的影子、
// 键盘焦点落在上面的时候画不画。
//
// ⚠️ **全用像素采样，不看代码**：这几条坏掉的时候界面上「只是不太对」——
// 旋钮探出轨道一点点（0% 时压住「0%」那一行）、按下去变平了、Tab 过去看不见
// 焦点在哪——没有一处会报错。
package ui

import (
	"image"
	"image/color"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// TestTheProgressKnobShowsWithKeyboardFocus 钉「键盘 Tab 过去时旋钮要画」。
//
// 🔴 **这一条是被评审指出来的**：这一票把播放条的进度条改成「旋钮只在悬停或按下
// 时画」，而**焦点圈是画在旋钮上的**（框架的 `paintKnob`：
// `if focus { p.FocusRing(k, ...) }`）——于是 Tab 过去的时候**旋钮和焦点圈一起
// 没了**，焦点在哪完全看不出来（改之前用框架的 `ui.Slider` 时两者都画着）。
//
// 判据：**Tab 过去之后，那一格里要出现旋钮**（主色的那一团）。
func TestTheProgressKnobShowsWithKeyboardFocus(t *testing.T) {
	tt, s := bottomBarTester(t, barState(), 1200, 320, false)

	bar, ok := tt.Find("播放进度")
	if !ok {
		t.Fatalf("画面上找不到进度条，文字有 %q", tt.Texts())
	}
	a := BuildTheme(s.app.SettingsSnapshot(), false).Accent
	accent := color.RGBA{R: a.R, G: a.G, B: a.B, A: a.A}

	// 指针不在进度条上（Tester 的指针在 (0,0)）——所以下面出现的旋钮只能是
	// **焦点**带来的。
	//
	// ⚠️ **量的是「最宽的那一行」**：主色既是旋钮的颜色、也是**轨道填过的那一段**
	// 的颜色（进度条上填过的那一段**悬停/有焦点时**是主色 ✗）。旋钮 16 宽、
	// 轨道 4 宽——所以没画旋钮的时候最宽的一行只有 8 个主色像素。
	//
	// ⚠️ **没 Tab 的时候那一段是前景色**（票 #218：不悬停是前景色，悬停才主色），
	// 所以这一档量到的其实是 **0** 个主色像素——比「8 个」还少。
	if _, wide := widestInkRow(tt.Image(), bar, accent); wide > 12 {
		t.Fatalf("还没 Tab 的时候进度条上就有 %d 宽的一行主色——旋钮不该在那儿", wide)
	}

	// 一路 Tab 到进度条上（焦点顺序取决于树，最多十二下）。
	for i := 1; i <= 12; i++ {
		tt.Key(0, mygoui.KeyTab)
		tt.Frame()
		if _, wide := widestInkRow(tt.Image(), bar, accent); wide > 12 {
			// 找到了：焦点落在进度条上，旋钮画出来了 ✓。
			// 焦点圈跟旋钮是一起的（框架的 `paintKnob` 把它画在旋钮上），
			// 所以旋钮在，焦点圈就在。
			return
		}
	}
	t.Errorf("Tab 了 12 下，进度条上一直没画旋钮——键盘焦点落在那儿的时候它得画")
}

// widestInkRow 找出 box 里「接近 want」的那些像素**最宽的一行**：哪一行、那一行
// 有几个。
//
// ⚠️ **为什么不用「上下沿」**：主色既是**旋钮**的颜色、也是**轨道填过的那一段**
// 的颜色——横着的那根进度条上，填过的那一段**悬停/有焦点时**也是主色（票 #218
// 之后：不悬停是前景色）；竖着的那根在 100% 时整条轨道都是主色 ✗。旋钮比轨道
// **宽**（14~16 对 4），所以量宽度才分得出来。
func widestInkRow(img *image.RGBA, box mygoui.Rect, want color.RGBA) (row, width int) {
	row, width = -1, 0
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		n := 0
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			if near(img.RGBAAt(x, y), want) {
				n++
			}
		}
		if n > width {
			row, width = y, n
		}
	}
	return row, width
}

// darkerThan 数 box 里有多少个像素比 want 暗（亮度更低）。
func darkerThan(img *image.RGBA, box mygoui.Rect, want color.RGBA) int {
	n := 0
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			if luma(p.R, p.G, p.B) < luma(want.R, want.G, want.B)-4 {
				n++
			}
		}
	}
	return n
}

// near 是两个颜色算不算同一个（留一点光栅化的余量）。
func near(a, b color.RGBA) bool {
	const slack = 24
	return absInt(int(a.R)-int(b.R)) <= slack &&
		absInt(int(a.G)-int(b.G)) <= slack &&
		absInt(int(a.B)-int(b.B)) <= slack
}

// 免得这一族跟别的测试重名。
var _ = app.State{}

// volumeBarTesterAtScale 是「带系统字号缩放」的播放条测试器。
//
// ⚠️ **缩放得在建外壳时就给**（`withPrefs`）：框架每帧把 `Preferences.TextScale`
// 缓存下来（`scale.Font` 读的是 `Context.Preferences()`），而字号是**布局**的一部分
// ——建好之后再改，量出来的尺寸是旧缩放的。
func volumeBarTesterAtScale(t *testing.T, st app.State, width, height int, dark bool, sc float32) (*mygoui.Tester, *Shell) {
	t.Helper()
	var b *testShell
	b = newTestShell(t, at(width, height), systemIsDark(dark),
		withPrefs(mygoui.Preferences{TextScale: sc}),
		withView(func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				s.wake.set(c)
				s.applyFrameTheme(c)
				mygoui.Column(c).Fill().Children(func() {
					mygoui.Row(c).Grow(1)
					s.viewPlayBar(c, st)
				})
			}
		}))
	tt := b.tt
	tt.Frame()
	return tt, b.s
}

// accentExtent 是「盒子里主色像素的 x 范围」。
//
// ⚠️ **量的是范围，不是宽度**：音量那根滑块上，主色既是旋钮的颜色、也是轨道
// **填过的那一段**的颜色——两者都居中于那一格，于是这个范围的中心就是柱子的中心。
func accentExtent(img *image.RGBA, box mygoui.Rect, accent color.RGBA) (lo, hi int) {
	lo, hi = -1, -1
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			if !near(img.RGBAAt(x, y), accent) {
				continue
			}
			if lo < 0 || x < lo {
				lo = x
			}
			if x > hi {
				hi = x
			}
		}
	}
	return lo, hi
}
