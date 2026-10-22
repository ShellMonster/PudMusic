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
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// TestTheVerticalKnobStaysInsideItsSlider 钉「竖着的那根旋钮不许画到框外面」。
//
// 🔴 **这一条是被评审指出来的**：旋钮的中心原来画在**边框框**的端上
// （`r.Y + r.H*(1-frac)`），而框架的竖直映射用的是**内容框**
// （`1-(y-st.cy)/st.ch`，`st.cy`/`st.ch` 是内容框的，`mygo/ui/runtime.go:1023`）。
// 两者差着**半个旋钮**：值 0 时旋钮探出内容框 7 像素，**而且压住「0%」那一行**；
// 值 100% 时探出顶边 7 像素。
//
// 判据是**旋钮墨迹的中心**：它该落在内容框的端上（框的端缩半个旋钮）。
func TestTheVerticalKnobStaysInsideItsSlider(t *testing.T) {
	knobH := volSliderLook.knobH
	for _, c := range []struct {
		vol  float64
		name string
	}{
		{0, "0%"},
		{1, "100%"},
	} {
		t.Run(c.name, func(t *testing.T) {
			st := barState()
			st.Volume = c.vol
			tt, s := bottomBarTester(t, st, 1200, 320, false)
			hoverVolumeKey(t, tt)

			sl, ok := tt.Find("音量")
			if !ok {
				t.Fatalf("悬停之后浮层里没有那根音量滑块，文字有 %q", tt.Texts())
			}
			a := BuildTheme(s.app.SettingsSnapshot(), false).Accent
			accent := color.RGBA{R: a.R, G: a.G, B: a.B, A: a.A}

			// ⚠️ **量的是「最宽的那一行」**：主色既是旋钮的颜色、也是**轨道填过
			// 的那一段**的颜色 —— 竖着的那根在 100% 时整条轨道都是主色 ✗（而 0%
			// 时只剩半个旋钮的填充 ✓）。旋钮比轨道**宽**（14 对 4），所以量宽度
			// 才分得出来。
			row, wide := widestInkRow(tt.Image(), sl, accent)
			if wide < 10 {
				t.Fatalf("滑块上没画旋钮（最宽的一行只有 %d 个主色像素），%v", wide, sl)
			}
			// 旋钮的中心该在**内容框**的端上：值 0 在底、值 1 在顶，
			// 而内容框的端 = 边框框的端缩半个旋钮（那个 `PaddingY` 就是为它留的）。
			want := sl.Y + sl.H - knobH/2
			if c.vol == 1 {
				want = sl.Y + knobH/2
			}
			// 最宽的那一行就是旋钮的**中间**那一行（它是个圆）✓。
			//
			// ⚠️ **留两像素的余量**：圆的最宽的那一行和它紧挨着的那一行宽度几乎
			// 一样（光栅化），量出来可能偏一像素。
			got := float32(row)
			if absF(got-want) > 2 {
				t.Errorf("旋钮画在 y=%.1f（最宽的那一行是 %d），want %.1f ——"+
					"它探出了内容框，而框架的竖直映射（内容框上的 1-(y-cy)/ch）"+
					"取的是内容框那一头，两边对不上", got, row, want)
			}
			// 而且它不许探出滑块的框（0% 时它压住了「0%」那一行）。
			if got-float32(knobH)/2 < sl.Y-2 || got+float32(knobH)/2 > sl.Y+sl.H+2 {
				t.Errorf("旋钮探出了滑块的框 %v（中心 y=%.1f）", sl, got)
			}
		})
	}
}

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
	// 的颜色（进度条上填过的那一段是主色 ✗）。旋钮 16 宽、轨道 4 宽 ——
	// 所以没画旋钮的时候最宽的一行只有 8 个主色像素。
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

