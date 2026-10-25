package ui

// 这一组是票 #393 的证据：「窗口不可见 → 不画帧」这条链的**第一环**。
//
// # 这条链一共有三环
//
//	① 窗口不可见 → 不画帧        ← 本文件（MyGo 这一侧，能进 CI）
//	② 不画帧 → 2 秒后框架 trim   ← 框架内部（mygo/internal/gpu/metal），CI 跑不了
//	③ trim → 图形内存回落        ← 真窗口才量得到（phys_footprint），CI 跑不了
//
// ②③ 两条在 `presence.go` 里 `watchWindow` 的注释上写着（带文件:行号），包括
// 「画着时 160~166 MB / 停帧 2 秒后 74~79 MB 是框架的设计，不是泄漏」这一条。
//
// # 为什么①还要再写一条
//
// 「窗口看不见」在应用层有**两种**样子，而它们停帧的**位置不一样**：
//
//   - **「被别的窗口完全盖住」**：帧在**进到 `Shell.View` 之前**就被框架扣住了
//     （`mygo/ui/runtime.go:685-688` → `holdFrame()`）——**这一帧根本不存在**。
//   - **「失焦」**：框架管不了（`NSWindow.occlusionState` 还在 visible 位上），
//     是 MyGo 自己挡的——**挡的是「唤醒」，不是「已经送进来的帧」**。于是这一帧
//     画得出来，只是它**不再要下一帧**。
//
// 第二种样子是这一层（`presence.go`）的责任，而它**没有**被单独钉过：
// 「看不见就不画帧」那条（`window_seen_test.go`）量的是「不请求下一帧」和
// 「一秒里 0 帧」，**没有量「已经送进来的那一帧画不画」**——而那一帧恰恰是
// 用户切回来时看到的画面（`noteWindow` → `invalidateNow` 补的也是这一帧）。
//
// ⚠️ **「被盖住」那一档在 CI 里量不到**：无头 Tester 的 `occluded()` 只看
// `h.hidden`，而它没有对外的开关（`mygo/ui/headless.go:88`），而且 Tester 走
// 软件渲染器，根本不经过 Metal（也就没有 trim）。那一档只能真窗口验，步骤写在
// `presence.go` 的注释里。

import (
	"testing"
	"time"
)

