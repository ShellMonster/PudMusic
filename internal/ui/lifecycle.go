// 应用的生命周期：关掉之后去哪儿、第二个实例来了怎么办、开机自启对不对得上。
//
// 这一层全是「窗口活着的时候」的事，决策都很轻，重的活交给
// `internal/platform`（单实例、scheme、开机自启都是系统的事）。**都是主线程上的**：
// 这里的每个回调都由框架在主线程调，所以 Shell 的字段不需要锁。
package ui

import (
	"log"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/egoist/mygo"
)

// closeAction 是「用户点了关闭按钮」之后窗口的去向。
type closeAction int

const (
	// closeProceed 是照常关掉窗口，OnClosed 的收尾照跑。
	closeProceed closeAction = iota
	// closeToBackground 是把窗口收起来：窗口没了，应用还在，音乐还在放。
	closeToBackground
)

// planWindowClose 决定点关闭按钮时窗口的去向。
//
// quitting 这一条最要紧：**退出流程本身就是靠关窗口走完的**。MyGo 的
// prepareQuit 遍历所有窗口挨个 close（`mygo/app.go:190`），用户按的 ⌘Q、
// 设置选「关掉就退出」之后窗口关光触发的那次 Quit，走的都是同一条路。
// 这里要是还照设置把它拦下来，「退出」就变成了「退出不了」，现象还特别怪：
// 按了退出，窗口一收就没了，进程还在后台，用户只能去活动监视器里杀它。
//
// 所以 quitting 一律放行——**不是**「关掉时隐藏」这个设置说了算。
func planWindowClose(quitting bool, opt app.CloseWindowOption) closeAction {
	if quitting || opt == app.CloseWindowExit {
		return closeProceed
	}
	return closeToBackground
}

// installLifecycle 把应用级的生命周期装上。**要在 mygo.App.Run() 之前调**。
func (s *Shell) installLifecycle() {
	s.installMenu()

	// 退出流程的第一站。框架的 prepareQuit 先发 OnBeforeQuit、再挨个关窗口，
	// 所以这里记下的 quitting 到窗口的 OnClose 跑的时候一定已经是 true。
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { s.quitting = true })

	// 退进程时把登记在系统上的全局快捷键撤掉。**不是**关窗口时撤：关窗口选「隐藏」
	// 时应用还活着、音乐还在放，这时候撤了等于「按 ⌘⌥P 在别的应用里没反应，
	// 而本应用的窗口明明还开着」——托盘那套（#28）要的正是关窗之后仍然可用。
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { s.uninstallGlobalShortcuts() })

	// 装了 OnWindowAllClosed 就等于告诉框架「窗口全关也别退出」——这正是
	// macOS 上要的：窗口收起、应用还在。
	//
	// 但设置说「关掉就退出」的时候不能这样。那一条就是用户要的退出，不放行
	// 就变成了「进程活着但什么都没有，用户只能强杀」。
	//
	// ⚠️ 这里读的是 **closeOption 那份缓存**，不是现去问设置。现去问会读到错的
	// 值：窗口真关掉时 OnClosed 已经调过 `CloseSettings`，把设置的挂载摘掉了，
	// 那一刻 `SettingsSnapshot()` 返回的不是用户的设置、是 `DefaultSettings("")`
	// 也就是 hide。于是选了「直接退出」的用户点关闭 → 窗口没了、进程还在，
	// 而界面上那个设置还亮着（issue #28 验收第 1 条就是这么红的）。
	// 缓存就绕开了这个先后顺序：OnClose 每关一次窗口都会读一次（那会儿挂载还在）。
	mygo.App.OnWindowAllClosed(func() {
		if s.closeOption == app.CloseWindowExit {
			mygo.App.Quit()
		}
	})

	// 第二个实例来了。它转交过来的命令行里要是有 biu://…，框架自己会投给
	// OnOpenURL（`mygo/deeplink.go` 的 deliverArgs，由 single_instance.go 的
	// acceptSecondInstances 调），这里只负责把窗口叫回来：用户明明是在唤起这个
	// 应用，结果窗口还藏在那儿，是最容易被当成「点了没反应」的一种情况。
	mygo.App.OnSecondInstance(func([]string, string) { s.openWindow() })

	// biu://… 到了就跳到它指的那一页。
	//
	// ⚠️ **迷你开着的时候由 `handleDeepLink` 先把它关掉**，理由在那边。
	// `openWindow` 排在这一步之后：迷你已经被让开了，它叫回来的才是主窗口。
	mygo.App.OnOpenURL(func(u string) {
		route, ok := platform.RouteFromURL(u)
		if !ok {
			return
		}
		s.handleDeepLink(route)
		s.openWindow()
	})

	// macOS：点程序坞里的图标把窗口叫回来。设成「关闭窗口时隐藏」之后，
	// 这就是用户找回窗口的主要入口（第二个实例也能，但没人会为了看一眼
	// 播放状态去再启动一遍应用）。
	//
	// 框架在 macOS 上是 applicationShouldHandleReopen 才触发它，不是
	// DidBecomeActive——所以这里不会因为 RestoreFromBackground 里的
	// App.Focus 又绕回来。
	mygo.App.OnActivate(func(bool) { s.openWindow() })
}

