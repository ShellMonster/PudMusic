// macOS 刘海窗：住在屏幕刘海那一块的一个迷你播放器。
//
// # 形状
//
// 窗本身**一直都在**（`Panel` + `Transparent` + `raiseAboveMenuBar`：窗级
// 压过菜单栏、每个空间都在、全屏也贴着），收着的时候画的东西是「刘海宽 ×
// 刘海高」那么一小块，展开是「更宽 × 更深」的一张播放卡——窗尺寸不动，
// 动的是画出来的形状。
//
// 「指针进了刘海那一块」不总能靠窗口自己的 hover 拿：指针贴着菜单栏上方
// 那一条走时，它压根没进窗。所以挂一条 NSEvent 全局/本地监听（`notch_darwin.go`
// 的 `watchPointer`），指针一动就 `poll()`；`poll` 再算「在不在触发区」、
// 「窗要不要吃点击」。
//
// ⚠️ **收起不是隐藏窗**：窗一藏就永远收不到指针事件（那个监听是全局的，
// 但「指针在这儿」得有人去算，而窗藏着的时候没人叫它）——所以收着的时候
// 窗还在、画的是刘海形状，点击**穿透**到桌面（`SetIgnoreMouseEvents(true)`，
// 见 `poll` 里那一段）。
//
// # 三态 + 一个引导
//
//   - `notchWings`：刘海的两翼。收起时展开成「刘海宽 + 两侧翅膀」：左翼封面、
//     右翼频谱条——boring.notch 的 live-activity 那一档，一直在、不占桌面。
//   - `notchExpanded`：指针探进触发区停够 300ms 后展开的播放卡。
//   - `notchIdle`：没歌时展开成「未在播放 + 打开应用」。
//   - `notchHello`：第一次开始播放时的引导（「刘海里也能控制播放 + 开启」）。
package ui