// TestTheKnobGetsDeeperWhileHeld 钉「按住的时候旋钮底下多一层更深的影子」。
//
// ⚠️ **票面原话**：「另外那几层影子（松手时一层很淡的、按下时一层更深的）也要
// 跟着 held 走，旋钮才有『被按下去』的实感。」
//
// 框架自己的 `paintKnob` 是三层：`held > 0` 时多一层 `drop.Alpha(held)`（深色、
// 往下 8、糊 14）——**少了它，按住到 held=1 时那两层软影子全淡掉，旋钮一层影子
// 都不剩**，观感是「变平了」而不是「被按下去」。
//
// 判据：**旋钮底下那一带，按住的时候比松手的时候暗**。
func TestTheKnobGetsDeeperWhileHeld(t *testing.T) {
	tt, s := bottomBarTester(t, barState(), 1200, 320, false)
	hoverVolumeKey(t, tt)

	sl, ok := tt.Find("音量")
	if !ok {
		t.Fatalf("悬停之后浮层里没有那根音量滑块，文字有 %q", tt.Texts())
	}
	// 旋钮在 80%（这一份状态就是 0.8）→ 旋钮靠上，底下有地方看影子。
	knobY := sl.Y + volSliderLook.knobH/2 +
		(sl.H-volSliderLook.knobH)*(1-0.8)

	// 旋钮底下 11~21 那一带：软影子（往下 1.5、糊 7）够不着，按住时多出来
	// 的那层（往下 8、糊 14）罩得住。
	band := mygoui.Rect{X: sl.X, Y: knobY + 11, W: sl.W, H: 10}
	bg := BuildTheme(s.app.SettingsSnapshot(), false).Background
	bgc := color.RGBA{R: bg.R, G: bg.G, B: bg.B, A: bg.A}

	before := darkerThan(tt.Image(), band, bgc)

	// 按住旋钮：按住的那一趟，框架的 `Animate("held", …)` 要走 150 毫秒才到 1，
	// 而**无头 Tester 的钟站着不动**——所以要推钟（CLAUDE.md §2）。
	tt.Press(sl.X+sl.W/2, knobY)
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	after := darkerThan(tt.Image(), band, bgc)
	tt.Release(sl.X+sl.W/2, knobY)
	tt.Advance(300 * time.Millisecond)
	tt.Frame()

	if after <= before {
		t.Errorf("按住之后旋钮底下那一带没有更暗（松手 %d 个暗像素 / 按住 %d 个）——"+
			"「按下时一层更深的影子」那一层没画", before, after)
	}
}

// widestInkRow 找出 box 里「接近 want」的那些像素**最宽的一行**：哪一行、那一行
// 有几个。
//
// ⚠️ **为什么不用「上下沿」**：主色既是**旋钮**的颜色、也是**轨道填过的那一段**
// 的颜色——横着的那根进度条上，填过的那一段也是主色；竖着的那根在 100% 时整条
// 轨道都是主色 ✗。旋钮比轨道**宽**（14~16 对 4），所以量宽度才分得出来。
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

// TestTheVolumeSliderDoesNotDriftWithThePercentText 钉「滑块不跟着百分比字数横向漂」。
//
// 🔴 **这一条是评审点名缺的那一个护栏**（F6）：行为已经对了（那一列和百分比都定宽），
// 但**没有任何测试能抓住它回退**——把两处定宽一起抽掉，仓库里原有四条相关测试
// **全绿**，而滑块实测会从 1080.3 漂到 1085.0（4.7 像素，而它就在指针底下）。
//
// 判据是**滑块自己的 X**，三档音量各量一次，一像素都不许动：
//
//	音量 0   → 「0%」（两个字）
//	音量 0.5 → 「50%」（三个字）
//	音量 1   → 「100%」（四个字）
//
// 字数不同 → 百分比那行自然宽度不同 → 列宽若跟着它走、而滑块是居中的，
// 滑块就会挪。
func TestTheVolumeSliderDoesNotDriftWithThePercentText(t *testing.T) {
	var first float32
	var firstVol float64
	for i, vol := range []float64{0, 0.5, 1} {
		st := barState()
		st.Volume = vol
		tt, _ := bottomBarTester(t, st, 1200, 320, false)
		hoverVolumeKey(t, tt)

		sl, ok := tt.Find("音量")
		if !ok {
			t.Fatalf("音量 %.1f：浮层里没有那根音量滑块，文字有 %q", vol, tt.Texts())
		}
		if i == 0 {
			first, firstVol = sl.X, vol
			continue
		}
		if absF(sl.X-first) > 0.01 {
			t.Errorf("音量 %.1f 时滑块的 x 是 %.2f，音量 %.1f 时是 %.2f（差 %.2f）——"+
				"它跟着百分比的字数横向漂了：滑块就在指针底下，漂一下拖起来就跳",
				vol, sl.X, firstVol, first, absF(sl.X-first))
		}
	}
}
