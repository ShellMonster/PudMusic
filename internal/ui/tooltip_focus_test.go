package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
	mygoui "github.com/egoist/mygo/ui"
)

// 这一组量的是票 #245 的 E3：**提示条该互斥，不是各弹各的**。
//
// ## 它挡的是什么
//
// 键盘 Tab 走位之后，顶栏那几枚控件的提示**一起留在屏幕上**，三句挤成一片，
// 读不出哪句对哪枚图标（UX 报告 §E3，截图 `shots/60-深色-键盘Tab-8.png`）。
//
// 根因是提示的「出/收」原本是一档：`anchor.Hovered() || anchor.FocusVisible()`
// （旧 `tooltip.go` 的 `active`）。焦点从一枚控件走到下一枚时，上一枚的
// `active` 变 false，而**收提示要等 500ms**——那一档的 500ms 等的是「**指针**
// 移开之后」，键盘走位蹭的是「焦点移开」。于是走过三枚就留下三条。
//
// 现在两档分开（`tooltip.go` 的 `tipState.hover` / `tipState.focus`）：
// 悬停那一档照旧 0 出 500 收，焦点那一档 0 出 0 收。

// tipRow 造一个「三枚各带一枚提示」的窗口。
//
// 用 `withView` 把整窗换掉：提示状态挂在 Shell 上（tooltip.go），所以必须走
// 真外壳；而要**精确地数提示条**，提示文字就得是独一无二的——顶栏那几枚的
// 提示（「设置」）和界面上的别处重名，数不出来。
func tipRow(t *testing.T) *mygoui.Tester {
	t.Helper()
	b := newTestShell(t, withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.applyFrameTheme(c)
			ui.Column(c).Padding(40).Children(func() {
				for _, name := range []string{"第一枚", "第二枚", "第三枚"} {
					e := ui.Button(c, name).Size(140, 30)
					s.tip(c, e, "提示"+name, tipDefault)
				}
			})
		}
	}))
	return b.tt
}

// tipTexts 是探针那三枚提示的文字。
var tipTexts = []string{"提示第一枚", "提示第二枚", "提示第三枚"}

// countTips 数画面上有几条提示。
//
// ⚠️ **按文字数，不按浮层数**：Tester 的 `Texts()` 只给画出来的文字（无障碍
// 名字不在里面），而探针那三枚的提示文字是独一无二的——所以「出现了几次」
// 就是「有几条」。
func countTips(tt *mygoui.Tester) int {
	n := 0
	for _, s := range tt.Texts() {
		for _, want := range tipTexts {
			if s == want {
				n++
			}
		}
	}
	return n
}

// TestOnlyOneTooltipIsOnScreenWhileTabbing 钉住「键盘走位时屏幕上只有一条提示」。
//
// ⚠️ **判据是「最多一条」，不是「恰好一条」**：焦点走到没有提示的控件上时，
// 屏幕上就该一条都没有（那才是对的）。
func TestOnlyOneTooltipIsOnScreenWhileTabbing(t *testing.T) {
	tt := tipRow(t)
	// 窗口没有焦点的时候 `Focused()` 恒为 false，焦点那一档根本不会激活——
	// 所以这一条必须先把窗口点亮，否则它测的是「什么提示都不出」。
	tt.SetFocused(true)
	tt.Frame()

	seen := 0
	for i := range 6 {
		tt.Key(0, ui.KeyTab)
		tt.Frame()
		if n := countTips(tt); n > 1 {
			t.Fatalf("Tab 了 %d 次之后屏幕上有 %d 条提示（%q）——"+
				"提示条该互斥，不该各弹各的", i+1, n, tt.Texts())
		}
		if n := countTips(tt); n == 1 {
			seen++
		}
	}
	// ⚠️ **不许是一条都没有**：焦点停在带提示的按钮上时，那一枚的提示就该在。
	// 这一句是为了让上面那个判据不是空的（按钮要是根本拿不到焦点，
	// 「最多一条」就恒成立，这条测试什么也没测）。
	if seen == 0 {
		t.Fatalf("Tab 了 6 次，屏幕上一次都没出过提示（文字有 %q）——"+
			"焦点驱动的那一档没生效，这条测试是空的", tt.Texts())
	}
}

// TestTheHoveredTooltipGivesWayToTheFocusedOne 钉住「悬停的那一枚和焦点的那一枚
// 不同时在屏幕上」。
//
// 这是 E3 的另一半：键盘走位时，指针很可能还停在某枚控件上（用户是从鼠标
// 切到键盘的）。那一枚的**悬停**提示和焦点所在那一枚的**焦点**提示如果同时在，
// 屏幕上就是两条——而两条叠在一起正是「读不出哪句对哪枚」。
func TestTheHoveredTooltipGivesWayToTheFocusedOne(t *testing.T) {
	tt := tipRow(t)
	tt.SetFocused(true)
	tt.Frame()

	// 指针停在第一枚上：出的是它的悬停提示。
	r, ok := tt.Find("第一枚")
	if !ok {
		t.Fatalf("画面上没有「第一枚」，文字有 %q", tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if !tt.HasText("提示第一枚") {
		t.Fatalf("指针停在第一枚上，它的悬停提示没出来（文字有 %q）", tt.Texts())
	}

	// 焦点走到第二枚：第一枚的悬停提示让位，第二枚的焦点提示接手。
	tt.Key(0, ui.KeyTab)
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if n := countTips(tt); n != 1 {
		t.Fatalf("焦点在第二枚、指针在第一枚的时候，屏幕上有 %d 条提示（%q）——"+
			"该只剩一条", n, tt.Texts())
	}
	if !tt.HasText("提示第二枚") {
		t.Errorf("焦点那一枚的提示没出来（文字有 %q）", tt.Texts())
	}
	if tt.HasText("提示第一枚") {
		t.Errorf("指针那一枚的悬停提示还挂在屏幕上（文字有 %q）——"+
			"它该给焦点那一枚让位", tt.Texts())
	}
}

// TestTheNavBarTooltipsDoNotStackWhileTabbing 在**整窗**上钉同一件事。
//
// ⚠️ 探针那一条量的是机制；这一条量的是**真的顶栏**：那一排图标键的提示在
// 键盘走位时会不会一起挂着。判据用两个**界面上独一无二**的顶栏提示
// （「设置」在侧栏菜单里也画着，数不出来，所以不用它）。
func TestTheNavBarTooltipsDoNotStackWhileTabbing(t *testing.T) {
	_, tt := newShell(t)
	tt.SetFocused(true)
	tt.Frame()

	// 顶栏那两枚带提示的图标键。
	const openTip = "打开本地音频文件"
	const pipTip = "缩成 320×100 的小窗贴边悬浮，音乐不停"

	for i := range 10 {
		tt.Key(0, ui.KeyTab)
		tt.Frame()
		n := 0
		for _, s := range tt.Texts() {
			if s == openTip || s == pipTip {
				n++
			}
		}
		if n > 1 {
			t.Fatalf("Tab 了 %d 次之后，顶栏那两枚的提示同时在屏幕上（%q）", i+1, tt.Texts())
		}
	}
}
