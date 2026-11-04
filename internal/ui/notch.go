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
//   - `notchHello`：第一次放歌时的引导（「刘海里也能控制播放 + 开启」）。
package ui

import (
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// 刘海窗的窗尺寸（固定，不变）：展开卡宽 468 + 两侧余量，高 240（刘海高 ~38
// + 展开深度 ~178 + 余量）。窗尺寸由 `pinToTop` 一次钉在屏顶正中，之后
// 不再动——动的是画出来的形状。
const (
	notchWinW = 468 + 60
	notchWinH = 240

	notchOpenW  = 468 // 展开卡的宽
	notchIdleW  = 330 // 无歌时那张卡的宽
	notchHelloW = 340 // 引导那条的宽

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

	// hello 是「正在引导」（第一次放歌时从刘海里长出来那句提示）。
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

	// polling 是「下一帧 poll 排上了没有」——自适应轮询的去抖。
	polling bool

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

	// 频谱条：见 `viewNotchLevels`。
	bars []float32

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
		if !s.quitting {
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

// releaseNotch 收掉刘海窗自己占着的（逐窗口那一摊），不收应用。
func boolPtr(b bool) *bool { return &b }

func (s *Shell) releaseNotch() {
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
	s.win.Hide()
	s.watchNotch()
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
	case n.open && !st.HasTrack:
		return notchIdleW, n.h0 + 72
	case n.open:
		return notchOpenW, n.h0 + 178
	case n.hello && st.HasTrack:
		return notchHelloW, n.h0 + 58
	case st.HasTrack && s.notchEnabled() && st.Playing:
		// 两翼：刘海宽 + 两边各一只翅膀（封面 + 频谱），不挡菜单栏文字。
		return n.w0 + 2*max(0, n.h0-12) + 20, n.h0
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
func (s *Shell) notchPollAgain(inside bool) {
	n := &s.notchSt
	var d time.Duration
	switch {
	case inside || n.open:
		d = 33 * time.Millisecond
	case n.active || !n.watched:
		d = 100 * time.Millisecond
	}
	if d == 0 || n.polling {
		return
	}
	n.polling = true
	s.clock.EveryFunc(d, func() {
		n.polling = false
		s.notchPoll()
	})
}

// ---- 引导（第一次放歌）----

// notchGreet 是「第一次放歌的时候，从刘海里长出来那句提示」。
//
// ⚠️ **默认不开**的用户才走这条：已经开着的不会再被问候一次
// （`NotchPlayerGreeted` 记过一次）。
func (s *Shell) notchGreet() {
	n := &s.notchSt
	if s.notchEnabled() || s.app.SettingsSnapshot().NotchPlayerGreeted {
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
	time.AfterFunc(time.Second, func() {
		if !n.hello && !s.notchEnabled() && s.win != nil {
			s.win.Hide()
			s.watchNotch()
		}
	})
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
	if s.notch != nil {
		s.closeNotchPlayer()
		return
	}
	s.openNotchPlayer()
}

// ---- 绘制 ----

// viewNotch 是刘海窗那一帧：一个贴着刘海长出来的黑色形状 + 里面的三态内容。
func (s *Shell) viewNotch(c *ui.Context) {
	s.applyFrameTheme(c)
	s.images.BeginFrame(c.Now())
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
	foot := 10 + 18*open
	flare := 6 + 8*open
	cx := n.winW / 2

	stage.Draw(func(p *ui.Painter, r ui.Rect) {
		if w < 1 || h < 1 {
			return
		}
		path := notchShape(r.X+cx, w, h, flare, foot)
		if open > 0.05 {
			p.Shadow(ui.Rect{X: r.X + cx - w/2, Y: r.Y + h - 40, W: w, H: 40},
				foot, 0, 14, 30, -6, ui.RGBA(0, 0, 0, 0.45*open))
		}
		p.FillPath(path, ui.RGB(0, 0, 0))
	})
	stage.Children(func() {
		body := ui.Box(c).Absolute().Top(0).Left(cx-w/2).Size(w, h).
			Radius(0, 0, foot, foot).Clip().TextColor(ui.RGB(255, 255, 255))
		body.Children(func() {
			if !st.HasTrack {
				s.viewNotchIdle(c, open, (w-notchIdleW)/2)
				return
			}
			// 两翼只在展开没盖掉它们的时候画。
			s.viewNotchWings(c, st, 1-open)
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

// viewNotchWings 是刘海的两翼：左封面、右频谱条。
//
// ⚠️ **只在收起的时候画**（`shown = 1-open`）：展开卡盖下来的时候两翼
// 藏在它底下，画两遍是白画。
func (s *Shell) viewNotchWings(c *ui.Context, st app.State, shown float32) {
	if shown < 0.02 {
		return
	}
	n := &s.notchSt
	sz := n.h0 - 12
	ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(n.h0).
		PaddingX(space.Px7).AlignItems(ui.Center).Opacity(shown).
		Children(func() {
			cov := ui.Box(c).Size(sz, sz).Radius(sz * 0.28).Clip()
			if st.CoverURL == "" {
				cov.Background(ui.RGBA(255, 255, 255, 0.12))
			}
			cov.Children(func() {
				imgcache.ViewTitled(c, s.images, st.CoverURL, st.Title, sz, sz)
			})
			ui.Spacer(c)
			s.viewNotchLevels(c, st, 16, sz*0.7, 4)
		})
}

// viewNotchLevels 画频谱条：取当前帧的频谱条高度画几根柱。
//
// 颜色是封面主色染白一点（和满屏播放器那一排条同色系的取法）。
func (s *Shell) viewNotchLevels(c *ui.Context, st app.State, w, h float32, count int) {
	ui.Box(c).Size(w, h).Draw(func(p *ui.Painter, r ui.Rect) {
		bars := s.app.SpectrumBars(count, 0)
		if len(s.notchSt.bars) < count {
			s.notchSt.bars = make([]float32, count)
		}
		live := false
		for i, v := range bars {
			s.notchSt.bars[i] = v
			if v > 0.01 {
				live = true
			}
		}
		barW := r.W / float32(count*2-1)
		for i, v := range s.notchSt.bars {
			if v <= 0.01 {
				continue
			}
			bh := max(2, v*r.H)
			x := r.X + float32(i*2)*barW
			p.Fill(ui.Rect{X: x, Y: r.Y + r.H - bh, W: barW, H: bh},
				ui.RGBA(255, 255, 255, 0.9), 1)
		}
		if live {
			p.AnimationFrame()
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
			ui.Text(c, title).FontSize(scale.Font(c, scale.Size14)).FontWeight(700).SingleLine()
			ui.Text(c, hint).FontSize(scale.Font(c, scale.Size12)).TextColor(ui.RGBA(255, 255, 255, 0.55)).SingleLine()
		})
		b := ui.ButtonBase(c.Key("notch-keep")).Padding(space.Px6, space.Px14).Radius(999).
			Background(c.Theme().Accent).FocusRing(false).Label("开启刘海播放器")
		b.Children(func() {
			ui.Text(c, "开启").FontSize(scale.Font(c, scale.Size13)).FontWeight(600)
		})
		if b.Clicked() {
			s.notchKeep()
		}
	})
}

// viewNotchIdle 是「没歌」那张卡：「未在播放」+「打开」。
func (s *Shell) viewNotchIdle(c *ui.Context, open, left float32) {
	if open < 0.02 {
		return
	}
	n := &s.notchSt
	ui.Row(c).Absolute().Top(n.h0+4).Left(left).Width(notchIdleW).
		Padding(space.Px10, space.Px20, space.Px14, space.Px20).Gap(space.Px12).AlignItems(ui.Center).
		Opacity(max(0, open*1.4-0.4)).Children(func() {
		ui.Box(c).Size(36, 36).Radius(10).Background(c.Theme().Accent).Center().Children(func() {
			ui.Icon(c, icon(iconPlay)).Size(18, 18).TextColor(ui.RGB(255, 255, 255))
		})
		ui.Column(c).Grow(1).Gap(space.Px2).Children(func() {
			ui.Text(c, appName).FontSize(scale.Font(c, scale.Size14)).FontWeight(700)
			ui.Text(c, "未在播放").FontSize(scale.Font(c, scale.Size12)).TextColor(ui.RGBA(255, 255, 255, 0.5))
		})
		b := ui.ButtonBase(c.Key("notch-open")).Padding(space.Px6, space.Px14).Radius(999).
			Background(ui.RGBA(255, 255, 255, 0.14)).FocusRing(false).Label("打开")
		b.Children(func() {
			ui.Text(c, "打开").FontSize(scale.Font(c, scale.Size13)).FontWeight(600)
		})
		if b.Clicked() {
			s.restoreMainWindow()
		}
	})
}

// viewNotchExpanded 是展开的那张播放卡：封面 + 歌名/歌手 + 频谱 + 进度 + 控制。
func (s *Shell) viewNotchExpanded(c *ui.Context, st app.State, open, left float32) {
	n := &s.notchSt
	col := ui.Column(c).Absolute().Top(n.h0+6).Left(left).Width(notchOpenW).
		Padding(space.Px8, space.Px22, space.Px16, space.Px22).Gap(space.Px12).
		Opacity(max(0, open*1.4-0.4))
	col.Translate(0, (1-open)*-10)
	col.Children(func() {
		// 上行：封面 + 歌名/歌手 + 频谱。
		ui.Row(c).Gap(space.Px14).AlignItems(ui.Center).Children(func() {
			cov := ui.Box(c).Size(60, 60).Radius(12).Clip()
			if st.CoverURL == "" {
				cov.Background(ui.RGBA(255, 255, 255, 0.12))
			}
			cov.Children(func() {
				imgcache.ViewTitled(c, s.images, st.CoverURL, st.Title, 60, 60)
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(space.Px4).Children(func() {
				ui.Text(c, st.Title).FontSize(scale.Font(c, scale.Size15)).FontWeight(700).SingleLine()
				if st.Loading {
					ui.Text(c, "正在缓冲…").FontSize(scale.Font(c, scale.Size13)).TextColor(ui.RGBA(255, 255, 255, 0.55)).SingleLine()
				} else if st.Artist != "" {
					ui.Text(c, st.Artist).FontSize(scale.Font(c, scale.Size13)).TextColor(ui.RGBA(255, 255, 255, 0.55)).SingleLine()
				}
			})
			s.viewNotchLevels(c, st, 26, 22, 5)
		})
		// 中行：进度。
		ui.Row(c).Gap(space.Px10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, clockOrDash(st.Position)).FontSize(scale.Font(c, scale.Size11)).
				FontFeatures("tnum").TextColor(ui.RGBA(255, 255, 255, 0.5)).Width(30)
			s.viewNotchProgress(c, st)
			ui.Text(c, "-"+clockOrDash(st.Duration-st.Position)).FontSize(scale.Font(c, scale.Size11)).
				FontFeatures("tnum").TextColor(ui.RGBA(255, 255, 255, 0.5)).Width(34).TextAlign(ui.End)
		})
		// 下行：控制键。
		ui.Box(c).Margin(-6, space.None, space.None, space.None).Children(func() {
			ui.Row(c).Gap(space.Px10).AlignItems(ui.Center).Children(func() {
				if flatIconButton(c, iconPrev, "上一首", 18).
					Key("notch-prev").Size(28, 28).Padding(space.None, space.Px5).
					Disabled(!s.app.CanPrev()).Clicked() {
					s.app.Prev()
				}
				s.viewNotchPlay(c, st)
				if flatIconButton(c, iconNext, "下一首", 18).
					Key("notch-next").Size(28, 28).Padding(space.None, space.Px5).
					Disabled(!s.app.CanNext()).Clicked() {
					s.app.Next()
				}
			})
		})
	})
}

// viewNotchPlay 是展开卡上那一枚播放/暂停键。
func (s *Shell) viewNotchPlay(c *ui.Context, st app.State) {
	const style = 36
	if st.Playing {
		b := flatIconButton(c, transportIcon(true), transportLabel(true), 28).
			Key("notch-play").Size(style, style).Padding(space.None, space.Px4)
		b = s.withLoadHalo(c, b, st, 28, true)
		if b.Clicked() {
			s.app.Toggle()
		}
		return
	}
	b := flatIconButton(c, transportIcon(false), transportLabel(false), 28).
		Key("notch-play").Size(style, style).Padding(space.None, space.Px4).
		Disabled(!st.HasTrack)
	b = s.withLoadHalo(c, b, st, 28, st.HasTrack)
	if b.Clicked() {
		s.app.Toggle()
	}
}

// viewNotchProgress 是刘海窗的进度条（和迷你那条同一份 scrub 状态）。
func (s *Shell) viewNotchProgress(c *ui.Context, st app.State) {
	known := st.Duration > 0
	sl := ui.Slider(c.Key("notch-progress"), &s.miniScrub, 0, 1).Grow(1).
		Height(10).Label("播放进度").Disabled(!known)
	sl = withChapterDotsCore(sl, st.Chapters, st.Duration)
	switch held := sl.Pressed(); {
	case held && !s.miniHeld:
		s.miniHeld = true
		s.app.ScrubStart()
	case !held && s.miniHeld:
		s.miniHeld = false
		s.app.ScrubEnd(s.miniScrub)
	case !s.miniHeld && known:
		s.miniScrub = float64(st.Position) / float64(st.Duration)
	}
}
