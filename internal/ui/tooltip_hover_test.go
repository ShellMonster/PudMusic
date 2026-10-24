package ui

import (
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
	mygoui "github.com/egoist/mygo/ui"
)

// 这一组量的是票 #420：**同一时刻只出一枚悬停提示**。
//
// # 它挡的是什么
//
// 指针从一枚按钮移到旁边另一枚按钮时，**两枚提示会同时挂在画面上**：播放条上
// 「播放模式」和「下载」挨得近，两枚宽提示就视觉上叠在一起。
//
// 用户 2026-10-25 说的：「我移动到播放循环按钮，再移动到下载，两个 hover 会有
// 重叠的情况」。
//
// # 根因
//
// 「同一时刻只出一枚提示」这条规矩本来就有（票 #245 的 E3），但它只管
// **焦点 vs 悬停**——两枚**悬停**提示之间没有互斥：它们各自 `shown`，就都画。
// 而悬停那一档的收尾期是 500ms（上游 HeroUI 的 `closeDelay=500`），于是指针
// 从 A 移到 B 时，A 还在那 500ms 里（还画着），B 又出来了。
//
// 现在改成：**这一遍 build 走完之后挑出该画的那一枚**（`tooltip.go` 的
// `pickTip`），并且「有新的提示来时旧的立刻走、没有新的来时才按 500ms 收」。

// TestOnlyOneHoverTooltipIsOnScreen 钉住「指针从一枚按钮移到另一枚，画面上
// 只有一枚提示」。
//
// ⚠️ **判据是「恰好一枚」**：指针停在第二枚上时，屏幕上就该是第二枚那一条。
func TestOnlyOneHoverTooltipIsOnScreen(t *testing.T) {
	tt := tipRow(t)

	// 指针停在第一枚上：它那一枚的提示出来。
	hoverOn(t, tt, "第一枚")
	if !tt.HasText("提示第一枚") {
		t.Fatalf("指针停到第一枚上，提示没出来（文字有 %q）", tt.Texts())
	}

	// 挪到第二枚上。
	r, ok := tt.Find("第二枚")
	if !ok {
		t.Fatalf("画面上没有「第二枚」，文字有 %q", tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()

	if n := countTips(tt); n != 1 {
		t.Fatalf("指针从第一枚挪到第二枚，屏幕上有 %d 条提示（%q）——"+
			"同一时刻只该有一枚", n, tt.Texts())
	}
	// ⚠️ **旧的那一枚要立刻走**：它还在 500ms 的收尾期里，但那 500ms 是
	// 「没有新的提示来时」的语义——新的来了就不该等。
	if tt.HasText("提示第一枚") {
		t.Errorf("上一枚的提示还挂在屏幕上（文字有 %q）——"+
			"指针落到另一枚上时，上一枚该立刻走", tt.Texts())
	}
	if !tt.HasText("提示第二枚") {
		t.Errorf("指针停到第二枚上，它那一枚的提示没出来（文字有 %q）", tt.Texts())
	}
}

// TestTheTooltipOnTheFocusedAnchorStillShows 钉住「指针和焦点在**同一枚**上时，
// 它照常显示」。
//
// ⚠️ **这是票 #245 的 E3 的另一半**（另一半是「焦点那一枚给指针让位」，
// `TestTheFocusTooltipGivesWayToTheHoveredOne` 盯着）：两个来源指着**同一枚**
// 时，那一枚照常显示——而且**只显示一条**（两个来源共用同一块面板）。
func TestTheTooltipOnTheFocusedAnchorStillShows(t *testing.T) {
	tt := tipRow(t)
	tt.SetFocused(true)
	tt.Frame()

	// Tab 到第一枚：焦点那一枚的提示出来。
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	waitFrames(t, tt, "焦点那一枚的提示出来",
		func() bool { return tt.HasText("提示第一枚") })

	// 指针也停在同一枚上。
	hoverOn(t, tt, "第一枚")
	if !tt.HasText("提示第一枚") {
		t.Errorf("指针和焦点在同一枚上，那一枚的提示没了（文字有 %q）", tt.Texts())
	}
	if n := countTips(tt); n != 1 {
		t.Errorf("指针和焦点在同一枚上，屏幕上有 %d 条提示（%q）——"+
			"同一枚只该有一条", n, tt.Texts())
	}
}

// TestTheHoverTooltipWaitsOutItsCloseDelay 钉住「那 500ms 的收尾期没丢」。
//
// ⚠️ **上游 HeroUI 的 `closeDelay=500`**：指针移开之后提示还赖一会儿——手一抖
// 划过时不至于看不见。删掉它，扫过一排按钮时提示会一闪一闪的。
//
// ⚠️ **这里量的是「指针移到一个没有提示的地方」**：那种情况下**没有新的提示
// 来**，所以旧的那一枚该赖够 500ms。
func TestTheHoverTooltipWaitsOutItsCloseDelay(t *testing.T) {
	tt := tipRow(t)
	hoverOn(t, tt, "第一枚")
	if !tt.HasText("提示第一枚") {
		t.Fatalf("指针停到第一枚上，提示没出来（文字有 %q）", tt.Texts())
	}

	// 指针移开：移到一个谁都没有提示的地方。
	tt.Move(1, 1)
	tt.Frame()
	if !tt.HasText("提示第一枚") {
		t.Fatalf("指针刚移开提示就收了——上游 closeDelay 是 500ms（文字有 %q）", tt.Texts())
	}
	tt.Advance(499 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("提示第一枚") {
		t.Errorf("指针离开不到 500ms 提示就收了——上游 closeDelay 是 500（文字有 %q）", tt.Texts())
	}
	tt.Advance(2 * time.Millisecond)
	tt.Frame()
	if tt.HasText("提示第一枚") {
		t.Errorf("指针离开 501ms 之后提示还挂着——上游 closeDelay 是 500（文字有 %q）", tt.Texts())
	}
}

// TestTheHoverTooltipFollowsThePointer 钉住「快速扫过一排按钮时，提示不闪成
// 一串」。
//
// ⚠️ **一帧挪一枚**：真实指针比帧快，扫过的那几枚根本不会被画到——所以这里量
// 的是「每一帧画面上只有一枚」，而不是「扫完之后剩一枚」。
func TestTheHoverTooltipFollowsThePointer(t *testing.T) {
	tt := tipRow(t)
	for _, name := range []string{"第一枚", "第二枚", "第三枚", "第二枚", "第一枚"} {
		r, ok := tt.Find(name)
		if !ok {
			t.Fatalf("画面上没有 %q，文字有 %q", name, tt.Texts())
		}
		tt.Move(r.X+r.W/2, r.Y+r.H/2)
		tt.Frame()
		if n := countTips(tt); n != 1 {
			t.Fatalf("指针扫到 %q 的时候，屏幕上有 %d 条提示（%q）——"+
				"扫过一排按钮时提示不该闪成一串", name, n, tt.Texts())
		}
	}
}

// TestTheTooltipGoesWithItsAnchor 钉住「锚点不再被画的时候，提示不许留在屏幕
// 上」。
//
// 🔴 **这一条是票 #420 点名要查的那个更坏的情况**：提示的收放靠
// `tipPhase.step`，而 `step` 只在**那个锚点被 build** 的时候才被调用。锚点
// 忽然不画了（状态变了、那一支没走），它的状态就停在「开着」——
//
// ⚠️ **画出来的是「这一遍登记过的」那些**（`tooltip.go` 的 `tipCandidate`）：
// 锚点不画 → 不登记 → 不画。提示挂在屏幕上不动的那条路 therefore 被结构性地
// 堵死了——不是靠状态机碰巧收掉，是**它压根不会被登记**。
func TestTheTooltipGoesWithItsAnchor(t *testing.T) {
	gone := false
	b := newTestShell(t, withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.applyFrameTheme(c)
			ui.Column(c).Padding(40).Children(func() {
				if gone {
					return
				}
				e := ui.Button(c, "会走的锚点").Size(140, 30)
				s.tip(c, e, "会走的提示", tipDefault)
			})
		}
	}))
	tt := b.tt

	hoverOn(t, tt, "会走的锚点")
	if !tt.HasText("会走的提示") {
		t.Fatalf("指针停上去提示没出来（文字有 %q）", tt.Texts())
	}

	// 锚点忽然不画了。
	gone = true
	tt.Frame()
	if tt.HasText("会走的提示") {
		t.Errorf("锚点不再被画了，提示还挂在屏幕上（文字有 %q）——"+
			"它该跟着锚点一起走", tt.Texts())
	}
}

