// Package ui 是界面层：整个项目里只有它 import MyGo 的 ui 包。
//
// 本文件里的 `ui.Xxx` 一律指 MyGo 的 ui 包，不是本包——包内引用自己不需要写包名。
package ui

import (
	"log"
	"runtime/debug"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/app"
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

	// sideBody 是画侧栏的那一支，同样是为了能替掉它测崩溃。
	//
	// 侧栏比页面更需要这一层：它里面有拖拽、宽度动画和收藏夹列表，出事时
	// 坏掉的是**整个窗口**（侧栏在外层布局里，一空就空一大块），而且出错
	// 的地方往往不在页面上。
	sideBody func(*ui.Context)

	// navBody 是画顶栏的那一支，同样是为了能替掉它测崩溃。
	//
	// 顶栏也需要这一层：它里面有搜索框、建议浮层、原生菜单和轮询，出事时坏的
	// 是**整条顶栏**——用户连窗口都拖不动了，而且报错的地方不在页面上。
	navBody func(*ui.Context)

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

	// 侧栏这一摊。**只由 view 在主线程上读写**，不跨 goroutine。
	//
	// sideWidth 是这一帧画出来的宽度：拖动中它跟着指针走（可能小于 160，
	// 看上去就是折叠着的），没在拖时宽度走 MyGo 的动画，见 sidebar.go。
	//
	// sideDragFromX / sideDragFromWidth 是这次拖拽的起点：按下那一刻指针的
	// 横坐标，和当时的宽度。每一次拖拽重新记，不跨拖拽沿用。
	sideWidth         float64
	sideDragging      bool
	sideDragFromX     float32
	sideDragFromWidth float64

	// sideLists 是两组收藏夹各自的列表状态。拖拽排序靠它，
	// 两组一份是因为两组各排各的（见 sidebar_favorites.go）。
	sideLists [favGroups]ui.ListState

	// 顶栏这一摊。**同样只由 view 在主线程上读写**。
	//
	// searchFocused 是搜索框有没有焦点，顶栏用它染边框。浮层**不**靠它开
	// （见 navbar_search.go 里那一段：焦点被 MyGo 还回来是个假信号）。
	searchQuery   string
	searchSuggest bool
	searchFocused bool
	// user 是头像下拉认的那个人。这一版还没有登录，所以恒为零值。
	user navUser
	// feed 是「动态更新红点」的 5 分钟节奏。假时钟在 navbar_feed.go。
	feed *feedPoller
	// hist 是搜索历史。没挂上盘时是内存里的一份（见 searchhistory.go）。
	hist *searchHistory

	// crash 是上一帧 panic 到的东西，非 nil 时这一帧画错误页。
	crash any
}

// New 建一个窗口外壳，绑在状态 a 上。
//
// ⚠️ 封面缓存的唤醒在这里装，不在 Attach 里：注册的是这个外壳自己的唤醒句柄，
// 跟窗口没有关系（wakeHandle 内部只存 Context 给的服务句柄，窗口关了自动失效）。
// 放在 Attach 的后果是：那条注册只有真能建窗口时才发生，无头 Tester 永远碰不到，
// 「建了外壳却忘了注册唤醒 → 图下完之后界面停在占位」这个 bug 就没有任何测试
// 钉得住（实测：只投递不唤醒 = 0 帧刷新）。**每个窗口一个外壳，也就注册一次。**
//
// TestPlayBarCoverWakeRedrawsTheWindow 走的就是这一行。
func New(a *app.App) *Shell {
	s := &Shell{app: a, router: ui.NewRouter("/"), images: imgcache.New(imgcache.Options{})}
	s.images.AddWake(s.wake.invalidate)
	s.hist = newSearchHistory("")
	s.feed = newFeedPoller(dynamicFeedInterval, nil)
	s.pageBody = s.pageContent
	s.sideBody = s.viewSide
	s.navBody = s.viewNav
	s.draw = Guard(s.viewMain, s.onPanic)
	return s
}

// Attach 把外壳接到窗口上：装唤醒回调，窗口关掉时收尾。
func (s *Shell) Attach(w *mygo.Window) {
	s.win = w
	s.app.SetWake(s.wake.invalidate)
	w.OnClosed(func() {
		s.images.Close()
		s.app.Close()
		// 关窗就收尾设置：停掉后台写盘，并把还欠着的改动同步落完。
		//
		// MountSettings 里那个 OnWillQuit 的 Flush 兜的是「退进程」这条路；
		// 这一句兜的是「关窗口」——以后加了托盘（issue #28），关窗口不再等于
		// 退进程，那条 OnWillQuit 就不一定会跑，两句要一起看。
		//
		// `CloseSettings` 可以重复调（内部先 LoadAndDelete），所以窗口关了之后
		// 进程再退、再调一次都不会出错。
		if err := s.app.CloseSettings(); err != nil {
			log.Printf("biu: 关窗时保存设置失败：%v", err)
		}
		// 搜索历史同理：写盘是后台合并的，最后一下有可能还等在退避窗口里。
		if err := s.hist.Close(); err != nil {
			log.Printf("biu: 关窗时保存搜索历史失败：%v", err)
		}
	})
}

// Run 开出一个窗口并跑起来。窗口骨架归界面层管：以后要加迷你播放器、
// 要改标题栏、要调窗口尺寸，都在这一处。
func Run(a *app.App) error {
	mygo.App.WhenReady(func() {
		s := New(a)
		s.mountSearchHistory()
		w := mygo.NewWindow(WindowOptions(s.View))
		// 装唤醒回调、接窗口关闭。必须在建窗口之后：外壳要拿得到窗口句柄。
		s.Attach(w)
	})
	return mygo.App.Run()
}

// mountSearchHistory 把搜索历史挂到盘上。
//
// 拿不到应用数据目录不算错误——记一条日志，接着用内存里那一份。搜索照常能用，
// 只是关掉之后不记得搜过什么。为一件可有可无的东西挡住开窗口不值当。
func (s *Shell) mountSearchHistory() {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("biu: 拿不到应用数据目录，搜索历史这次关掉就没了：%v", err)
		return
	}
	if err := s.hist.Close(); err != nil {
		log.Printf("biu: 收尾内存里的搜索历史失败：%v", err)
	}
	s.hist = newSearchHistory(searchHistoryPath(dir))
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
		// 顶栏同样外面包一层 Guard（铁律三）：它坏了坏掉的是整条顶栏，
		// 用户连窗口都拖不动了，而出错的地方不在页面上。
		Guard(s.navBody, s.onPanic)(c)
		ui.Row(c).Grow(1).Children(func() {
			// 侧栏是**一整块** view，外面包一层 Guard（铁律三）。它坏了的
			// 表现是整个窗口空一大块，连报错的地方都没有。
			Guard(s.sideBody, s.onPanic)(c)
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

// enqueuePicked 把文件框选回来的每一条路径都排进队列。
//
// 一条都不能少：少了的那几条在界面上看不出任何异样，用户只会以为是自己没选上。
//
// 为什么单独拎出来：文件框要系统面板，测试里调不了；路径进队列这一步是纯逻辑，
// 拎出来才测得到（见 shell_pick_test.go）。弹框本身只有一个调用方
// （pickAndEnqueue），在这里没有第二条路。
func (s *Shell) enqueuePicked(paths []string) {
	items := make([]app.Item, len(paths))
	for i, p := range paths {
		items[i] = app.LocalItem(p)
	}
	s.app.Enqueue(items...)
}
