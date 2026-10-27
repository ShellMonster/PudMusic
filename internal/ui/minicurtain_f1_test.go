package ui

// 迷你播放器那个「歌词卷帘」的 **F1**（PR #590 第 1 轮 critical）这一组。
//
// # F1 说的是什么
//
// 「普通点击迷你窗封面 ⇒ 应用**要激活**（迷你窗是 `Panel`，点它不激活应用）
// ⇒ **Esc 到得了这一扇窗** ⇒ 收起。」
//
// 评审实测（2026-10-27 真机）：**Finder 保持前台，点封面之后应用压根没被
// 激活**，于是 Esc 走的是**访达**，卷帘收不起来。
//
// # 这一组量的是**应用自己那一段**
//
// ⚠️ **「物理鼠标点上去」那一跳在无头这一档量不到**：它是 macOS 那一侧
// （WindowServer → AppKit → 视图）的事，而这一档压根没有窗口。
//
// 而**应用自己那一段**（点击 → 命中 → 那一档处理 → 激活 → 键盘）是可以量的：
//
//  1. **指针点击**（`Tester.ClickAt` = 按下 + 松开 + **命中测试**）——这一条
//     与物理点击走的是**同一条路**：框架的指针路径在「松开」时把那个元素的
//     `clicks` 加一（`mygo/ui/input.go:477`），而辅助功能那条路径加的是**同
//     一个计数器**（`mygo/ui/access.go:522`）——两条路在应用这一侧是**同一件
//     事**。
//  2. **激活那一档真的跑了**（`wantsKey` + 重试窗口开着）。
//  3. **Esc 收得起**（窗口级 `c.Shortcut(0, ui.KeyEscape)` 那一档）。
//
// # 真机那一半
//
// 真机证据在收尾说明里（`$TMPDIR/BMG-307/native/f1-evidence-r2.txt`）：别的
// 应用在前台 → **按屏幕坐标点封面** → 应用变成前台 + 卷帘展开 → Esc → 收起。

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestTheMiniCurtainAnswersAPointerClickAndTakesTheKeyboard 盯「普通点击封面
// ⇒ 应用被激活 ⇒ Esc 收得起」（票 #307 修 F1）。
//
// 🔴 **点击是「指针」那一档**（`tt.ClickAt` = 按下 + 松开 + 命中测试），
// **不是**辅助功能动作——票面要的是「普通点击」。
//
// ⚠️ **判据是「激活那一档跑了」**，不是「应用成了前台」：无头这一档压根没有
// 应用（`mygo.App.IsReady()` 是假），成没成前台量不到——而**能不能成前台**
// 那一半在真机证据里。
func TestTheMiniCurtainAnswersAPointerClickAndTakesTheKeyboard(t *testing.T) {
	s, a := miniCurtainShell(t)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停
	a.PutLyricsFrom("", "[00:00.00]第一句\n[00:05.00]第二句\n", "")
	tt := miniCurtainTester(s)
	tt.Frame()

	// 🔴 **指针点击封面**（按下去 + 松开 + 命中测试）。
	clickCurtainCover(t, tt)
	settleCurtain(t, tt, s, true)
	if !s.miniCurtain.open {
		t.Fatalf("指针点封面之后卷帘没有展开")
	}

	// 🔴 **激活那一档跑了**：键盘要抢到这一扇窗上（不然 Esc 到不了它）。
	if !s.miniCurtainWantsKey {
		t.Errorf("点封面之后没有把键盘抢到这一扇窗上（`wantsKey` 是假）：" +
			"Esc 走的是用户原来那个应用，卷帘收不起来（F1）")
	}
	// 🔴 **而且开了「重试激活」的窗口**：点封面那一刻应用**不是**前台（无头
	// 这一档压根没有前台应用），而 `activateIgnoringOtherApps:` **不是每次都
	// 成**——所以必须留一个重试窗口（票 #307 修 F1 的第二半）。
	if !s.miniCurtainFocus.need {
		t.Errorf("点封面那一刻应用不是前台，却没有开「重试激活」的窗口：" +
			"激活落空之后就没有第二次机会了（F1）")
	}
	if !s.miniCurtainFocus.until.After(tt.Now()) {
		t.Errorf("「重试激活」的窗口已经关了（until=%v，现在 %v）",
			s.miniCurtainFocus.until, tt.Now())
	}

	// 🔴 **Esc 收得起**（窗口级 `c.Shortcut(0, ui.KeyEscape)` 那一档）。
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	settleCurtain(t, tt, s, false)
	if s.miniCurtain.open {
		t.Errorf("按了 Esc 之后卷帘还开着：键盘没到这一扇窗上（F1）")
	}
}