// TestThePlayBarShowsOneTooltipAtATime 在**真的播放条**上钉同一件事。
//
// ⚠️ **这一条是用户报的那一处**：播放条右边那一组里「播放模式」和「下载」挨
// 着，两枚提示都摆在各自锚点的上方居中——宽提示叠在一起最明显的就是这两枚。
//
// ⚠️ **判据数的是「这两枚的提示各自在不在」**：播放条上别的提示（收藏、播放
// 列表、加歌……）不在这一次挪动的范围里，不参与数。
func TestThePlayBarShowsOneTooltipAtATime(t *testing.T) {
	const modeTip = "循环：循环播放（点一下换，右键看全部）"
	const downloadTip = "把正在放的这首下载到本地"

	st := app.State{HasTrack: true, Title: "一首歌", Artist: "某个 UP 主",
		Volume: 0.8, Rate: 1, Source: "online", PlayMode: app.PlayModeLoop}
	b := newTestShell(t, at(1200, coverWinH), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.viewPlayBar(c, st)
		}
	}))
	tt := b.tt
	tt.Frame()

	// 悬停「播放模式」。
	hoverOn(t, tt, "播放模式")
	if !tt.HasText(modeTip) {
		t.Fatalf("播放模式那一枚的提示没出来（文字有 %q）", tt.Texts())
	}

	// 挪到「下载」上。
	r, ok := tt.Find("下载")
	if !ok {
		t.Fatalf("播放条上找不到「下载」那一枚，文字有 %q", tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()

	n := 0
	for _, tip := range []string{modeTip, downloadTip} {
		if tt.HasText(tip) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("指针从「播放模式」挪到「下载」，这两枚的提示里有 %d 条在屏幕上"+
			"（文字有 %q）——同一时刻只该有一枚", n, tt.Texts())
	}
	if tt.HasText(modeTip) {
		t.Errorf("「播放模式」那一枚的提示还挂在屏幕上（文字有 %q）——"+
			"指针落到另一枚上时，上一枚该立刻走", tt.Texts())
	}
	if !tt.HasText(downloadTip) {
		t.Errorf("指针停到「下载」上，它那一枚的提示没出来（文字有 %q）", tt.Texts())
	}
}
