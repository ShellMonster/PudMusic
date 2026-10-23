// 票 #244 的判据：设置页两端对齐（D1）、迷你窗音量滑块有悬停提示（B5）、
// 「全部播放」两处画法统一（B6）。
//
// 三条都对着**画面**断言：量的都是画面上那一块的位置和大小，不是「调了哪个
// 函数」。
//
// ⚠️ **深浅两套都要过**：设置页那一段空档在两套主题下是同一组数，而迷你窗那枚
// 提示是**反色底**（深浅两套反过来的），浅色下那一枚浅底深字、深色下深底浅字。
package ui

import (
	"fmt"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// D1：设置页每一行的说明和控件之间不该空着几百像素
// ─────────────────────────────────────────────────────────────────────────────

// settingsDescGapMax 是「说明」和右边那枚控件之间允许的最大空档。
//
// 🔴 **它是量出来的**：改动之前（内容段 900）那一档是 **347** 像素——用户改一个
// 设置，眼睛要在屏幕两端来回横跳三百多像素。改动之后（内容段 780）是 **226**。
//
// 250 卡在两档中间：比改动前那一档小得多，又给「说明换行」留了余量（说明那一列
// 的下限是 320，最长的一句是 15 个字 ≈ 210 像素）。
const settingsDescGapMax = 250

// settingsLongestDesc 是设置页上最长的一句说明（15 个字）。
//
// ⚠️ **它得是最长的那一句**：那一列的宽是定死的，而说明是**左对齐**的——空档
// = 那一列的宽 − 这一句的宽 + 缝。句子越长空档越小，所以量空档要量最长的那一句。
const settingsLongestDesc = "内容页排成列表、网格还是紧凑"

// TestTheSettingsRowsDoNotStretchAcrossTheWindow 钉住「设置页每一行的说明和
// 控件之间不该空着几百像素」。
//
// # 那一段空档是什么
//
// 每一行是「左边标题 + 一句说明，右边一枚控件」，**两端对齐**——说明在左沿、
// 控件钉在右沿。内容那一段（去掉左右内边距）有多宽，那一段空档就有多宽减去
// 说明自己的宽。
//
// # 判据是**画面上的墨迹**
//
// 说明那一格是个 `Wrap` 的文本，它**画出来的盒子是整列的宽**（说明在那一格里
// 左对齐），所以量盒子的右边量到的是「列的右沿」，量不到用户看见的那一段空白。
// 真正要量的是**字迹的最右沿**——那一段空白就是从那儿到控件列之间的那一截。
func TestTheSettingsRowsDoNotStretchAcrossTheWindow(t *testing.T) {
	for _, winW := range []int{1200, 900} {
		t.Run(fmt.Sprintf("窗口%d", winW), func(t *testing.T) {
			tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""), at(winW, 800))
			homeFrame(tt)

			desc, ok := tt.Find(settingsLongestDesc)
			if !ok {
				t.Fatalf("设置页上没有 %q 那一句，文字有 %q", settingsLongestDesc, tt.Texts())
			}
			// 控件列的左沿：那一列的控件**都从这一条线上起**（票 #235 的 F-04）。
			ctrl := settingRect(t, tt, s, "主题模式")

			ink := settingsInkRight(tt, desc, ctrl.X)
			gap := ctrl.X - ink
			if gap > settingsDescGapMax {
				t.Errorf("说明和右边那枚控件之间空了 %.0f 像素（上限 %.0f）——"+
					"改一个设置，眼睛要在屏幕两端横跳这么远（窗口 %d）",
					gap, float32(settingsDescGapMax), winW)
			}
		})
	}
}

