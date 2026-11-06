package ui

// 刘海窗的开合状态机与窗口开关。真窗那半边（NSWindow 配置、NSEvent 监听）
// 只有真机能验，这里钉的是**不靠窗口也成立**的那一半：
//
//   - `openNotchPlayer`/`closeNotchPlayer` 对外壳的开/关；
//   - `notchPoll` 那个「指针在不在触发区」的状态机（300ms 进、350ms 出、
//     「在区里挪动会重算」）；
//   - 引导那一档（`notchGreet`/`notchKeep`/`notchBye`）；
//   - 穿透：指针不在形状上时窗把点击让给桌面。
//
// ⚠️ 「刘海的 `notchSt` 长在外壳自己身上」是这一组的地基：监听那边写、
// `notchPoll` 这边读（`notch.go` 那一段注释）。

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// newNotchShell 建一个刘海外壳 + 它的主窗外壳，不建真窗（无头 Tester 里
// `mygo.NewWindow` 会 panic——和 `newMiniShell` 同一个理由）。
func newNotchShell(t *testing.T) (*Shell, *Shell) {
	t.Helper()
	sh := newTestShell(t, withSettings(app.Settings{NotchPlayer: boolPtr(false)}))
	main := sh.s
	n := main.newNotchShell()
	t.Cleanup(n.closePerWindow)
	// 假窗尺寸填上：`notchMeasure` 要走 `mygo.Screen.PrimaryDisplay`，
	// 无头 Tester 里那个返回零值——给一份真的，好让 screenX/screenY 是数。
	n.notchSt.winW, n.notchSt.winH = notchWinW, notchWinH
	n.notchSt.w0, n.notchSt.h0 = 185, 32
	n.notchSt.real = true
	return n, main
}

// cursorIn 把刘海的指针坐标写成「在触发区里的一个点」。
// 真窗那一段是 `mygo.Screen.CursorScreenPoint`，无头 Tester 里它返回零值
// ——所以这里不读它，直接把 `inside` 算好塞进状态机（`poll` 的量的是
// `inside` 之后的进出时间）。
func notchEnter(n *Shell, at time.Time) {
	n.notchSt.lastInside = at
	n.notchSt.enteredAt = at
}

func TestNotchPlayerOpensAndClosesTheShell(t *testing.T) {
	main := newTestShell(t).s
	if main.notch != nil {
		t.Fatal("刚开始刘海不该开着")
	}
	n := main.newNotchShell()
	if main.notch != n || n.main != main {
		t.Fatal("刘海和主窗口没接上")
	}
	if !main.dropNotch() {
		t.Fatal("dropNotch 说没有刘海")
	}
	if main.notch != nil {
		t.Fatal("关掉之后主窗口还指着那个外壳")
	}
	if main.dropNotch() {
		t.Fatal("第二个 dropNotch 不该说开着")
	}
}

func TestToggleNotchPlayerFromTheNotchSide(t *testing.T) {
	main := newTestShell(t, withSettings(app.Settings{NotchPlayer: boolPtr(true)})).s
	n := main.newNotchShell()
	t.Cleanup(n.closePerWindow)
	n.toggleNotchPlayer()
	if main.notch == nil {
		t.Fatal("切换刘海播放器不该销毁刘海外壳")
	}
	if main.notchEnabled() {
		t.Fatal("从刘海那侧切换后设置还开着")
	}
	main.toggleNotchPlayer()
	if main.notch == nil {
		t.Fatal("主窗口那边没把刘海开回来")
	}
	if !main.notchEnabled() {
		t.Fatal("主窗口那边切换后设置没打开")
	}
}

func TestNotchGreetOnlyOnce(t *testing.T) {
	n, _ := newNotchShell(t)
	if err := enqueueTwo(n.app, t); err != nil {
		t.Fatal(err)
	}
	// 第一次打开引导：问候一次，记下「问候过了」。
	n.notchGreet()
	if !n.notchSt.hello {
		t.Fatal("第一首歌没问候")
	}
	if !n.app.SettingsSnapshot().NotchPlayerGreeted {
		t.Fatal("问候了没记下")
	}
	// 同一首再问候：不再长出来。
	n.notchSt.hello = false
	n.notchGreet()
	if n.notchSt.hello {
		t.Fatal("问候过又长出来")
	}
}

// TestNotchTarget 钉「展开 / 两翼 / 收合」三种尺寸各是多大。
func TestNotchTarget(t *testing.T) {
	n, _ := newNotchShell(t)
	// 没歌 + 展开：idle 那张卡。
	n.notchSt.open = true
	w, h := n.notchTarget()
	if w != notchIdleW || h != n.notchSt.h0+72 {
		t.Fatalf("无歌展开是 %v×%v，want %v×%v", w, h, notchIdleW, n.notchSt.h0+72)
	}
	// 没歌 + 收着：刘海本身。
	n.notchSt.open = false
	w, h = n.notchTarget()
	if w != n.notchSt.w0 || h != n.notchSt.h0 {
		t.Fatalf("没歌时收着是 %v×%v，want %v×%v", w, h, n.notchSt.w0, n.notchSt.h0)
	}
}

