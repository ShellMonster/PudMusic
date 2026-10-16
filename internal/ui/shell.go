// Package ui 是界面层：整个项目里只有它 import MyGo 的 ui 包。
//
// 本文件里的 `ui.Xxx` 一律指 MyGo 的 ui 包，不是本包——包内引用自己不需要写包名。
package ui

import (
	"log"
	"runtime/debug"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Shell 是一个窗口的全部东西：状态、画法、唤醒句柄。
type Shell struct {
	app *app.App
	win *mygo.Window

	// draw 是外面包了一层 recover 的 viewMain。
	draw func(*ui.Context)

	// wake 是这个窗口的唤醒句柄，由 view 每帧重新登记。
	wake wakeHandle

	// router 是 Biu 那 14 条路由的调度，带前进/后退历史。
	router *ui.Router

	// pageBody 是画页面内容的那一支。单独拎出来是为了能替掉它测崩溃
	// （铁律四的验收点：页面里 panic，进程不能崩）。
	pageBody func(*ui.Context, *ui.Route, Route)

	// images 是网络封面缓存。整个窗口共用一个。
	images *imgcache.Cache

	// toasts 是右下角那些提示，addToast / clearToasts 是唯一改它的地方。
	toastMu  sync.Mutex
	toasts   []toast
	toastSeq uint64

	// scrub 是进度条的值（0..1），held 是指针有没有按在它上面。
	// 两个都只由 view 读写——view 永远跑在主线程上。
	scrub float64
	held  bool

	// crash 是上一帧 panic 到的东西，非 nil 时这一帧画错误页。
	crash any
}

// New 建一个窗口外壳，绑在状态 a 上。
func New(a *app.App) *Shell {
	s := &Shell{app: a, router: ui.NewRouter("/"), images: imgcache.New(imgcache.Options{})}
	s.pageBody = s.placeholder
	s.draw = Guard(s.viewMain, s.onPanic)
	return s
}

// Attach 把外壳接到窗口上：装唤醒回调，窗口关掉时收尾。
func (s *Shell) Attach(w *mygo.Window) {
	s.win = w
	s.app.SetWake(s.wake.invalidate)
	// 封面下完 / 解完要叫醒这个窗口。**每个窗口注册一次**，只通知主窗口的话
	// 迷你窗口会一直停在占位图上（实测）。
	s.images.AddWake(s.wake.invalidate)
	w.OnClosed(func() {
		s.images.Close()
		s.app.Close()
	})
}

// Run 开出一个窗口并跑起来。窗口骨架归界面层管：以后要加迷你播放器、
// 要改标题栏、要调窗口尺寸，都在这一处。
func Run(a *app.App) error {
	mygo.App.WhenReady(func() {
		s := New(a)
		w := mygo.NewWindow(WindowOptions(s.View))
		// 装唤醒回调、接窗口关闭。必须在建窗口之后：外壳要拿得到窗口句柄。
		s.Attach(w)
	})
	return mygo.App.Run()
}

// WindowOptions 是主窗口的样子。view 是这个窗口每帧要画的界面。
//
// ⚠️ macOS 上要的是**保留红绿灯的隐藏标题栏**，不是 `Frameless: true`——
// `Frameless` 会把系统控件一起删掉，红黄绿就没了。Biu 是无边框、标题栏自
// 绘、系统控件留在上面，MyGo 的 `TitleBarHidden` 说的就是同一件事
// （`window.go` 里 `hiddenTitleBar = !Frameless && (TitleBarHidden || …)`）。
// `TitleBarHiddenInset` 只是把控件再往里缩一点，Biu 没做这个差别。
func WindowOptions(view func(*ui.Context)) mygo.WindowOptions {
	return mygo.WindowOptions{
		Title:     "Biu MyGo",
		Width:     1200,
		Height:    800,
		MinWidth:  900,
		MinHeight: 560,
		// 整条顶栏可拖窗口，控件叠在它上面。
		TitleBarStyle:        mygo.TitleBarHidden,
		TrafficLightPosition: &mygo.Point{X: 14, Y: 18},
		// 窗口停在用户上次关掉它的地方。
		StateKey: "main",
		// 窗口里是 MyGo 自己画的界面，不是网页。
		Content: ui.View(view),
	}
}

// Guard 给一个 view 包一层页面级的 panic recover。
//
// 这是整个界面层最重要的一条约定：MyGo 的 Dispatch（app.go:576）和 drain
// 都没有 recover，view 里 panic 会穿透 Cocoa 事件循环**直接杀死进程**，连
// 「这一页坏了」的机会都没有。Biu 原来靠 React 的 ErrorBoundary 兜住，
// 这里用 recover 兜住同一个位置的效果。
//
// onPanic 拿到崩掉的东西，界面用它换一张错误页。崩了以后这一帧已经建了一半，
// MyGo 的 build pass 会在下一次 pass 从头重建，所以 onPanic 里只要记下状态
// 并要一帧，画面下一帧就是完整的。
func Guard(draw func(*ui.Context), onPanic func(any)) func(*ui.Context) {
	return func(c *ui.Context) {
		defer func() {
			if r := recover(); r != nil {
				onPanic(r)
			}
		}()
		draw(c)
	}
}

// View 画一帧，MyGo 在需要新画面的时候调它。
func (s *Shell) View(c *ui.Context) {
	// 每帧重新登记：句柄内部是窗口的弱引用，窗口关了自动失效，关掉重开也不用管。
	s.wake.set(c)

	if s.crash != nil {
		s.viewCrash(c)
		return
	}
	s.draw(c)
}

// onPanic 记下崩掉的东西并要一帧，下一帧画错误页。
//
// ⚠️ 这里补的 Invalidate 不是多余的：recover 是在 build pass **中间**接住的，
// 那一帧剩下的部分没建，画出来是半张。要下一帧才完整（实测，文档没写）。
func (s *Shell) onPanic(r any) {
	s.crash = r
	log.Printf("biu: 界面崩溃，已切到错误页\n%v\n%s", r, debug.Stack())
	s.wake.invalidate()
}

// viewMain 是正常的那一帧：上导航 + 左边栏 + 路由页 + 播放条，右下角浮着提示。
func (s *Shell) viewMain(c *ui.Context) {
	// 🔴 铁律二：每帧第一件事就是装主题，而且必须在最开头——SetTheme 只影响
	// 它之后创建的控件。不装的话 MyGo 在帧开头重置好的那份跟随系统的主题
	// 就是这一帧真正用的那份，用户选了深色也只在这一帧是对的。
	s.applyFrameTheme(c)
	s.reportSettingsError()

	st := s.app.Snapshot()

	ui.Column(c).Fill().Children(func() {
		s.viewNav(c)
		ui.Row(c).Grow(1).Children(func() {
			s.viewSide(c)
			s.viewRoutes(c)
		})
		s.viewPlayBar(c, st)
	})

	s.viewToasts(c)
}

// viewCrash 是崩溃后那一帧。Biu 原来的 ErrorBoundary 也是这么做的：程序活着，
// 出事的那块换一张说明页。
func (s *Shell) viewCrash(c *ui.Context) {
	// 错误页也是一帧。不装主题的话它会用系统色画出来：用户选深色时崩了，
	// 看到的却是一张亮色的错误页，像换了个应用。
	s.applyFrameTheme(c)

	t := c.Theme()
	ui.Column(c).Fill().Center().Gap(10).Padding(24).Children(func() {
		ui.Text(c, "界面出错了，播放器已经停在这一页").FontSize(18).Bold()
		ui.Textf(c, "%v", s.crash).FontSize(12).TextColor(t.TextMuted)
		ui.Text(c, "细节已经写进日志。点下面的按钮再试一次。").FontSize(12).TextColor(t.TextMuted)
		ui.Row(c).Gap(10).Justify(ui.Center).Children(func() {
			if ui.PrimaryButton(c, "重新载入").Clicked() {
				s.crash = nil
			}
			// 崩的是某一页，不一定是首页。得给一条走得出去的路，
			// 不然错误页里连侧栏都点不到，只能干看着。
			if ui.Button(c, "回到首页").Clicked() {
				s.navigate("/")
			}
		})
	})
}

// pickFile 弹系统文件框。
//
// 对话框在另一个 goroutine 里开：MyGo 把弹框请求 post 到主线程再等系统回调，
// 在主线程里等就是死等，事件循环会卡死。
func (s *Shell) pickFile() {
	win := s.win
	go func() {
		paths, err := platform.PickAudioFile(win)
		if err != nil || len(paths) == 0 {
			return
		}
		s.app.Open(paths[0])
	}()
}