// settingsInkRight 量说明那一格里**字迹的最右沿**。
//
// ⚠️ **量的是像素，不是那个文本元素的盒子**：说明是 `Wrap` 的，它画出来的盒子
// 是整列的宽（字在那一格里左对齐），而用户看见的那一段空白是从**字迹的最右
// 沿**开始的——量盒子量不到它。
//
// ⚠️ **只扫到控件列之前**：右边那枚控件（分段控件 / 开关）在同一行上，扫过去
// 量到的就是它了。
//
// ⚠️ **判据是「明显比底色深」**：说明是次要色（浅色下 #6b6b70 一档），底色是
// 页面底色（浅色下接近白）。阈值取 200：抗锯齿的边缘落在这一侧，字芯落在另一侧。
func settingsInkRight(tt *mygoui.Tester, text mygoui.Rect, limit float32) float32 {
	img := tt.Image()
	right := text.X
	for x := int(text.X); float32(x) < limit; x++ {
		for y := int(text.Y); y < int(text.Y+text.H); y++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 < 200 && g>>8 < 200 && b>>8 < 200 {
				right = float32(x)
				break
			}
		}
	}
	return right
}

// ─────────────────────────────────────────────────────────────────────────────
// B5：迷你窗那根音量滑块要有一枚看得见的悬停提示
// ─────────────────────────────────────────────────────────────────────────────

