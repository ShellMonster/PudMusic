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
	"fmt"
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

// TestTheVolumePercentSitsUnderTheSlider 钉「浮层底下那个百分比和滑块居中对齐」。
//
// 用户 2026-10-25 说的（票 #485）：「音量播放那个调整音量的，底下的 100% 什么的，
// 跟那个柱子的不是对齐的，看起来数字的靠左」。
//
// 断两样：
//
//   - **结构**：百分比那一格的**中心**和滑块那一格的中心在同一条竖线上。
//   - **像素**：字（墨迹）的中心线和那根柱子（主色）的中心线重合。
//
// ⚠️ **结构那条断的是「中心」，不是「同一个盒子」**：百分比那一格是 **40** 宽
// （比滑块那 32 宽，好让字号放大时那几个字不折行——见
// `TestTheVolumePercentDoesNotWrapAtBigTextScales`），而列是 `AlignItems(Center)`：
// 这一格在 32 宽的内容区里居中，于是**中心重合**、左右各探出 4（正好铺满列）。
//
// ⚠️ **像素那条才断得住「盒子对了而字靠左」**：改之前就是那样——格子 40 宽、
// 字靠左，墨迹中心比柱子中心**偏左 7.3 像素**。
//
// ⚠️ **容差 1.5 像素**：12 号字下墨迹宽度的**奇偶**会让中心差半个像素（同一份
// 画面，浅色下墨迹 25 宽、深色下 26 宽）。
//
// ⚠️ **两档都要断**：80% 那一档字窄（排版宽 26.3），而**用户点名的是 100%**
// （排版宽 31.8，最宽的一档）——只断前者的话，那一档坏了照样绿。
func TestTheVolumePercentSitsUnderTheSlider(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		for _, c := range []struct {
			vol  float64
			text string
		}{{0.8, "80%"}, {1, "100%"}} {
			t.Run(fmt.Sprintf("%s-%s", name, c.text), func(t *testing.T) {
				st := barState()
				st.Volume = c.vol
				tt, s := bottomBarTester(t, st, 1200, 320, dark)
				hoverVolumeKey(t, tt)

				sl, ok := tt.Find("音量")
				if !ok {
					t.Fatalf("浮层里没有那根音量滑块，文字有 %q", tt.Texts())
				}
				pct, ok := tt.Find(c.text)
				if !ok {
					t.Fatalf("浮层里没有那个百分比，文字有 %q", tt.Texts())
				}

				// 结构：两个盒子的中心在同一条竖线上。
				slMid := sl.X + sl.W/2
				pctMid := pct.X + pct.W/2
				if absF(pctMid-slMid) > 0.01 {
					t.Errorf("百分比那一格的中心在 x=%.1f，而滑块那一格的中心在 x=%.1f"+
						"（差 %.2f）——字和柱子不在同一条竖线上",
						pctMid, slMid, absF(pctMid-slMid))
				}

				// 像素：柱子的中心 = 主色像素的 x 范围中心（旋钮和轨道都居中于那一格）；
				// 字的中心 = 墨迹的 x 范围中心。
				th := BuildTheme(s.app.SettingsSnapshot(), dark)
				img := tt.Image()
				klo, khi := accentExtent(img, sl, rgb(th.Accent))
				if klo < 0 {
					t.Fatalf("那根滑块上量不到主色像素 %v", sl)
				}
				tlo, thi, n := inkExtent(img, pct, rgb(th.Background))
				if n == 0 {
					t.Fatalf("那个百分比上量不到墨迹")
				}
				kMid := float32(klo+khi) / 2
				tMid := float32(tlo+thi) / 2
				if absF(kMid-tMid) > 1.5 {
					t.Errorf("百分比的墨迹中心在 x=%.1f，而那根柱子的中心在 x=%.1f"+
						"（差 %.1f）——数字没有和柱子对齐",
						tMid, kMid, absF(kMid-tMid))
				}
			})
		}
	}
}

// TestTheVolumePercentDoesNotWrapAtBigTextScales 钉「字号放大时浮层里那个百分比
// 不折行」。
//
// 🔴 **这一条是评审抓出来的回归**（票 #485 的第二轮）：把百分比那一格收成和滑块
// 同宽（32）之后，「100%」在 12 号字下的排版宽是 **31.8**，格子只剩 **0.2 像素**
// 余量——系统字号放大到 **1.05 倍**它就折成两行（画成「100」换行「%」），而改动前
// 要到 1.40 倍才折。
//
// ⚠️ **macOS 上看不出问题**：它的 TextScale 恒为 1。Windows 读注册表
// TextScaleFactor（`mygo/internal/windows/system.go:438`）、Linux 读 GNOME 的
// text-scaling-factor（`mygo/internal/linux/system.go:406`）——那两边放大是常态。
//
// 判据是**那一格的高度**：一行 ≈ 字号 × 缩放 × 1.18（框架的行高），超过「一行半」
// 就是折行了（和 `TestTheSettingsDescriptionDoesNotWrapAtBigTextScales` 同一个
// 量法）。
//
// 顺带断**放大之后中心仍重合**：字居中于那一格，格子居中于列的内容区，于是两个
// 中心一直是一条线——这条断了的话，字放大后会偏出去。
func TestTheVolumePercentDoesNotWrapAtBigTextScales(t *testing.T) {
	for _, sc := range []float32{1, 1.05, 1.25, 1.4, 1.5, 1.75} {
		t.Run(fmt.Sprintf("%.2f倍", sc), func(t *testing.T) {
			st := barState()
			st.Volume = 1
			tt, _ := volumeBarTesterAtScale(t, st, 1200, 320, false, sc)
			hoverVolumeKey(t, tt)

			sl, ok := tt.Find("音量")
			if !ok {
				t.Fatalf("浮层里没有那根音量滑块，文字有 %q", tt.Texts())
			}
			pct, ok := tt.Find("100%")
			if !ok {
				t.Fatalf("浮层里没有那个百分比，文字有 %q", tt.Texts())
			}

			// 一行字的高 ≈ 12 × 缩放 × 1.18；一行半是「还没折」的宽限。
			if line := 12 * sc * 1.18; pct.H > line*1.5 {
				t.Errorf("%.2f 倍字号下那个百分比折行了（高 %.1f，一行是 %.1f）——"+
					"「100」换行「%%」既难看，又把浮层撑高、把那个百分比挤到离滑块"+
					"更远的地方去", sc, pct.H, line)
			}
			// 放大之后，中心仍得重合。
			if absF((pct.X+pct.W/2)-(sl.X+sl.W/2)) > 0.01 {
				t.Errorf("%.2f 倍字号下百分比那一格的中心在 x=%.1f，而滑块那一格的"+
					"中心在 x=%.1f——放大之后两者不重合了",
					sc, pct.X+pct.W/2, sl.X+sl.W/2)
			}
		})
	}
}

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
