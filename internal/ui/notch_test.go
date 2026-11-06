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

// TestNotchByeHideStopsPollingEntirely 钉「引导收掉后那一档藏窗，轮询是
// 整摊停的」：不光 ticker 停了，`pollAt`/`pollEvery` 也得清——不然
// `viewNotch` 会拿一条死了的 ticker 留下的欠账当到点，白跑一次
// `notchPoll`（曾经这里只 `stopNotchTicker`，和其它停法不一致）。
//
// 这一档 `win` 本来是非空才进的；但「停的完整不完整」跟窗在不在无关，
// 无头外壳（`win == nil`）同样该清干净。
func TestNotchByeHideStopsPollingEntirely(t *testing.T) {
	n, _ := newNotchShell(t)
	fc := newFakeClock(time.Unix(1767225600, 0))
	tc := &notchPollTestClock{fakeClock: fc}
	n.clock = tc

	// 引导结束：hello 已收、开关还关着，正排着一条 ticker（悬停那一档
	// 常见：引导里 `notchPoll` 排了 33ms 或 100ms 的 ticker）。
	n.notchSt.active = true
	n.notchPollAgain(true)
	if got := tc.activeTimers(); got != 1 {
		t.Fatalf("收窗前还排着 %d 条 ticker，want 1", got)
	}

	n.notchHideAfterBye()
	if got := tc.activeTimers(); got != 0 {
		t.Fatalf("收窗后还有 %d 条 ticker，want 0", got)
	}
	if !n.notchSt.pollAt.IsZero() || n.notchSt.pollEvery != 0 {
		t.Fatalf("收窗后轮询时刻没清：pollAt=%v pollEvery=%v",
			n.notchSt.pollAt, n.notchSt.pollEvery)
	}
	// 推钟到原来的下一跳之后：那条欠账不该再被认成到点。
	fc.Advance(time.Second)
	if n.notchPollTick() {
		t.Fatal("整摊停了之后还被叫醒：pollAt 没清，死了的 ticker 留了欠账")
	}
}

// TestNotchByeHideSkippedWhenGreetingRevives 钉延迟那一档的反例：跑它的
// 时候引导复活了（指针又进来重新问候）或开关已经开了，就不许藏窗、不许
// 停轮询。
func TestNotchByeHideSkippedWhenGreetingRevives(t *testing.T) {
	n, _ := newNotchShell(t)
	tc := &notchPollTestClock{fakeClock: newFakeClock(time.Unix(1767225600, 0))}
	n.clock = tc
	n.notchSt.active = true
	n.notchPollAgain(true)

	// 那一秒钟里用户又把指针放回刘海，重新问候了：藏窗取消。
	n.notchSt.hello = true
	n.notchHideAfterBye()
	if got := tc.activeTimers(); got != 1 {
		t.Fatalf("引导复活后 ticker 被停了：active=%d, want 1", got)
	}

	// 引导没落回来，但用户在那一秒里把开关拨到开了：也不许藏。
	n.notchSt.hello = false
	if _, err := n.app.UpdateSettings(func(set *app.Settings) {
		set.NotchPlayer = boolPtr(true)
	}); err != nil {
		t.Fatal(err)
	}
	n.notchHideAfterBye()
	if got := tc.activeTimers(); got != 1 {
		t.Fatalf("开关开着时 ticker 被停了：active=%d, want 1", got)
	}
}

// TestNotchWindowCloseProceedsWhenTheAppIsQuitting 钉「应用退出时刘海窗不许
// 再把关窗拦回去」那条。
//
// 根因是「quitting 记在哪一份外壳上」：`installLifecycle` 的 `OnBeforeQuit`
// 只把**主窗口**那个外壳的 `quitting` 置上，而框架的 `prepareQuit` 紧接着会
// 遍历**所有**窗口挨个 close——刘海窗也在其中。它自己的 `quitting` 那一刻
// 还是 false，于是 OnClose 照 `PreventDefault`，把整次退出拦掉：⌘Q / 菜单
// 退出 / SIGTERM 触发的 MyGo quit 全都退不掉，只能 SIGKILL。
//
// 这里按真实顺序重演：`OnBeforeQuit` 置的是主壳那一份，随后由刘海这侧的
// 关窗判据去答「该放行吗」。
func TestNotchWindowCloseProceedsWhenTheAppIsQuitting(t *testing.T) {
	n, main := newNotchShell(t)

	// 用户按 ⌘Q / 菜单退出 / SIGTERM：框架先跑 OnBeforeQuit（置主壳），
	// 再挨个关窗口。
	main.quitting = true
	if !n.notchCloseProceeds() {
		t.Fatal("应用已在退出流程，刘海窗还把关窗拦回去：prepareQuit 会被它取消")
	}

	// 反面：不在退出流程时刘海窗照拦（⌘W / Esc 不许真的关掉它，理由见
	// attachNotchWindow 里 OnClose 那段）。
	main.quitting = false
	if n.notchCloseProceeds() {
		t.Fatal("没在退出流程时刘海窗放行关窗：⌘W 会把它真的关掉，再也收不到指针事件")
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