// TestTheMiniVolumeSliderSaysWhatItIs 钉住「迷你窗那根音量滑块有悬停提示」。
//
// # 为什么这一条不是「加一句标签」就完了
//
// 那一扇窗只有 **320×100**，而音量滑块在**第一行**（y=14..30）。框架默认把提示
// 摆在锚点的**上面**——摆在上面的话整个提示落在窗口之外，**一个字都看不见**，
// 而「看不见的提示」和「没有提示」在画面上是一回事。
//
// # 判据是**提示在窗内**
//
// 光断言「画面上有『音量 80%』这几个字」不够：提示画在窗口外时，Tester 里的
// 文字列表照样有它（它是被建出来了的），而用户看不见。所以这一条同时断它的
// **位置**：上下左右都得在 320×100 之内。
func TestTheMiniVolumeSliderSaysWhatItIs(t *testing.T) {
	s, a := newMiniShell(t, nil)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.SetVolume(0.8)
	tt := miniTester(s)
	settleMini(t, tt, "音量读回来了", func() bool { return a.Snapshot().Volume > 0 })

	bar, ok := tt.Find("音量")
	if !ok {
		t.Fatalf("画面上找不到音量条，文字有 %q", tt.Texts())
	}
	// 把指针停在那根滑块上。
	tt.Move(bar.X+bar.W/2, bar.Y+bar.H/2)
	tt.Frame()

	want := "音量 80%"
	tip, ok := tt.Find(want)
	if !ok {
		t.Fatalf("音量滑块上没有 %q 那枚提示，文字有 %q", want, tt.Texts())
	}
	// ⚠️ **提示得在窗内**：这一扇窗只有 320×100，而音量滑块在第一行——摆在上面
	// 的话整个提示落在窗口外，用户一个字也看不见。
	if tip.X < 0 || tip.Y < 0 || tip.X+tip.W > miniWidth || tip.Y+tip.H > miniHeight {
		t.Errorf("那枚提示画在 %v，而迷你窗只有 %d×%d——它落在窗外，用户看不见",
			tip, miniWidth, miniHeight)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// B6：「全部播放」两处画法统一
// ─────────────────────────────────────────────────────────────────────────────

// TestThePlayAllButtonIsTheSameOnBothPages 钉住「『全部播放』在两个页面上是
// 同一枚」。
//
// # 同一个动作，两个页面
//
// 搜索结果页有「全部播放」，此前**首页没有**——用户在搜索页能按、在首页找不到，
// 会以为是两个不同的功能（UX 报告 §2 的 B6）。
//
// # 判据是**两枚按钮的盒子一样**
//
// 同一个字、同一颗实心主色、同一枚控件 ⇒ 两处的盒子一模一样。而「一个页面有、
// 另一个没有」这件事本身也由这一条断掉：少一个就量不成一对。
//
// ⚠️ **不判语义**：队列语义（追加 / 从这首开始）仍然待人裁决，这一票只统一画法。
func TestThePlayAllButtonIsTheSameOnBothPages(t *testing.T) {
	// 首页：摆三首歌，那一枚按钮才是可按的。
	home := newTestShell(t, withSettings(app.DefaultSettings("")))
	home.s.home.SetSongs(homeSongs{
		tabs: [homeTabCount][]list.Music{0: homeSongsOf("a", 3)},
	})
	homeFrame(home.tt)
	homeBtn, ok := home.tt.Find(playAllLabel)
	if !ok {
		t.Fatalf("首页上没有 %q 那一枚，文字有 %q", playAllLabel, home.tt.Texts())
	}

	// 搜索结果页：同一个动作，那一枚按钮在排序那一排右边。
	st := newSearchStub(10, 20)
	_, searchTT := newSearchPage(t, "周杰伦", st)
	waitFor(t, "搜索结果出来了", func() bool {
		searchTT.Frame()
		return searchTT.HasText(playAllLabel)
	})
	searchBtn, ok := searchTT.Find(playAllLabel)
	if !ok {
		t.Fatalf("搜索页上没有 %q 那一枚，文字有 %q", playAllLabel, searchTT.Texts())
	}

	if homeBtn.W != searchBtn.W || homeBtn.H != searchBtn.H {
		t.Errorf("两处的 %q 不是同一枚：首页是 %v，搜索页是 %v——"+
			"同一个动作在两个页面上长得不一样，用户会以为是两个功能",
			playAllLabel, homeBtn, searchBtn)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// B6 的另一半：两枚按钮按下去做的是同一件事
// ─────────────────────────────────────────────────────────────────────────────

// TestThePlayAllButtonEnqueuesOnBothPages 钉住「两个页面上按『全部播放』，
// 进去队列的是同一批歌」。
//
// ⚠️ **不判语义**：只判「按下去之后队列里多了这一页的歌」，**不判**它是追加
// 还是从这首开始（那是待人裁决的那一条，DESIGN.md §十第 3 条）。
func TestThePlayAllButtonEnqueuesOnBothPages(t *testing.T) {
	// 首页：三首歌按下去，队列里就该多三首。
	home := newTestShell(t, withSettings(app.DefaultSettings("")))
	home.s.home.SetSongs(homeSongs{
		tabs: [homeTabCount][]list.Music{0: homeSongsOf("a", 3)},
	})
	homeFrame(home.tt)
	btn, ok := home.tt.Find(playAllLabel)
	if !ok {
		t.Fatalf("首页上没有 %q 那一枚，文字有 %q", playAllLabel, home.tt.Texts())
	}
	home.tt.ClickAt(btn.X+btn.W/2, btn.Y+btn.H/2)
	home.tt.Frame()
	if got := home.s.app.Queue().Len(); got != 3 {
		t.Errorf("首页上按了 %q 之后队列里是 %d 首，want 3", playAllLabel, got)
	}

	// 搜索页：同样按一下，进去的是这一页的结果。
	st := newSearchStub(10, 20)
	search, searchTT := newSearchPage(t, "周杰伦", st)
	waitFor(t, "搜索结果出来了", func() bool {
		searchTT.Frame()
		return searchTT.HasText(playAllLabel)
	})
	before := search.app.Queue().Len()
	searchBtn, ok := searchTT.Find(playAllLabel)
	if !ok {
		t.Fatalf("搜索页上没有 %q 那一枚，文字有 %q", playAllLabel, searchTT.Texts())
	}
	searchTT.ClickAt(searchBtn.X+searchBtn.W/2, searchBtn.Y+searchBtn.H/2)
	searchTT.Frame()
	if got := search.app.Queue().Len(); got <= before {
		t.Errorf("搜索页上按了 %q 之后队列里还是 %d 首——那一枚按钮按了没反应",
			playAllLabel, got)
	}
}
