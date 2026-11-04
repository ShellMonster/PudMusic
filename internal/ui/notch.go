// macOS 刘海窗：贴在屏幕刘海那一块的一条迷你播放条，指针探进刘海区就展开
// 成一张播放卡，移开一会儿自己收回去。
//
// # 做法从哪来
//
// 参考 [boring.notch](https://github.com/TheBoredTeam/boring.notch)（GPL-3.0，
// 只学思路不抄代码）：`NSPanel` 设 `nonactivatingPanel` + 窗级压过菜单栏 +
// `canJoinAllSpaces|stationary|ignoresCycle|fullScreenAuxiliary`，悬停时把
// 藏在刘海底下的窗拉大、填上播放卡。那一套几何（刘海宽 = 屏宽 - 左右辅助
// 区、菜单栏高 = frame - visibleFrame）在 `notch_darwin.go`。
//
// # 和迷你播放器的关系
//
// 它是**第三扇窗**，不和主窗/迷你窗互斥：它自己一条命，开着的时候不挡
// 那两个的开合（主窗收去后台、迷你停在角落的时候，刘海都还在）。三扇窗
// 各一个 `Shell`、共用同一个 `internal/app.App`——和迷你那一份一样的接法
// （`adoptShared` 接主窗口的封面缓存 / 快捷键 / 搜索历史 / notice）。
//
// # 收起不是隐藏，是「缩回刘海底下」
//
// 窗本身一直在（`level = NSMainMenuWindowLevel + 2`、`transparent`）：收起
// 时只有 **刘海宽 × 2px** 高，贴在刘海下缘那一行，画一条正在播放的指示；
// 展开时整扇窗变高、把播放卡铺出来。
//
// ⚠️ **不能把收起的窗整个 Hide**：藏起来的窗不再收鼠标事件，而「指针探进
// 刘海区」恰恰要靠一个全局事件 tap 看见（`notch_darwin.go` 的
// `notchStartPointerWatch`）——窗一藏，那个 tap 没东西可喂，刘海就成了死的。
//
// # 交互时序（照 boring.notch 的那几个数）
//
//	指针进触发区 → 350ms 内一直在 → 展开（`openNotch`）
//	展开中指针离开 → 350ms 宽限内回来 → 不收；过宽限 → 收起（`closeNotch`）
//
// 轮询在主窗口那一侧跑（`notchTick`）：框架拿不到「没进窗的鼠标」，所以
// 「指针在不在刘海区」这件事由一个 60ms 一跳的定时器去读 CGEvent tap 交
// 上来的位置——`notch_darwin.go` 的 `notchTapHandle`。
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

// 刘海窗的两个高度：
//
//   - `notchCollapsedH`（收起）只有 2px：贴在刘海下缘画一条「正在放」的
//     指示，还没挡到桌面就几乎看不见——而它**得在那儿**（上面文件头那
//     段：藏起来就收不到鼠标）。
//   - `notchOpenH`（展开）140px：比迷你播放器（84）高一点——刘海区宽，排
//     得开一行控制；而它不是常驻桌面控件，高一点不碍事。
const (
	notchCollapsedW = 220 // 收起时的宽（刘海宽约 200 + 一点点余量）
	notchCollapsedH = 2

	notchOpenW = 640
	notchOpenH = 140

	// notchHoverOpenDelay / notchHoverExitDelay 是开合的两个毫秒数（照
	// boring.notch 的 minimumHoverDuration / hoverExitDelayMilliseconds）。
	notchHoverOpenDelay = 300 * time.Millisecond
	notchHoverExitDelay = 350 * time.Millisecond

	// notchPollInterval 是主窗那个「指针在不在刘海」的轮询间隔。
	notchPollInterval = 60 * time.Millisecond
)

// notchState 是刘海窗的开合状态机。
type notchState int

const (
	notchClosed notchState = iota // 收着：刘海底下那条细指示
	notchOpen                     // 展开：播放卡铺出来
)

