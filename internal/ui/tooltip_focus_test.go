package ui

import (
	"testing"
	"time"

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
// TestTheFocusTooltipGivesWayToTheHoveredOne 钉住「**指针悬停的那一枚优先**」。
//
// 🔴 **指针是比键盘焦点更贴近当下操作的信号**（票 #245 的评审）：焦点还停在
// 顶栏「打开文件」那一枚上，而鼠标移到了「迷你」那一枚——屏幕上要留的是**「迷你」
// 那一句**（用户当下指着的东西），而不是焦点那一枚的旧说明。
//
// ⚠️ **「焦点那一枚」自己也可能是被悬停的** —— 那时两档指的是同一枚，它当然显示。
func TestTheFocusTooltipGivesWayToTheHoveredOne(t *testing.T) {
	tt := tipRow(t)
	tt.SetFocused(true)
	tt.Frame()

	// 焦点走到第一枚上：它的提示出来。
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	waitFrames(t, tt, "焦点那一枚的提示出来",
		func() bool { return tt.HasText("提示第一枚") })

	// 指针移到第二枚上：屏幕上留的是「提示第二枚」。
	r, ok := tt.Find("第二枚")
	if !ok {
		t.Fatalf("画面上没有「第二枚」，文字有 %q", tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	waitFrames(t, tt, "指针那一枚的提示出来",
		func() bool { return tt.HasText("提示第二枚") })
	if tt.HasText("提示第一枚") {
		t.Errorf("焦点那一枚的提示还挂在屏幕上（文字有 %q）——"+
			"指针那一枚该优先", tt.Texts())
	}
}

// TestTheTooltipSettlesOnItsOwn 钉住「延时到了之后，界面**自己**画那一帧」。
//
// 🔴 **这一条是回归**（票 #245 的评审抓到的）：把提示的「出/收」两档拆成状态机
// 的时候，`c.After(wait)` 那两句被顺手删掉了——而那两句**就是那一帧的安排**。
//
// ⚠️ **界面在空闲时是没有帧的**：没人安排的话，指针一离开，提示就赖在屏幕上
// 直到下一次碰巧的帧；而 300ms 那一档（出提示要等）则**永远出不来**。
//
// ⚠️ **仓库自己的验收命令抓不到**：那些用例 `Advance` 之后又**手动画了一帧**，
// 把缺的那一帧补上了——所以「手动画一帧之后的状态」是对的，只有「**没人画帧
// 的时候**」才露馅。
//
// 所以这一条的关键是：**推进时钟之后不主动画帧**，直接读画面。
func TestTheTooltipSettlesOnItsOwn(t *testing.T) {
	// ① 指针移开之后，提示自己收掉。
	_, tt := tipProbe(t, "自己会收", tipDefault)
	hoverOn(t, tt, "锚点")
	if !tt.HasText("自己会收") {
		t.Fatalf("指针停上去提示没出来（文字有 %q）", tt.Texts())
	}
	// 移开这一帧要画：提示的「收」是从这一帧开始算的（`c.After` 也是这一帧
	// 安排的）。
	tt.Move(1, 1)
	tt.Frame()
	// ⚠️ **只推进时钟，不画帧**：收提示的那一帧是提示自己安排的。
	tt.Advance(501 * time.Millisecond)
	if tt.HasText("自己会收") {
		t.Errorf("指针离开 501ms 之后提示还挂在屏幕上（文字有 %q）——"+
			"收提示的那一帧没人安排（`c.After`）", tt.Texts())
	}

	// ② 出提示要等 300ms 那一档：延时到了，提示自己出来。
	_, tt2 := tipProbe(t, "自己会出", tipTiming{open: tipDelayVolume, close: tipDelayClose})
	hoverOn(t, tt2, "锚点")
	if tt2.HasText("自己会出") {
		t.Fatalf("指针刚停上去提示就出来了——这一档的 open delay 是 300ms")
	}
	// ⚠️ **同样只推进时钟**：出提示的那一帧是提示自己安排的。
	tt2.Advance(301 * time.Millisecond)
	if !tt2.HasText("自己会出") {
		t.Errorf("301ms 之后提示还没出来（文字有 %q）——"+
			"出提示的那一帧没人安排（`c.After`）", tt2.Texts())
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
	const pipTip = "缩成 320×84 的小窗贴边悬浮，音乐不停"

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