// TestNotchKeepTurnsItOn 钉「引导上那个开启键把设置拨到开」。
func TestNotchKeepTurnsItOn(t *testing.T) {
	n, _ := newNotchShell(t)
	n.notchSt.hello = true
	n.notchKeep()
	if n.notchSt.hello {
		t.Fatal("keep 之后还在引导")
	}
	if !n.app.SettingsSnapshot().NotchPlayerOn() {
		t.Fatal("keep 没把开关拨到开")
	}
}

func TestNotchGreetingWaitsForPlayback(t *testing.T) {
	n, _ := newNotchShell(t)
	n.notchGreet()
	if n.notchSt.hello {
		t.Fatal("没有歌曲时不该显示刘海引导")
	}
	if n.app.SettingsSnapshot().NotchPlayerGreeted {
		t.Fatal("没有歌曲时不该记为已经问候")
	}
	if err := enqueueTwo(n.app, t); err != nil {
		t.Fatal(err)
	}
	n.notchGreet()
	if !n.notchSt.hello {
		t.Fatal("开始播放后没有显示刘海引导")
	}
	w, h := n.notchTarget()
	if w != notchHelloW || h != n.notchSt.h0+58 {
		t.Fatalf("播放后的引导尺寸是 %v×%v，want %v×%v", w, h, notchHelloW, n.notchSt.h0+58)
	}
}

func TestNotchPollingKeepsOneScheduledTimer(t *testing.T) {
	n, _ := newNotchShell(t)
	fc := newFakeClock(time.Unix(1767225600, 0))
	tc := &notchPollTestClock{fakeClock: fc}
	n.clock = tc
	n.notchSt.active = true

	// 指针在触发区里：33ms 那一档，装一条 ticker。
	n.notchPollAgain(true)
	if got := tc.activeTimers(); got != 1 {
		t.Fatalf("首次轮询排了 %d 条 ticker，want 1", got)
	}

	// 指针还在：不是「再起一条」，是同一条在跳——重排不许扇出。
	n.notchPollAgain(true)
	if got := tc.activeTimers(); got != 1 {
		t.Fatalf("悬停中重排后有 %d 条 ticker，want 1", got)
	}

	// ⚠️ 指针每动一下都进 `notchPollAgain`：同间隔时不许重置周期，不然
	// 「指针离开 350ms」永远等不到——1.0.1 里 `polling` 闸顺带扛住了这条。
	// 连着三次「指针还在」后推 33ms，该到点还是得到点。
	n.notchPollAgain(true)
	n.notchPollAgain(true)
	n.notchPollAgain(true)

	// 到点叫醒的是界面不是回调自己：推钟，tick 那一帧应该看见欠着一跳。
	fc.Advance(33 * time.Millisecond)
	if got := tc.activeTimers(); got != 1 {
		t.Fatalf("ticker 到点唤醒后有 %d 条 ticker，want 1", got)
	}
	if !n.notchPollTick() {
		t.Fatal("推了 33ms 之后这一帧没认出 ticker 到点")
	}

	// 场景变掉重排还是一条：失焦以后切 100ms 那档，旧的停新的起。
	n.notchSt.active = false
	n.notchPollAgain(false)
	if got := tc.activeTimers(); got != 1 {
		t.Fatalf("失焦重排后有 %d 条 ticker，want 1（旧的该停掉）", got)
	}

	// 设置关上、引导也收了：ticker 必须停，goroutine 不能挂着。
	if _, err := n.app.UpdateSettings(func(set *app.Settings) {
		set.NotchPlayer = boolPtr(false)
	}); err != nil {
		t.Fatal(err)
	}
	n.notchSt.hello = false
	n.stopNotchPolling()
	if got := tc.activeTimers(); got != 0 {
		t.Fatalf("停止轮询后还有 %d 条 ticker，want 0", got)
	}

	// 推钟也不该再有醒：旧 ticker 的回调再叫也要被无视。
	fc.Advance(time.Second)
	if n.notchPollTick() {
		t.Fatal("停掉之后还被叫醒（旧 ticker 还在跳）")
	}
}

// notchPollTestClock 是真时钟行为的假钟：EveryFunc 走 fakeClock 那一套
// （Advance 到点**同步跑**回调），多记一份「现在还活着几条 ticker」。
type notchPollTestClock struct {
	*fakeClock
	active int
}

func (c *notchPollTestClock) EveryFunc(d time.Duration, f func()) func() {
	stop := c.fakeClock.EveryFunc(d, f)
	c.active++
	stopped := false
	return func() {
		if !stopped {
			stopped = true
			c.active--
		}
		stop()
	}
}

func (c *notchPollTestClock) activeTimers() int { return c.active }