// notchShell 是刘海窗自己的外壳字段（挂在 `Shell` 上，见 shell.go 那一段）。
//
// 它和 `miniCurtain` 是同一路数：状态（开/收）、进出时间（算宽限）、
// 以及那个 CGEvent tap 报上来的指针位置。
type notchShell struct {
	state notchState

	// pointerIn 是「指针现在在刘海触发区里吗」——tap 那边写、主线程读。
	pointerIn bool
	// 进出时间：算「停够 300ms 没有」和「走开 350ms 没有」。
	inAt, outAt time.Time

	// lastPointer / pointerAt 是 tap 最近报的那个点、和「现在在算的那一个」
	//（去抖：同一位置不重复算进出）。
	lastPointer notchNSPoint
	pointerAt   notchNSPoint
	hasPointer  bool
}

// NewNotch 建刘海窗那个外壳。**状态和主窗口、迷你窗共用同一个 App**。
//
// ⚠️ **它归「收不掉的第三扇窗」那一档**：所以 `OnClosed` 走 `releaseNotch`
// （不收应用、不停播放——理由同 `attachMiniWindow` 顶上那一段），而**不是**
// `Shell.Close`（那一个会 `s.app.Close()` 把正在放的歌停掉）。
func NewNotch(a *app.App, main *Shell) *Shell {
	s := newShellWithImages(a, "")
	// 刘海窗是面板（Panel 语义），点它不激活应用——和迷你窗同一个理由：
	// 「有没有焦点」不能拿来判它看不看得见（presence.go 的 freeWhileBlurred）。
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
// `Frameless`、`AlwaysOnTop`——**没有** `StateKey`（它永远贴屏顶居中，没有
// 「上次拖到哪儿」可言），也没有 `MinWidth/MinHeight`（收合/展开的尺寸由
// 代码改，不由用户拖）。
func NotchWindowOptions(view func(*ui.Context)) mygo.WindowOptions {
	return mygo.WindowOptions{
		Title:             "布丁音乐刘海",
		Width:             notchCollapsedW,
		Height:            notchCollapsedH,
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
		Content:           ui.View(view),
	}
}

// openNotchPlayer 开出刘海窗。给「进迷你」之外的第三个入口用：
// 主窗的 navbar、托盘、快捷键都能叫它。
//
// ⚠️ **它不挡主窗/迷你窗**：和 `openMiniPlayer` 不同，这里**不**收主窗——
// 刘海是第三扇窗，开着它不该把用户手上的那扇收走。
func (s *Shell) openNotchPlayer() {
	if s.notch == nil {
		s.newNotchShell()
		s.ensureNotchWindow()
		return
	}
	// 已经开着就叫到前面（它可能正收在刘海底下，看不见的等价于不在）。
	if w := s.notch.win; w != nil && !w.IsDestroyed() {
		w.ShowInactive()
	}
}

// newNotchShell 建出刘海那个外壳并和主窗口接上（`s.notch` 指过去、反向指回来）。
// **不建窗口**——建窗口要事件循环，那一半在 `ensureNotchWindow`。
//
// ⚠️ **已经有一个就原样还回去**：连按两下「刘海」会开出两个刘海窗。
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

// closeNotchPlayer 关掉刘海窗。**叫回主窗的事不在它身上**：刘海本来就是「主窗
// 还开着的时候加的那一扇」，关了它主窗该在哪儿还在哪儿。
func (s *Shell) closeNotchPlayer() {
	s.dropNotch()
}

// dropNotch 关掉刘海窗，返回「刚才确实开着」。和 `dropMini` 一个形状：
// 先摘 `s.notch` 的反向指针再关窗，免得 `OnClosed` 回头再找一遍。
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

// ensureNotchWindow 真的把刘海窗开出来。**只能在应用 ready 之后调**
// （`mygo.NewWindow` 在还没 ready 时是直接 panic 的，和 `ensureMiniWindow`
// 顶上那句同一理由）。
func (s *Shell) ensureNotchWindow() {
	n := s.notch
	mygo.App.WhenReady(func() {
		if n == nil || s.notch != n || n.win != nil {
			return
		}
		opts := NotchWindowOptions(n.View)
		w := mygo.NewWindow(opts)
		n.attachNotchWindow(w)
		// 🔴 macOS 原生半层：窗级压过菜单栏、贴屏顶居中、起全局指针监听。
		// 这三件都要 `NativeHandle()` 拿到的 NSWindow*，所以排在建窗之后。
		notchConfigure(w)
		notchStartWatch(n)
		n.installNotchPoll()
	})
}

// bindNotchWindow 登记这一扇窗的唤醒句柄（铁律三：逐窗口挨个叫醒）。
//
// ⚠️ **重复调是空操作**（同 `bindMiniWindow`）：建外壳一次、真窗建好又一次。
func (s *Shell) bindNotchWindow() {
	if s.wakeRemove != nil {
		return
	}
	s.wakeRemove = s.app.AddWake(s.wake.invalidate)
}

// attachNotchWindow 把刘海窗接到它的外壳上。
//
// ⚠️ **和 `Attach`/`attachMiniWindow` 一样不复用**：主窗那套 `OnClosed` 会
// 停播放（整应用收尾），迷你那套会叫回主窗——刘海窗的关只收它自己那一摊。
func (s *Shell) attachNotchWindow(w *mygo.Window) {
	s.win = w
	s.bindNotchWindow()
	s.watchWindow(w)
	w.OnClosed(func() {
		if s.win == w {
			s.win = nil
		}
		// 摘掉 tap 和轮询（tap 回调拿着这一个外壳，不摘就一直喂）。
		notchStopWatch()
		s.stopNotchPoll()
		s.releaseNotch()
		// 通知主窗「刘海没了」：下次再按「刘海」得重新开一扇。
		if s.main != nil && s.main.notch == s {
			s.main.notch = nil
		}
	})
}

// releaseNotch 收掉刘海窗自己占着的（逐窗口那一摊），不收应用。
// 和 `releaseMini` 一个形状：它不 `s.app.Close()`。
func (s *Shell) releaseNotch() {
	s.closePerWindow()
}

// ---- 开合状态机 ----

// notchOpenNow 展开刘海（收着 → 铺成播放卡）。
func (s *Shell) notchOpenNow() {
	if s.notchState() == notchOpen {
		return
	}
	s.notchSt.state = notchOpen
	s.notchApplySize()
	s.wake.invalidate()
}

// notchCloseNow 收起刘海（铺着 → 缩回刘海底下那条）。
func (s *Shell) notchCloseNow() {
	if s.notchState() == notchClosed {
		return
	}
	s.notchSt.state = notchClosed
	s.notchApplySize()
	s.wake.invalidate()
}

func (s *Shell) notchState() notchState { return s.notchSt.state }

// notchApplySize 把窗的尺寸和位置改成当前状态那一档。
//
// ⚠️ **改位置要跟着尺寸一起改**：窗是贴屏顶居中的——高度变了而 Y 不动的话，
// 展开的那一扇是从刘海底下**往下长**（对），但收起时要重新回到屏顶那条 2px。
func (s *Shell) notchApplySize() {
	w := s.win
	if w == nil || w.IsDestroyed() {
		return
	}
	if s.notchSt.state == notchOpen {
		w.SetSize(notchOpenW, notchOpenH)
	} else {
		w.SetSize(notchCollapsedW, notchCollapsedH)
	}
	notchReposition(w)
}

// ---- 轮询：指针在不在刘海 ----

// installNotchPoll 在主窗这一侧装一个 60ms 一跳的轮询：读 tap 报上来的位置，
// 进触发区 → 300ms 后展开；离开 → 350ms 宽限后收起。
//
// ⚠️ **轮询挂在刘海那个外壳上**（不是主窗）：它叫醒的是刘海窗
// （`s.wake.invalidate`）。主窗挂不挂它无所谓——它不画刘海。
func (s *Shell) installNotchPoll() {
	// tap 报上来的位置先进 `s.notchSt.lastPointer`（`notchStartWatch` 那边
	// 用 `notchMu` 写），轮询再拿去算「在不在触发区」——两件事情分开：
	// tap 那边只管报点，开/收的判断全在这条 60ms 一跳的轮询里。
	s.notchPollStop = s.clock.EveryFunc(notchPollInterval, func() {
		s.notchTick()
	})
}

func (s *Shell) stopNotchPoll() {
	if s.notchPollStop != nil {
		s.notchPollStop()
		s.notchPollStop = nil
	}
}

// notchTick 是 60ms 一跳的那次判断。**主线程调**（`s.clock.EveryFunc` 的
// 回调就在主线程上跑，和 `viewNotch` 同一条）。
//
// ⚠️ **它读的是这个外壳自己的 `notchSt`**：tap 报来的点由 `notchStartWatch`
// 写进这个外壳（`attachNotchWindow` 里 `notchStartWatch(n)`）——和 tap 那边
// 用 `notchMu` 守着，这边读的时候同一把锁拿一下。
func (s *Shell) notchTick() {
	s.notchMu.Lock()
	p := s.notchSt.lastPointer
	has := s.notchSt.hasPointer
	in := s.notchSt.pointerIn
	s.notchMu.Unlock()
	if !has {
		return
	}

	if in {
		// 指针在区里挪了：重算「停够没有」。
		if !p.equals(s.notchSt.pointerAt) {
			s.notchSt.pointerAt = p
			s.notchSt.inAt = s.clock.Now()
			return
		}
		if s.notchSt.state == notchClosed && s.clock.Now().Sub(s.notchSt.inAt) >= notchHoverOpenDelay {
			s.notchOpenNow()
		}
		return
	}
	if s.notchSt.state == notchOpen {
		if !p.equals(s.notchSt.pointerAt) {
			s.notchSt.pointerAt = p
			s.notchSt.outAt = s.clock.Now()
			return
		}
		if s.clock.Now().Sub(s.notchSt.outAt) >= notchHoverExitDelay {
			s.notchCloseNow()
		}
	}
}

func (p notchNSPoint) equals(q notchNSPoint) bool {
	return p.X == q.X && p.Y == q.Y
}

// ---- 绘制 ----

// viewNotch 是刘海窗那一帧。
//
// 🔴 铁律二：先装主题。收合时画一条「正在放」的细指示；展开时一张播放卡
// （封面 + 歌名/歌手 + 播放/暂停 + 上/下一首 + 进度条）。
func (s *Shell) viewNotch(c *ui.Context) {
	s.applyFrameTheme(c)
	s.images.BeginFrame(c.Now())
	st := s.app.Snapshot()
	t := c.Theme()

	if s.notchSt.state == notchOpen {
		s.viewNotchOpen(c, st, t)
		return
	}
	s.viewNotchCollapsed(c, st, t)
}

// ---- 收合：刘海底下那条细指示 ----

// viewNotchCollapsed 是收着的那一帧：一条贴着刘海下缘的细条，正在放就亮
// 主色、没在放就暗掉。
//
// ⚠️ **它只有 2px 高**：不是「窗藏起来了」——窗还在（文件头那段：藏起来
// 就收不到 tap 报的点），是它画的东西几乎看不见。
func (s *Shell) viewNotchCollapsed(c *ui.Context, st app.State, t *ui.Theme) {
	ui.Box(c).Key("notch-collapsed").FillWidth().FillHeight().Children(func() {
		bar := ui.Box(c).Key("notch-bar").Grow(1).FillHeight()
		if st.Playing {
			bar.Background(t.Accent)
		}
	})
}

// ---- 展开：一张播放卡 ----

// viewNotchOpen 是展开的那一帧：圆角卡片，封面 + 歌名/歌手 + 控制 + 进度。
//
// ⚠️ **底是自己画的**：窗是透明的（`NotchWindowOptions.Transparent`），
// 框架清成透明，这一块不透明底是它唯一的底——不画的话窗在桌面上是隐形的
// （同 `MiniWindowOptions` 顶上那一条）。
//
// ⚠️ **只有下缘圆角**：刘海窗是从刘海底下**往下长**的，上缘贴着刘海的
// 圆角矩形本身、不该再圆一次；下缘圆角才是它自己的边。
func (s *Shell) viewNotchOpen(c *ui.Context, st app.State, t *ui.Theme) {
	cardBG := s.coverWash(t, t.Surface)

	ui.Box(c).Key("notch-open").FillWidth().FillHeight().
		Radius(t.Radius).Background(cardBG).Clip().
		Children(func() {
			ui.Row(c).Key("notch").Fill().Padding(space.Px12, space.Px12, space.Px12, space.Px12).
				Gap(space.Px12).AlignItems(ui.Center).
				Children(func() {
					// 封面：56×56，圆角跟着主题。
					ui.Box(c).Key("notch-cover").Width(56).Height(56).
						Radius(t.Radius).Clip().
						Children(func() {
							if st.CoverURL == "" {
								return
							}
							imgcache.ViewTitled(c, s.images, st.CoverURL, st.Title, 56, 56)
						})
					// 中列：歌名 / 歌手 / 进度条。
					ui.Column(c).Key("notch-mid").Grow(1).FillHeight().
						Gap(space.Px2).
						Children(func() {
							ui.Row(c).Key("notch-title-row").FillWidth().Gap(space.Px8).AlignItems(ui.Center).
								Children(func() {
									title := st.Title
									if title == "" {
										title = "未在播放"
									}
									ui.Text(c, title).Key("notch-title").
										FontSize(scale.Font(c, scale.Size14)).TextColor(t.Text).MaxLines(1).Ellipsis("…")
									if st.Artist != "" {
										ui.Text(c, st.Artist).Key("notch-artist").
											FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).MaxLines(1).Ellipsis("…")
									}
								})
							ui.Box(c).Key("notch-progress").FillWidth().Padding(0, 0, 0, 0).
								Children(func() { s.viewNotchProgress(c, st) })
						})
					// 控制键：上一首 / 播放 / 下一首。
					ui.Row(c).Key("notch-transport").Gap(space.Px6).AlignItems(ui.Center).
						Children(func() {
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

// viewNotchPlay 是刘海窗上那一枚播放/暂停键。
//
// ⚠️ **画的是实心圆**（`transportIcon` 是 play-circle-fill）：它是这一行
// 的视觉重心，和迷你窗上那一枚同一个安排（`viewMiniPlay`）。
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

// viewNotchProgress 是刘海窗的进度条。
//
// ⚠️ **它和迷你那条是一份**（`miniScrub`/`miniHeld`）：三扇窗各画各的进度条，
// 但「值归谁」是同一个约定——按住的时候值归用户、松手才 seek
// （`viewMiniProgress` 顶上那句同一理由）。所以这里**不改**那一对字段：
// 刘海窗和迷你窗不会同时开——它和迷你互斥这件事由调用方管（现在没有「同
// 时开」的入口，见 `openNotchPlayer` 顶上那句）。
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

// toggleNotchPlayer 开 / 关刘海窗。
//
// ⚠️ **刘海窗那一侧要绕回主窗口**（同 `toggleMiniPlayer` 那一段理由）：
// 小窗那边的 `s.notch` 是 nil，照走会把刘海建在迷你外壳上——开出两扇。
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