// handleDeepLink 是「biu://… 指到了 route」那一半：让开迷你，跳到那一页。
//
// ⚠️ **先让开迷你**。`navigate` 改的是**主窗口**那一侧的路由（路由表是主窗口的），
// 而主窗口在迷你期间是收着的。不让开的话，按一个链接的结果是「窗口弹出来了，还是
// 迷你那一扇，页面上什么都没有」——`openWindow` 那一支优先叫回迷你窗就直接 return，
// 连主窗口都不弹。
//
// 让开是有意的那一半：链接指的是主窗口里的一页，而两个窗口互斥（见 `openWindow`），
// 所以「去那一页」只能是把迷你让开。
//
// 叫回窗口那一步**不在这里**：`openWindow` 要走系统（`RestoreFromBackground`，或者
// 压根没有窗口时的 `WhenReady`），在无头 Tester 里调不到也量不到。而「迷你被关掉」
// 和「路由换了」这两样都是纯逻辑，留在这一半里测。
func (s *Shell) handleDeepLink(route string) {
	s.dropMini()
	s.navigate(route)
}

// openWindow 把主窗口叫回来；没有窗口就开一个。**绝不开第二个。**
//
// 单实例的意义就在这儿：从程序坞再点一下图标弹出两个窗口，两个窗口各放一首
// 歌、进度条各走各的，是最难解释的一种错。
//
// ⚠️ 这里**必须绕一层 WhenReady，不能直接建窗口**。有一个很窄但是真实存在的
// 窗口期：框架的 `App.Run()` 先把事件循环转起来（`b.Run()` 开始 drain
// postMain 队列），`applicationDidFinishLaunching` 才把 `ready` 置上。在这两件
// 事之间，第二个实例转交过来的命令行**已经会被处理**了——它的回调走的也是
// postMain。而 `NewWindow` 在主线程上遇到「还没 ready」是直接 panic 的
// （`mygo/window.go:374`）。
//
// 症状是：用户把图标连点两下（或者同一个链接点两下），第一个进程当场崩掉，
// 栈顶是 `NewWindow ← openWindow ← OnSecondInstance`。改用 WhenReady 之后，
// 应用没 ready 它就排进队列等 ready，ready 之后立刻建——没有窗口可开的时段
// 也就没有了。
//
// ⚠️ **迷你播放器开着的时候被叫回来的应该是那一扇**：主窗口是「进迷你」那一步
// 收起来的（`miniplayer.go` 的 `openMiniPlayer`），而两个窗口是互斥的。照常把主
// 窗口 Show 出来的症状是「点了程序坞图标，主窗口和迷你播放器一起冒出来」——
// 用户刚缩小的那个窗口又全屏回来了，而迷你那一扇还在。
func (s *Shell) openWindow() {
	// 迷你窗口优先。`openMiniPlayer` 里主窗口已经被收起来了，所以这一支要先把
	// 迷你那一扇叫到前面来。
	if s.mini != nil && s.mini.win != nil && !s.mini.win.IsDestroyed() {
		platform.RestoreFromBackground(s.mini.win)
		return
	}
	if s.win != nil && !s.win.IsDestroyed() {
		platform.RestoreFromBackground(s.win)
		return
	}
	mygo.App.WhenReady(s.ensureWindow)
}

// ensureWindow 真的把窗口开出来。**只能在应用 ready 之后调**（见 openWindow）。
func (s *Shell) ensureWindow() {
	// 排队的这段时间里窗口可能已经被人叫起来了（两个第二实例几乎同时到）。
	if s.win != nil && !s.win.IsDestroyed() {
		platform.RestoreFromBackground(s.win)
		return
	}
	w := mygo.NewWindow(WindowOptions(s.View))
	s.Attach(w)
	// 系统媒体会话在 Windows 上是**按窗口**的（浮窗和锁屏走 SMTC，任务栏
	// 缩略图上那三个按钮直接长在这个 HWND 上），而窗口到这里才建出来——
	// 所以只能在建完之后补这一步。macOS 那边是全局的，这一句是空操作。
	s.bindMedia(w)
}

// autoStartService 是「开机自启在系统上的那一份」。
//
// 做成字段是为了能替掉它：真的那一份要走事件循环（框架那两个 API 都是
// `onMainValue`），无头环境里没有事件循环，不替掉就只能等上真系统才验得到
// 「先动系统、成功了才写设置」那条顺序。和 pageBody 同一个道理。
type autoStartService interface {
	set(on bool) error
	enabled() bool
}

// systemAutoStart 是真的那一份，也就是 internal/platform 那两个函数。
type systemAutoStart struct{}

func (systemAutoStart) set(on bool) error { return platform.SetAutoStart(on) }
func (systemAutoStart) enabled() bool     { return platform.AutoStartEnabled() }

// syncAutoStart 启动时把设置里的「开机自启」补到系统上。**主线程调**。
//
// 见 platform.PlanAutoStart：只补「设置说开、系统说关」这一种，而且失败
// 只记日志不改设置——设置里的那个开关是用户的意图，注册不上是系统那边的事
// （没打包成 bundle、用户没给权限），拿用户的意图去迁就系统的现状，方向反了。
func (s *Shell) syncAutoStart() {
	want := s.app.SettingsSnapshot().AutoStart
	if platform.PlanAutoStart(want, s.autostart.enabled()) != platform.AutoStartTurnOn {
		return
	}
	if err := s.autostart.set(true); err != nil {
		log.Printf("biu: 设置里开着开机自启，系统里却没开，注册失败：%v", err)
	}
}

// SetAutoStart 改「开机自启」这个设置，并立刻作用到系统上。给设置页用。
//
// **先动系统、成功了才写设置**，顺序反了会留下「设置说开、系统说关」——启动
// 时的对齐（见 syncAutoStart）恰好只补这一种，于是用户下次开机发现根本没自启，
// 而界面上那个开关还亮着。系统动不了的时候设置一个字都不改，让设置页把错
// 显示出来，而不是让用户以为已经开了。
func (s *Shell) SetAutoStart(on bool) error {
	if err := s.autostart.set(on); err != nil {
		return err
	}
	_, err := s.app.UpdateSettings(func(st *app.Settings) { st.AutoStart = on })
	return err
}
