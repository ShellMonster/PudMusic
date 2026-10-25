package ui

// 这一族钉住票 #392 的那一条：**波形走的是「只重画不重建」**。
//
// # 为什么要有这一条
//
// 「波形每一帧要一帧」这件事，**改之前**是 `c.AnimationFrame()`——那一帧要把
// **整棵元素树重建一遍**（框架的 `runtime.go` 的 `runFrame`：build + layout +
// paint）。放歌的时候它每一帧都要一帧，于是**每秒重建 60 遍整窗**。
//
// 改之后是 `p.AnimationFrame()`——那一帧走**局部重绘**（`repaintFrame`）：
// 视图**不重建**、布局**不重排**，只有画面重画一遍。
//
// ⚠️ **两者在画面上长得一模一样**（波形照跳），所以**没有一条断言画面的测试
// 能分出它们**——而这一条正是那种「不改任何签名、行为却变了」的改动
// （AGENTS.md §1 末）。所以这里断的是**重建的次数**。
//
// # 判据是「次数」，不是「时间」
//
// 次数不受机器快慢影响（AGENTS.md §2：不要用固定毫秒当判据）：改之前是
// 「每一帧一次」，改之后是「那一次唤醒一次」。而时间类判据在这台机器上
// （常年 load 30+）根本分不出来。

import (
	"testing"
	"time"
)

// TestTheWaveDoesNotRebuildTheWindowEveryFrame 钉住「放歌时整窗不是每一帧重建」。
//
// ⚠️ **量的是「重建了几遍」**：那一条计数钩子在 `countingShell` 的 view 外层
// （它数的是 view 被调了几次——view 被调一次就是重建一遍）。
//
// ⚠️ **先要一次帧**：无头 Tester 是拉式的，不 `Frame()` 一帧都不画，而
// `Advance` 只画「有人要了帧」的那些时刻（`mygo/ui/headless.go` 的 `due()`）。
// 那一次唤醒模拟的是 `App.tick`（放歌时它每 50ms 叫醒每个窗口一次）。
func TestTheWaveDoesNotRebuildTheWindowEveryFrame(t *testing.T) {
	s, tt, frames := countingShell(t)

	// ⚠️ **先钉住「那个波形真的在要下一帧」**：不然下面那条「重建得少」可能是
	// 因为根本没有动画在跑，绿得毫无意义。
	//
	// ⚠️ **判据是 `FramePending`，不是「重建了几次」**：波形要的那些帧现在走
	// **局部重绘**（不重建），数重建是数不出来的。而这一档里**没有别的人
	// 要帧**——`countingShell` 只摆队列不起播（`App` 的 `live` 为假），
	// 于是 `App.tick` 从不叫醒窗口。所以 `FramePending` 为真就是波形要的。
	if !s.wake.invalidateIf(true) {
		t.Fatal("看得见的窗口却把唤醒丢掉了")
	}
	before := *frames
	tt.Advance(500 * time.Millisecond)
	if !tt.FramePending() {
		t.Fatal("推进了半秒，画面却没有在要下一帧——" +
			"跳动波形没有在逐帧动画，下面那条「重建得少」就验不到东西")
	}

	// 500 毫秒是 30 帧（60Hz）。**改之前**这 30 帧每一帧都是一次重建
	//（波形要的那一帧是 `c.AnimationFrame()`，框架于是重建整棵树）；
	// **改之后**只有开头那一次重建——**而那一次是测试自己叫醒带来的**
	//（`s.wake.invalidateIf(true)`，模拟 `App.tick`），波形要的那 29 帧走的是
	// **局部重绘**：视图不重建。
	//
	// ⚠️ **真应用里不是 1 次**：放歌时 `App.tick` 每 50ms 叫醒每个窗口一次，
	// 而**那每一次叫醒都是一次全量重建**（`Services.Invalidate` → `engine.changed`
	// → `rt.redraw = false`）——所以真应用里是**每秒 20 次**，不是 1 次。
	// 这一档之所以只有 1 次，是因为 `countingShell` 只摆队列**不起播**
	//（`App` 的 `live` 为假），于是 `App.tick` 从不叫醒窗口。
	//
	// ⚠️ **上限给到 3**：机器慢的时候帧率会掉（那是好事，方向是同一个），
	// 而偶尔一次别的路径送来一帧重建也不该红。
	got := *frames - before
	t.Logf("推进 500 毫秒（30 帧）重建了 %d 次整棵元素树", got)
	if got > 3 {
		t.Errorf("推进 500 毫秒重建了 %d 次整棵元素树，want ≤ 3。\n"+
			"那 30 帧里只有开头那一次该重建（测试自己叫醒的那一次）——"+
			"波形要的那些帧走的是**局部重绘**"+
			"（`p.AnimationFrame()`，框架的 `repaintFrame`：视图不重建、布局不重排）。\n"+
			"（改之前是 `c.AnimationFrame()`：每一帧都要一帧，而那一帧是**整棵元素树"+
			"重建一遍**——放歌时每秒 60 次，那就是这一票要省的那笔 CPU 和电。）",
			got)
	}
}

// TestTheLikeRingDoesNotRebuildTheWindowEveryFrame 是上一条的那一半：
// **长按进度环**也走局部重绘。
//
// ⚠️ **它比波形那条更要紧**：环画的是「按了多久」，而「多久」是要**算**的
// ——算的那一层在**画的时候**（`likeRingProgress`）。局部重绘那一帧**不重建**，
// 于是算出来的那一份是**冻结**的：环会停在原地不动。
//
// ⚠️ **按住的是封面**（`holdCover`），而封面在播放条上——所以要先把歌放起来
// （那一档的假 B 站会真的把流接上）。
func TestTheLikeRingDoesNotRebuildTheWindowEveryFrame(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	holdCover(t, e)

	// 按住 600 毫秒：环已经出来了（500ms 起画），从这一刻起那些帧走的是
	// 局部重绘。
	advanceLike(e, 600*time.Millisecond)

	// 计数钩子：播放条**每一帧重建一次**（它是在 `viewMain` 里建的），
	// 所以它建了几遍就是整棵树重建了几遍（见 builds.go）。
	e.s.builds = buildTally{}
	// 再按住一秒：这一秒里环一直在动。
	advanceLike(e, time.Second)
	got := e.s.builds.playbar
	t.Logf("环在动的那一秒（60 帧）重建了 %d 次整棵元素树", got)
	// ⚠️ **上限给到 5**：那一秒里后台的唤醒（`App.tick` 每 50ms 一次）会带来
	// 一些重建，那些是**该有的**（后台状态变了要重新画一遍）。
	if got > 5 {
		t.Errorf("环在动的那一秒里重建了 %d 次整棵元素树，want ≤ 5。\n"+
			"环走的是**局部重绘**（`p.AnimationFrame()`）——视图不重建，"+
			"于是「按了多久」是**画的时候**算的（`likeRingProgress`）。\n"+
			"（改之前是 `c.AnimationFrame()`：每一帧重建一遍整棵树。）", got)
	}
}