// TestAnUnseenWindowDrawsTheFrameItGetsButAsksForNoMore 钉住「窗口看不见时，
// **已经送进来的那一帧画得出来，但它不再要下一帧**」。
//
// # 为什么这一帧必须画出来
//
// 「看不见」不是「这一扇窗坏掉了」：后台照样在改状态（歌往前走、封面下完、
// 列表翻页），而 view 每帧都从 `Snapshot()` 读当前状态——所以硬塞进来的那一帧
// 画出来就是**此刻**该有的样子。用户切回来的第一眼看到的就是它。
//
// 换句话说：**「这一帧画不出来」和「这一帧画成半张」都是坏处**，而症状一模一样
// （切回来看到的是旧内容或者空白），所以这一条得单独钉。
func TestAnUnseenWindowDrawsTheFrameItGetsButAsksForNoMore(t *testing.T) {
	s, tt, frames := countingShell(t)

	// 先钉住**看得见的时候真的在逐帧画**：不然下面那些「看不见」可能只是
	// 因为根本没有动画在跑，绿得毫无意义。
	if !s.wake.invalidateIf(true) {
		t.Fatal("看得见的窗口却把唤醒丢掉了")
	}
	before := *frames
	tt.Advance(500 * time.Millisecond)
	if *frames <= before {
		t.Fatalf("看得见、有歌在放，推进半秒却只画了 %d 帧——"+
			"跳动波形没有在要下一帧，下面那些「看不见」就验不到东西",
			*frames-before)
	}

	// 看不见（失焦：框架管不了的那一档，是这一层补的）。
	s.noteWindow(func(p *windowPresence) { p.focused = false })
	// 这一帧是「最后那一下」：正在飞的动画要把它收掉。它画几帧都行。
	tt.Frame()
	if tt.FramePending() {
		t.Fatalf("窗口看不见了，画面还在请求下一帧")
	}

	// 别的路径硬塞一帧进来：**绕开** `wakeHandle` 那道「看不看得见」的闸门，
	// 直接叫 `Services.Invalidate`。
	//
	// ⚠️ 生产里真有这样的路径（本地库扫完那一支直接调 `Services.Invalidate`
	// ——见 shell_view.go 里 `hover.SetVisibleFlag` 那一段的注释），所以这一档不是
	// 假想的。
	before = *frames
	beforeBar := s.builds.playbar
	invalidateOutside(t, s)
	tt.Advance(50 * time.Millisecond)

	// 这一帧**画出来了**。
	//
	// ⚠️ **这里有两条判据，分工不一样，缺一条就漏一种坏处**：
	//
	//   - 帧计数（`frames`）记的是「**view 被调用了几次**」——它抓的是「这一帧
	//     根本没进到 view」（有人要它、但它没来）。
	//   - `builds.playbar` 记的是「**界面建了几遍**」（钩子在 builds.go）——
	//     它抓的是「view 进了、但界面没建」，比如 view 开头就 return 了。
	//
	// 两条都验过（各自反向验证过一次：把 view 改成看不见就 return，只有第二条
	// 会红；把送帧的那一句去掉，两条都会红）。
	if *frames <= before {
		t.Errorf("看不见的窗口收到一帧之后，view 一次都没被调用——" +
			"这一帧是用户切回来时看到的，它不能丢")
	}
	// 而且是**建出来了**，不是画了半张：播放条那一条界面建过。
	//
	// ⚠️ 「建」比「画」更严：`builds.playbar` 记的是「`viewPlayBar` 被调用过
	// 几次」，而它是在**建**的过程中加的——建到一半崩了也加，所以这一条顺带
	// 挡住「这一帧画的是错误页」。
	if got := s.builds.playbar - beforeBar; got == 0 {
		t.Errorf("看不见的窗口收到一帧之后，界面一次也没建（builds.playbar %d→%d）——"+
			"用户切回来看到的是空的",
			beforeBar, s.builds.playbar)
	}

	// 而它**不再要下一帧**：`hover.SetVisibleFlag` 装的那一个开关（这一扇窗
	// 看不见时是 false）挡住了自发动画（波形、频谱），所以这一帧画完就停。
	if tt.FramePending() {
		t.Errorf("看不见的窗口画完这一帧之后还在请求下一帧——" +
			"这一帧画完就该停（`hover.SetVisibleFlag` 装的那一个开关挡住了自发动画）")
	}

	// 而那一帧没有引出下一帧：往后一秒，一帧都不画、一次界面都不建。
	before = *frames
	beforeBar = s.builds.playbar
	tt.Advance(time.Second)
	if got := *frames - before; got != 0 {
		t.Errorf("看不见的窗口在一秒里画了 %d 帧，want 0——"+
			"用户看不到这一扇窗，那些帧全是白烧的电", got)
	}
	if got := s.builds.playbar - beforeBar; got != 0 {
		t.Errorf("看不见的窗口在一秒里建了 %d 遍界面，want 0——"+
			"「没画帧」和「画了帧但没建界面」在画面上是一回事：用户切回来看到空的",
			got)
	}
}

// invalidateOutside 模拟「别的路径直接叫 `Services.Invalidate`」——绕开
// `wakeHandle` 那道「看不看得见」的闸门（见 wake.go 的规则三）。
//
// 它走的是**框架**的入口（`mygo/ui/services.go:24`），所以这一帧是「有人要来的
// 帧」，而不是 Tester 自己拉的一帧。
func invalidateOutside(t *testing.T, s *Shell) {
	t.Helper()
	s.wake.mu.Lock()
	svc, held := s.wake.svc, s.wake.held
	s.wake.mu.Unlock()
	if !held {
		t.Fatal("窗口还没画过一帧，拿不到 Services")
	}
	svc.Invalidate()
}
