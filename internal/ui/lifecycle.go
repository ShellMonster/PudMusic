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

	// 装了 OnWindowAllClosed 就等于告诉框架「窗口全关也别退出」——这正是
	// macOS 上要的：窗口收起、应用还在。
	//
	// 但设置说「关掉就退出」的时候不能这样。那一条就是用户要的退出，不放行
	// 就变成了「进程活着但什么都没有，用户只能强杀」。
	mygo.App.OnWindowAllClosed(func() {
		if s.app.SettingsSnapshot().CloseWindowOption == app.CloseWindowHide {
			return
		}
		mygo.App.Quit()
	})

	// 第二个实例来了。它转交过来的命令行里要是有 biu://…，框架自己会投给
	// OnOpenURL（`mygo/deeplink.go` 的 deliverArgs，由 single_instance.go 的
	// acceptSecondInstances 调），这里只负责把窗口叫回来：用户明明是在唤起这个
	// 应用，结果窗口还藏在那儿，是最容易被当成「点了没反应」的一种情况。
	mygo.App.OnSecondInstance(func([]string, string) { s.openWindow() })

	// biu://… 到了就跳到它指的那一页。
	mygo.App.OnOpenURL(func(u string) {
		route, ok := platform.RouteFromURL(u)
		if !ok {
			return
		}
		s.openWindow()
		s.navigate(route)
	})

	// macOS：点程序坞里的图标把窗口叫回来。设成「关闭窗口时隐藏」之后，
	// 这就是用户找回窗口的主要入口（第二个实例也能，但没人会为了看一眼
	// 播放状态去再启动一遍应用）。
	//
	// 框架在 macOS 上是 applicationShouldHandleReopen 才触发它，不是
	// DidBecomeActive——所以这里不会因为 RestoreFromBackground 里的
	// App.Focus 又绕回来。
	mygo.App.OnActivate(func(bool) {
		if s.win == nil {
			return // 窗口还没开出来（或者正在退出）
		}
		platform.RestoreFromBackground(s.win)
	})
}

// openWindow 开主窗口。已经有一个活着的就把旧的叫回来，**绝不开第二个**。
//
// 单实例的意义就在这儿：从程序坞再点一下图标弹出两个窗口，两个窗口各放一首
// 歌、进度条各走各的，是最难解释的一种错。
func (s *Shell) openWindow() {
	if s.win != nil {
		if s.win.IsDestroyed() {
			s.win = nil
		} else {
			platform.RestoreFromBackground(s.win)
			return
		}
	}
	s.Attach(mygo.NewWindow(WindowOptions(s.View)))
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