import (
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// 刘海窗的窗尺寸（固定，不变）：展开卡宽 468 + 两侧余量，高 240（刘海高 ~38
// + 展开深度 178 + 余量）。窗尺寸由 `pinToTop` 一次钉在屏顶正中，之后
// 不再动——动的是画出来的形状。
const (
	notchWinW = notchOpenW + 60
	notchWinH = 240

	notchOpenW  = 468 // 展开卡的宽（参考 mygo-cloudmusic 的刘海播放器）
	notchOpenH  = 178 // 展开比刘海多出来的深度
	notchIdleW  = 330 // 无歌时那张卡的宽
	notchHelloW = 340 // 引导那条的宽

	notchWingCover = 20         // 两翼封面边长（原型 `.notch-min img`）
	notchWingPad   = space.Px12 // 两翼离边的内边距
	notchCardCover = 56         // 展开卡封面（原型 `.notch-card img`）

	notchHoverOpenDelay = 300 * time.Millisecond // 指针停够多久才展开
	notchHoverExitDelay = 350 * time.Millisecond // 走开多久才收起
	notchSlack          = 6                      // 触发区比形状宽的余量（points）
)

// notchState 是刘海窗的开合状态机。
type notchState int

const (
	notchClosed notchState = iota // 收着：刘海底下那一小块
	notchOpen                     // 展开：播放卡铺出来
)

// notchShell 是刘海窗自己的状态（挂在 `Shell` 上，见 shell.go 那一段）。
//
// ⚠️ **它长在每一个外壳上**：主窗和刘海外壳各有一份，但只有刘海外壳那一份
// 被真用（`watchPointer`/`poll` 读写的都是它）。
type notchShell struct {
	state notchState

	// open 是「指针现在在它上面」（比 state 更底层：进出事件先到这里，
	// `poll` 再决定开不开）。
	open bool

	// hello 是「正在引导」（第一次开始播放时从刘海里长出来那句提示）。
	hello bool

	// passing 是「窗此刻吃点击吗」——不吃时点击穿透到桌面。
	passing bool

	// enteredAt / lastInside 是算「停够 300ms 没有」「走开 350ms 没有」
	// 的两个时刻。
	enteredAt  time.Time
	lastInside time.Time

	// watched / unwatch 是指针监听那一对。
	watched bool
	unwatch func()

	// pollAt / pollEvery 是界面上排着的下一次 poll：pollAt 是后台那条
	// ticker 下一跳到点的时刻，pollEvery 是它的间隔。两格都只在界面线程
	// 读写——后台回调按 clock.go 的规矩只调 `wake.invalidate()`，「到没
	// 到点」由 `notchPollTick` 拿时钟现算。
	pollAt    time.Time
	pollEvery time.Duration

	// active 是「应用是前台吗」：应用失焦时刘海收进屏顶——那时指针在它
	// 上面的事没人看见，轮询可以放缓。
	active bool

	// w0, h0 是刘海的宽和高（points）。real 是这块屏真有一个刘海。
	w0, h0 float32
	real   bool

	// screenX, screenY 是窗在屏上的左上角（屏幕坐标）。
	screenX, screenY float32

	// winW, winH 是窗的 DIP 尺寸（= notchWinW/notchWinH，留在这是给测试替）。
	winW, winH float32

	// playing 是上一帧看到的「在放没有」——拿它找「开始放了」那个边沿。
	playing bool
}

// NewNotch 建刘海窗那个外壳。**状态和主窗口、迷你窗共用同一个 App**。
//
// ⚠️ **它归「收不掉的第三扇窗」那一档**：`OnClosed` 走 `releaseNotch`（不收
// 应用、不停播放——理由同 `attachMiniWindow` 顶上那一段），而**不是**
// `Shell.Close`（那一个会 `s.app.Close()` 把正在放的歌停掉）。
func NewNotch(a *app.App, main *Shell) *Shell {
	s := newShellWithImages(a, "")
	s.presence.update(func(p *windowPresence) { p.freeWhileBlurred = true })
	if main != nil {
		s.adoptShared(main)
	}
	s.draw = s.guardAt("刘海窗", s.viewNotch)
	return s
}

// NotchWindowOptions 是刘海窗那一扇的样子。
//
// 和 `MiniWindowOptions` 一路：`Transparent`（自己画底）、`Panel`（点不激活）、
// `Frameless`。**没有 `StateKey`**（位置永远钉在屏顶正中）、**没有
// `MinWidth/MinHeight`**（尺寸由代码钉死）。
func NotchWindowOptions(view func(*ui.Context)) mygo.WindowOptions {
	return mygo.WindowOptions{
		Title:             "布丁音乐刘海",
		Width:             notchWinW,
		Height:            notchWinH,
		Transparent:       true,
		AlwaysOnTop:       true,
		SkipTaskbar:       true,
		Panel:             true,
		Frameless:         true,
		DisableResize:     true,
		DisableMinimize:   true,
		DisableMaximize:   true,
		DisableFullScreen: true,
		DisableMove:       true,
		DisableShadow:     true,
		Hidden:            true, // 先不显示：setEnabled 那一步再 ShowInactive
		Content:           ui.View(view),
	}
}

// openNotchPlayer 开出刘海窗。
//
// ⚠️ **它不挡主窗/迷你窗**：刘海是第三扇窗，开着它不该把用户手上的那扇收走。
func (s *Shell) openNotchPlayer() {
	if s.notch == nil {
		s.newNotchShell()
		s.ensureNotchWindow()
		return
	}
	if w := s.notch.win; w != nil && !w.IsDestroyed() {
		w.ShowInactive()
	}
}

// newNotchShell 建出刘海那个外壳并和主窗口接上（`s.notch` 指过去、反向指回来）。
// **不建窗口**——建窗口要事件循环，那一半在 `ensureNotchWindow`。
func (s *Shell) newNotchShell() *Shell {
	if s.notch != nil {
		return s.notch
	}
	n := NewNotch(s.app, s)
	n.main = s
	s.notch = n
	n.bindNotchWindow()
	return n
}

// closeNotchPlayer 关掉刘海窗。
func (s *Shell) closeNotchPlayer() {
	s.dropNotch()
}

// dropNotch 关掉刘海窗，返回「刚才确实开着」。
func (s *Shell) dropNotch() bool {
	n := s.notch
	if n == nil {
		return false
	}
	s.notch = nil
	n.main = nil
	if w := n.win; w != nil && !w.IsDestroyed() {
		w.Close()
	}
	return true
}

// ensureNotchWindow 真的把刘海窗开出来。**只能在应用 ready 之后调**。
func (s *Shell) ensureNotchWindow() {
	n := s.notch
	mygo.App.WhenReady(func() {
		if n == nil || s.notch != n || n.win != nil {
			return
		}
		// 先量刘海：决定「真有一个刘海还是只有菜单栏」和两翼的尺寸。
		n.notchMeasure()
		opts := NotchWindowOptions(n.View)
		w := mygo.NewWindow(opts)
		n.attachNotchWindow(w)
		// 🔴 macOS 原生半层：抬到菜单栏之上、贴屏顶居中。
		notchConfigure(w)
		// 设置开着就把它亮出来；关着就藏着（窗在、看不见）。
		n.notchSetEnabled(n.notchEnabled())
		n.watchNotch()
		n.notchPoll()
		// 如果应用是带着一首正在播放的歌启动，按 cloud 的时机补一次引导；
		// 普通启动不主动打扰用户，等真正开始播放时由主窗口状态边沿触发。
		if s.app.Snapshot().Playing {
			n.notchGreet()
		}
	})
}

// bindNotchWindow 登记这一扇窗的唤醒句柄（铁律三：逐窗口挨个叫醒）。
func (s *Shell) bindNotchWindow() {
	if s.wakeRemove != nil {
		return
	}
	s.wakeRemove = s.app.AddWake(s.wake.invalidate)
}

// attachNotchWindow 把刘海窗接到它的外壳上。
//
// ⚠️ **和 `Attach`/`attachMiniWindow` 一样不复用**：主窗那套 `OnClosed` 会
// 停播放，迷你那套会叫回主窗——刘海窗的关只收它自己那一摊。
func (s *Shell) attachNotchWindow(w *mygo.Window) {
	s.win = w
	s.bindNotchWindow()
	s.watchWindow(w)
	// 刘海窗不让 ⌘W / Esc 真的关掉：关了就收不到指针事件了。想关掉走
	// 菜单「显示/隐藏刘海」或设置开关。
	w.OnClose(func(e *mygo.CloseEvent) {
		if !s.notchCloseProceeds() {
			e.PreventDefault()
		}
	})
	w.OnClosed(func() {
		if s.win == w {
			s.win = nil
		}
		s.unwatchNotch()
		s.releaseNotch()
		if s.main != nil && s.main.notch == s {
			s.main.notch = nil
		}
	})
	// 显示器变了（插拔 / 旋转）：重新量刘海、重新钉位置。
	mygo.Screen.OnDisplaysChanged(func() {
		s.notchMeasure()
		notchReposition(s.win)
	})
	// 应用前台/后台：失焦时刘海收进屏顶，指针监听可以放缓。
	s.notchSt.active = mygo.App.IsActive()
	mygo.App.OnDidBecomeActive(func() { s.notchSt.active = true; s.notchPoll() })
	mygo.App.OnDidResignActive(func() { s.notchSt.active = false })
}

// notchCloseProceeds 报「这一刻刘海窗的关该放行吗」。
//
// ⚠️ **quitting 记的是主窗口那一份**（`installLifecycle` 的 `OnBeforeQuit`
// 装在主壳上），而退出流程本身要靠框架挨个关窗口走完——刘海窗也在那一列里。
// 拿自己那份 `quitting`（永远是 false）去拦，⌘Q / 菜单退出 / SIGTERM 触发的
// quit 全都会被它拦掉：窗口关不掉，进程退不出，只能 SIGKILL（同 `Attach`
// 里 `planWindowClose` 那条「退出途中不能让设置拦路」的规矩）。
func (s *Shell) notchCloseProceeds() bool {
	if s.main != nil {
		return s.main.quitting
	}
	return s.quitting
}

// releaseNotch 收掉刘海窗自己占着的（逐窗口那一摊），不收应用。
func boolPtr(b bool) *bool { return &b }

func (s *Shell) releaseNotch() {
	s.stopNotchPolling()
	s.closePerWindow()
}

// ---- 设置 ----

// notchEnabled 报「刘海窗」那一格是开是关。
func (s *Shell) notchEnabled() bool {
	return s.app.SettingsSnapshot().NotchPlayerOn()
}

// notchSetEnabled 把刘海窗亮出来或者藏起来。
func (s *Shell) notchSetEnabled(on bool) {
	if s.win == nil {
		return // 测试里没有真窗
	}
	if on == s.win.IsVisible() {
		if !on {
			s.stopNotchPolling()
		}
		s.wake.invalidate()
		s.watchNotch()
		return
	}
	if on {
		s.win.ShowInactive()
		notchReposition(s.win)
		s.watchNotch()
		s.notchPoll()
		return
	}
	s.notchSt.open, s.notchSt.hello = false, false
	s.stopNotchPolling()
	s.win.Hide()
	s.watchNotch()
}

// syncNotchPlayer 把设置里的开关同步到刘海窗口。
//
// 刘海外壳在 macOS 启动时就会创建；设置页和菜单只改设置，实际的
// ShowInactive / Hide 统一从这里落到窗口，避免入口之间各做一半。
func (s *Shell) syncNotchPlayer() {
	if !notchSupported() {
		return
	}
	if s.notch == nil {
		// 生产启动会先建刘海外壳；无头测试和 ready 之前的设置帧
		// 没有可同步的窗口，留给启动 / 菜单入口创建即可。
		return
	}
	s.notch.notchSetEnabled(s.notchEnabled())
}

// watchNotch 按「刘海亮着没有」挂上/摘掉指针监听——刘海关着的时候，
// 指针动一下就把应用叫醒一次是白花钱。
func (s *Shell) watchNotch() {
	on := s.notchEnabled() || s.notchSt.hello
	switch {
	case on && s.notchSt.unwatch == nil:
		notchStartWatch(s)
	case !on && s.notchSt.unwatch != nil:
		s.unwatchNotch()
	}
}

func (s *Shell) unwatchNotch() { notchStopWatch(s) }

// notchMeasure 量菜单栏那块屏的刘海：宽、高、有没有真刘海。
func (s *Shell) notchMeasure() {
	n := &s.notchSt
	n.w0, n.h0, n.real = 200, 32, false
	if w, h, ok := notchSize(); ok {
		n.w0, n.h0, n.real = float32(w), float32(h), true
	} else if h := menuBarHeight(); h > 0 {
		// 没有刘海的屏上，刘海那么高就当「贴着菜单栏」。
		n.h0 = float32(h)
	}
	if n.winW == 0 {
		n.winW, n.winH = notchWinW, notchWinH
	}
	// 屏幕坐标下窗的左上角。
	d := mygo.Screen.PrimaryDisplay()
	n.screenX = float32(d.Bounds.X) + (float32(d.Bounds.Width)-n.winW)/2
	n.screenY = float32(d.Bounds.Y)
	if s.win != nil {
		s.wake.invalidate()
	}
}

// ---- 目标尺寸 ----

// notchTarget 报此刻刘海的形状该是多大（宽、高，points）。
func (s *Shell) notchTarget() (w, h float32) {
	n := &s.notchSt
	st := s.app.Snapshot()
	switch {
	case n.hello:
		return notchHelloW, n.h0 + 58
	case n.open && !st.HasTrack:
		return notchIdleW, n.h0 + 72
	case n.open:
		return notchOpenW, n.h0 + notchOpenH
	case !n.real && !s.app.SettingsSnapshot().NotchVirtualOn() &&
		!n.open && !n.hello:
		return 0, 0
	case st.HasTrack && s.notchEnabled() &&
		s.app.SettingsSnapshot().NotchActivityOn() && st.Playing:
		// 两翼：刘海宽 + 两边各一只翅膀（封面 + 频谱），不挡菜单栏文字。
		return n.w0 + 2*(notchWingPad+notchWingCover), n.h0
	default:
		return n.w0, n.h0
	}
}

// ---- 指针轮询 ----

// notchPoll 是「指针现在在不在刘海触发区」那一次判断。
//
// 它是**事件驱动**的：`watchPointer` 报一次动它就跑一遍，而`pollAgain`
// 在「悬停中/展开中/应用活跃但监听没挂上」时再排一次——指针不动的时候
// 这条 chain 自己就停了，不一直轮询白花钱。
func (s *Shell) notchPoll() {
	n := &s.notchSt
	if !s.notchEnabled() && !n.hello {
		s.stopNotchPolling()
		return
	}
	pt := mygo.Screen.CursorScreenPoint()
	w, h := s.notchTarget()
	x := n.screenX + (n.winW-w)/2
	px, py := float32(pt.X), float32(pt.Y)
	inside := px >= x-notchSlack && px <= x+w+notchSlack &&
		py >= n.screenY-notchSlack && py <= n.screenY+h+notchSlack
	if !n.real && !n.open && !n.hello {
		// 没刘海的屏：指针贴到屏顶正中那一条才算进——不然用户把窗停在
		// 屏幕上半段也被算成「在刘海上」。
		cx := n.screenX + n.winW/2
		inside = px >= cx-n.w0/2 && px <= cx+n.w0/2 && py <= n.screenY+1
	}
	now := s.clock.Now()
	changed := false
	if inside {
		if n.lastInside.IsZero() || now.Sub(n.lastInside) > 100*time.Millisecond {
			n.enteredAt = now
		}
		n.lastInside = now
		if !n.open && !n.hello && now.Sub(n.enteredAt) >= notchHoverOpenDelay {
			n.open, changed = true, true
		}
	} else if n.open && now.Sub(n.lastInside) > notchHoverExitDelay {
		n.open, changed = false, true
	}

	// 点击穿透：指针不在那块形状上的时候，窗把点击让给桌面。
	pass := !inside
	if pass != n.passing {
		n.passing = pass
		if s.win != nil {
			s.win.SetIgnoreMouseEvents(pass)
		}
	}
	if changed {
		s.wake.invalidate()
	}
	s.notchPollAgain(inside)
}

// notchPollAgain 再排一次 poll：指针在里面或开着的时候 33ms 一跳
// （跟上动画）；应用活跃或监听没挂上的时候 100ms；不然下一次指针动
// 会自己再进来。
//
// ⚠️ **整个刘海只活着一条 ticker**。1.0.1 的泄漏是回调里又 `EveryFunc`
// 起新 ticker：旧的不停、新的又起，几条之后应用就被后台唤醒拖垮。这里
// 反过来：重排先 `stopNotchPolling` 把旧 ticker 停掉，再装新的；后台回调
// 只递 `wake.invalidate`，什么都不写——「是不是又该 poll 了」由
// `notchPollTick` 在界面线程拿 `pollAt` 现算，所以「停掉」只是把
// `pollAt` 清掉并 `stop()`，那一刻之后旧 ticker 再叫也画不出 poll。
func (s *Shell) notchPollAgain(inside bool) {
	n := &s.notchSt
	var d time.Duration
	switch {
	case inside || n.open:
		d = 33 * time.Millisecond
	case n.active || !n.watched:
		d = 100 * time.Millisecond
	}
	if d == 0 {
		s.stopNotchPolling()
		return
	}
	if n.pollEvery == d && s.notchPollingActive() {
		// 间隔没变：**对齐相位不重排**。1.0.1 里 `polling` 闸顺带扛住了
		// 「指针每动一下就把下一跳往后推 d」这个 bug——同间隔时别动
		// `pollAt`（ticker 自己每 d 跳一次，到点由 `notchPollTick` 把
		// `pollAt` 拨到下一跳）。
		return
	}
	s.stopNotchPolling()
	n.pollEvery = d
	n.pollAt = s.clock.Now().Add(d)
	stop := s.clock.EveryFunc(d, func() {
		s.wake.invalidate()
	})
	s.setNotchPollStop(stop)
}

// stopNotchPolling 把排着的那条 ticker 停掉。「不排了」的那一帧
// （`notchPollAgain` 里 `d == 0`）、刘海藏起来（`notchSetEnabled(false)`）、
// 引导收完藏窗（`notchHideAfterBye`）、窗口关了（`releaseNotch`）都走这里。
// 可重复调。
func (s *Shell) stopNotchPolling() {
	s.stopNotchTicker()
	n := &s.notchSt
	n.pollAt, n.pollEvery = time.Time{}, 0
}

func (s *Shell) stopNotchTicker() {
	s.notchPollMu.Lock()
	stop := s.notchPollStop
	s.notchPollStop = nil
	s.notchPollMu.Unlock()
	if stop != nil {
		stop()
	}
}

func (s *Shell) setNotchPollStop(stop func()) {
	s.notchPollMu.Lock()
	s.notchPollStop = stop
	s.notchPollMu.Unlock()
}

func (s *Shell) notchPollingActive() bool {
	s.notchPollMu.Lock()
	defer s.notchPollMu.Unlock()
	return s.notchPollStop != nil
}

// notchPollTick 是「后台那条 ticker 刚叫过醒、界面这一帧是不是该再 poll
// 一次」的判据。它把 `clock.go` 顶上那条规矩落到刘海上：定时器回调不许写
// Shell 字段，所以「到没到点」不归回调管，归这一帧现算。
//
// 返回 true 时调用方（`viewNotch`）**先**把 `pollAt` 挪到下一跳再跑
// `notchPoll`——`notchPoll` 自己尾部还会调 `notchPollAgain` 重排，先挪走
// 「已过期」的旧时刻，重排才看得见「下一条还没排」。
func (s *Shell) notchPollTick() bool {
	n := &s.notchSt
	if n.pollAt.IsZero() {
		return false
	}
	now := s.clock.Now()
	if now.Before(n.pollAt) {
		return false
	}
	n.pollAt = now.Add(n.pollEvery)
	return true
}

// ---- 引导（第一次开始播放）----

// observeNotchPlayback 观察播放状态的上升沿，给关闭状态的刘海播放器一次引导。
func (s *Shell) observeNotchPlayback(st app.State) {
	if s.notch == nil {
		return
	}
	n := &s.notch.notchSt
	if st.Playing && !n.playing && !s.notch.notchEnabled() {
		// ready 回调可能还没把真窗接上：留给 ensureNotchWindow 在窗口
		// 建好后用当前播放状态补一次引导。
		if s.notch.win != nil {
			s.notch.notchGreet()
		}
	}
	n.playing = st.Playing
}

// notchGreet 是「第一次开始播放时，从刘海里长出来那句提示」。
//
// `NotchPlayerGreeted` 记过一次后不再重复出现。
func (s *Shell) notchGreet() {
	n := &s.notchSt
	st := s.app.Snapshot()
	if !st.HasTrack || !st.Playing || s.notchEnabled() ||
		s.app.SettingsSnapshot().NotchPlayerGreeted {
		return
	}
	s.app.UpdateSettings(func(set *app.Settings) { set.NotchPlayerGreeted = true })
	n.hello = true
	if s.win == nil {
		return
	}
	s.win.ShowInactive()
	notchReposition(s.win)
	s.watchNotch()
	s.notchPoll()
	// 7 秒里指针没在上面就自己收回去。
	time.AfterFunc(7*time.Second, func() {
		if !n.hello {
			return
		}
		if n.open || !n.passing {
			// 指针在它上面：再等 2 秒（别在用户正看着的时候收走）。
			time.AfterFunc(2*time.Second, func() { s.notchBye() })
			return
		}
		s.notchBye()
	})
}

// notchBye 结束引导：刘海收回屏顶，关着的话整个藏掉。
func (s *Shell) notchBye() {
	n := &s.notchSt
	n.hello = false
	s.wake.invalidate()
	if s.notchEnabled() {
		return
	}
	n.open = false
	n.passing = true
	if s.win != nil {
		s.win.SetIgnoreMouseEvents(true)
	}
	// 一秒钟之后再 Hide：让「收进去」那一下先画完。
	time.AfterFunc(time.Second, s.notchHideAfterBye)
}

// notchHideAfterBye 是引导收掉一秒钟之后那一档：引导没复活、开关也还
// 关着，就把轮询整摊停掉、窗藏掉、指针监听摘掉。
//
// ⚠️ 轮询的停要和 `releaseNotch`/`notchSetEnabled(false)` 那两档一样是
// 整套 `stopNotchPolling`——ticker 停掉之外 `pollAt`/`pollEvery` 也要清。
// 只 `stopNotchTicker` 的话那两格还留着「下一跳」的时刻，`viewNotch` 会
// 拿一条已经死了的欠账当到点，白跑一次 `notchPoll`。
func (s *Shell) notchHideAfterBye() {
	n := &s.notchSt
	if n.hello || s.notchEnabled() {
		return
	}
	s.stopNotchPolling()
	if s.win != nil {
		s.win.Hide()
	}
	s.watchNotch()
}

// notchKeep 是引导上那个「开启」键：把开关拨到开。
func (s *Shell) notchKeep() {
	s.notchSt.hello = false
	s.app.UpdateSettings(func(set *app.Settings) { set.NotchPlayer = boolPtr(true) })
	s.notchSetEnabled(true)
}

// toggleNotchPlayer 开 / 关刘海窗。
func (s *Shell) toggleNotchPlayer() {
	if s.main != nil {
		s.main.toggleNotchPlayer()
		return
	}
	on := !s.notchEnabled()
	if _, err := s.app.UpdateSettings(func(set *app.Settings) {
		set.NotchPlayer = boolPtr(on)
	}); err != nil {
		return
	}
	s.syncNotchPlayer()
	s.refreshMenuChecked(s.app.Snapshot())
}

// ---- 绘制 ----

// viewNotch 是刘海窗那一帧：一个贴着刘海长出来的黑色形状 + 里面的三态内容。
func (s *Shell) viewNotch(c *ui.Context) {
	s.applyFrameTheme(c)
	s.images.BeginFrame(c.Now())
	// 后台那条 ticker 叫醒了这一帧：把欠下的那次 poll 在主线程跑掉。
	// 其余唤醒（指针动、播放状态变）由事件自己调 `notchPoll`，这里不重跑——
	// 每帧都跑会变成「每画一帧读一次屏幕指针」的白花钱。
	if s.notchPollTick() {
		s.notchPoll()
	}
	st := s.app.Snapshot()
	n := &s.notchSt

	c.Root().Background(ui.Transparent)

	tw, th := s.notchTarget()
	spring := ui.Animation{Spring: ui.SpringOptions{Duration: 520 * time.Millisecond, Bounce: 0.28}}
	if !n.open {
		spring.Spring.Bounce = 0.12
	}
	stage := ui.Box(c).Fill()
	w := max(0, stage.AnimateTo("w", tw, spring))
	h := max(0, stage.AnimateTo("h", th, spring))
	opened := float32(0)
	if n.open {
		opened = 1
	}
	open := stage.AnimateTo("open", opened, spring)
	hello := stage.AnimateTo("hello", b2f(n.hello), spring)
	// 收起时底角 16，展开 24（原型 `border-radius: 0 0 16px 16px` → `0 0 24px 24px`）。
	foot := 16 + 8*open
	flare := 6 + 8*open
	cx := n.winW / 2

	stage.Draw(func(p *ui.Painter, r ui.Rect) {
		if w < 1 || h < 1 {
			return
		}
		path := notchShape(r.X+cx, w, h, flare, foot)
		// 原型的刘海没有阴影（`#notch` 只有黑底和圆角），所以这里也不画。
		p.FillPath(path, ui.RGB(0, 0, 0))
	})
	stage.Children(func() {
		body := ui.Box(c).Absolute().Top(0).Left(cx-w/2).Size(w, h).
			Radius(0, 0, foot, foot).Clip().TextColor(ui.RGB(255, 255, 255))
		body.Children(func() {
			if n.hello {
				s.viewNotchHello(c, hello, (w-notchHelloW)/2)
				return
			}
			if !st.HasTrack {
				s.viewNotchIdle(c, open, (w-notchIdleW)/2)
				return
			}
			// 顶上那一行（封面 + eq）收起、展开都在（原型展开态里它仍是第一行）。
			s.viewNotchWings(c, st, 1)
			s.viewNotchHello(c, hello*(1-open), (w-notchHelloW)/2)
			if open > 0.02 {
				s.viewNotchExpanded(c, st, open, (w-notchOpenW)/2)
			}
		})
	})
}

// notchShape 画「刘海变胖」的那个轮廓：上边两角向外张开（flare）接进
// 刘海的圆角，下边是圆角（foot）。纯几何。
func notchShape(cx, w, h, flare, foot float32) *ui.Path {
	const k = 0.5523 // 四分之一圆的贝塞尔控制点
	l, r := cx-w/2, cx+w/2
	p := &ui.Path{}
	p.MoveTo(l-flare, 0)
	p.CubeTo(l-flare+flare*k, 0, l, flare-flare*k, l, flare)
	p.LineTo(l, h-foot)
	p.CubeTo(l, h-foot+foot*k, l+foot-foot*k, h, l+foot, h)
	p.LineTo(r-foot, h)
	p.CubeTo(r-foot+foot*k, h, r, h-foot+foot*k, r, h-foot)
	p.LineTo(r, flare)
	p.CubeTo(r, flare-flare*k, r+flare-flare*k, 0, r+flare, 0)
	p.Close()
	return p
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// 刘海里的颜色：刘海永远是黑的（原型 `#notch { background: #000; color: #ecebe8 }`），
// 不随深浅主题变，字色取全屏播放器那一组固定暗色（`tok.Full`，同一份 #ecebe8 / #a6a8ae）。
//
// 字重 600：原型的歌名是 `<strong>`（700），系统字体里两者同一档；字重用命名常量，
// 理由同 `miniTitleWeight`。
const notchTitleWeight = 600

// viewNotchWings 是刘海的两翼（原型 `.notch-min`）：左翼 20×20 的封面（圆角 4），
// 右翼是三根跳动的 eq 柱；两侧各留 12。**只在收起的时候画**（`shown = 1-open`）：
// 展开卡盖下来的时候两翼藏在它底下，画两遍是白画。
//
// ⚠️ **展开后顶上那一行还在**：原型的展开态里 `.notch-min` 仍然在最上面一行（封面 +
// eq），卡片在它底下——所以展开时这一行也要画，`viewNotch` 传的是 1 而不是 1-open。
func (s *Shell) viewNotchWings(c *ui.Context, st app.State, shown float32) {
	if shown < 0.02 {
		return
	}
	n := &s.notchSt
	ui.Row(c).Key("notch-min").Absolute().Top(0).Left(0).Right(0).Height(n.h0).
		PaddingX(space.Px12).AlignItems(ui.Center).Opacity(shown).
		Children(func() {
			cov := ui.Box(c).Size(notchWingCover, notchWingCover).Radius(tok.R4).Clip().Shrink(0)
			if st.CoverURL == "" {
				cov.Background(ui.RGB(34, 34, 34))
			}
			cov.Children(func() {
				// ⚠️ 半径要明说 4：`ViewTitled` 跟主题圆角（8），在 20 的方块上就成了圆。
				imgcache.ViewRadius(c, s.images, st.CoverURL, notchWingCover, notchWingCover, tok.R4)
			})
			ui.Spacer(c).Grow(1)
			// eq 只在放的时候跳；暂停时是空的（原型 `S.playing ? '<i></i><i></i><i></i>' : ''`）。
			if st.Playing {
				pud.EqBars(c, "notch-eq", true)
			}
		})
}

// viewNotchHello 是引导那条：「刘海里也能控制播放 / 指针停在这里」+「开启」。
func (s *Shell) viewNotchHello(c *ui.Context, shown, left float32) {
	if shown < 0.02 {
		return
	}
	n := &s.notchSt
	row := ui.Row(c).Absolute().Top(n.h0+2).Left(left).Width(notchHelloW).
		Padding(space.Px6, space.Px14, space.Px14, space.Px20).Gap(space.Px12).AlignItems(ui.Center).
		Opacity(max(0, shown*1.4-0.4))
	row.Translate(0, (1-shown)*-8)
	row.Children(func() {
		title, hint := "刘海里也能控制播放", "指针停在这里，即可暂停、切歌"
		if !n.real {
			title, hint = "屏幕顶部也能控制播放", "指针移到顶部中央，即可暂停、切歌"
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(space.Px2).Children(func() {
			ui.Text(c, title).FontSize(scale.Font(c, scale.Size14)).FontWeight(notchTitleWeight).
				TextColor(tok.Full.Text).SingleLine()
			ui.Text(c, hint).FontSize(scale.Font(c, scale.Size12)).TextColor(tok.Full.Text2).SingleLine()
		})
		// 「开启」是主色胶囊（原型 `.btn-primary.btn-sm`：32 高、左右 14、13 号 600）。
		p := tok.Of(c.Theme())
		b := ui.ButtonBase(c.Key("notch-keep")).Height(tok.BtnSmH).PaddingX(space.Px14).
			Radius(tok.BtnSmH / 2).Background(p.Accent).AlignItems(ui.Center).Justify(ui.Center).
			FocusRing(false).Label("开启刘海播放器")
		b.Children(func() {
			ui.Text(c, "开启").FontSize(scale.Font(c, scale.Size13)).FontWeight(notchTitleWeight).
				TextColor(p.AccentInk).SingleLine()
		})
		if b.Clicked() {
			s.notchKeep()
		}
	})
}

// viewNotchIdle 是「没歌」那张卡：「PudMusic / 未在播放」+「打开」。
//
// 原型的空闲态（没歌时）展开只有顶上那一行、没有卡片；这里多一张小卡是为了给「没歌也想
// 回到应用」留一个入口——样式照展开卡的那一套（黑底、#ecebe8 / #a6a8ae、32 高的胶囊键）。
func (s *Shell) viewNotchIdle(c *ui.Context, open, left float32) {
	if open < 0.02 {
		return
	}
	n := &s.notchSt
	p := tok.Of(c.Theme())
	ui.Row(c).Absolute().Top(n.h0+4).Left(left).Width(notchIdleW).
		Padding(space.Px10, space.Px20, space.Px14, space.Px20).Gap(space.Px12).AlignItems(ui.Center).
		Opacity(max(0, open*1.4-0.4)).Children(func() {
		ui.Box(c).Size(36, 36).Radius(tok.R8).Background(p.Accent).Center().Shrink(0).Children(func() {
			ui.Icon(c, pud.Icon("play")).FontSize(18).TextColor(p.AccentInk)
		})
		ui.Column(c).Grow(1).Gap(space.Px2).Children(func() {
			ui.Text(c, appName).FontSize(scale.Font(c, scale.Size14)).FontWeight(notchTitleWeight).
				TextColor(tok.Full.Text)
			ui.Text(c, "未在播放").FontSize(scale.Font(c, scale.Size12)).TextColor(tok.Full.Text2)
		})
		b := ui.ButtonBase(c.Key("notch-open")).Height(tok.BtnSmH).PaddingX(space.Px14).
			Radius(tok.BtnSmH / 2).Background(tok.Full.Hover).AlignItems(ui.Center).Justify(ui.Center).
			FocusRing(false).Label("打开")
		b.Children(func() {
			ui.Text(c, "打开").FontSize(scale.Font(c, scale.Size13)).FontWeight(notchTitleWeight).
				TextColor(tok.Full.Text).SingleLine()
		})
		if b.Clicked() {
			s.restoreMainWindow()
		}
	})
}

// viewNotchExpanded 是展开的那张播放卡（原型 `.notch-card`）：内边距 4 / 16 / 16，
// 三列 56 / 1fr / auto、列间距 12、垂直居中——56 的封面（圆角 8）、歌名（14 号 600）
// 加「歌手 · 时间」（12 号 #a6a8ae，时间等宽）、上一首 / 播放 / 下一首三枚 36 的图标键
// （#ecebe8）。**没有进度条、没有频谱**：原型的刘海就是这一张小卡。
func (s *Shell) viewNotchExpanded(c *ui.Context, st app.State, open, left float32) {
	n := &s.notchSt
	row := ui.Row(c).Key("notch-card").Absolute().Top(n.h0).Left(left).Width(notchOpenW).
		Padding(space.Px4, space.Px16, space.Px16, space.Px16).Gap(space.Px12).AlignItems(ui.Center).
		Opacity(max(0, open*1.4-0.4))
	row.Translate(0, (1-open)*-10)
	row.Children(func() {
		cov := ui.Box(c).Size(notchCardCover, notchCardCover).Radius(tok.R8).Clip().Shrink(0)
		if st.CoverURL == "" {
			cov.Background(ui.RGB(34, 34, 34))
		}
		cov.Children(func() {
			imgcache.ViewTitled(c, s.images, st.CoverURL, st.Title, notchCardCover, notchCardCover)
		})
		ui.Column(c).Key("notch-info").Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, st.Title).FontSize(scale.Font(c, scale.Size14)).FontWeight(notchTitleWeight).
				TextColor(tok.Full.Text).SingleLine()
			sub := st.Artist
			if st.Loading {
				sub = "正在缓冲…"
			}
			ui.Row(c).Key("notch-sub").MinWidth(0).Children(func() {
				if sub != "" {
					ui.Text(c, sub+" · ").FontSize(scale.Font(c, scale.Size12)).TextColor(tok.Full.Text2).SingleLine()
				}
				typo.Mono(ui.Text(c, miniClock(st.Position))).FontSize(scale.Font(c, scale.Size12)).
					TextColor(tok.Full.Text2).SingleLine()
			})
		})
		ink := tok.Full.Text
		ui.Row(c).Key("notch-ctl").Shrink(0).Children(func() {
			if pud.IconBtn(c, "notch-prev", "prev", "上一首", pud.IconOpts{Color: ink, Disabled: !s.app.CanPrev()}).Clicked() {
				s.app.Prev()
			}
			s.viewNotchPlay(c, st, ink)
			if pud.IconBtn(c, "notch-next", "next", "下一首", pud.IconOpts{Color: ink, Disabled: !s.app.CanNext()}).Clicked() {
				s.app.Next()
			}
		})
	})
}

// viewNotchPlay 是展开卡上那一枚播放 / 暂停键：和另外两枚一样的图标键（原型里它在刘海里
// 不画实心圆），取流时外面绕那一圈光（票 #271）。
func (s *Shell) viewNotchPlay(c *ui.Context, st app.State, ink ui.Color) {
	name := "play"
	if st.Playing {
		name = "pause"
	}
	b := pud.IconBtn(c, "notch-play", name, transportLabel(st.Playing), pud.IconOpts{Color: ink}).
		Disabled(!st.HasTrack && !st.Playing)
	b = s.withLoadHalo(c, b, st, tok.IconHit, st.HasTrack)
	if b.Clicked() {
		s.app.Toggle()
	}
}
