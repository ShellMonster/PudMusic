// Package ui 是界面层：整个项目里只有它 import MyGo 的 ui 包。
//
// 本文件里的 `ui.Xxx` 一律指 MyGo 的 ui 包，不是本包——包内引用自己不需要写包名。
package ui

import (
	"log"
	"runtime/debug"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/guard"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
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

	// clock 是界面层等东西用的时钟（clock.go）。测试会换成假的，于是「404 页
	// 3 秒后回首页」不用真睡 3 秒。
	clock clockSource

	// shownPath 是上一帧画的是哪一页，syncRoute 拿它认出「换页了」。
	shownPath string

	// searchDraft 是搜索框里正在敲的字。放在壳上而不是页面的局部变量：即时模式
	// 每一帧都重画一遍，局部变量下一帧就没了（只有 view 里显式声明并被元素
	// 认下的状态才留得住）。
	searchDraft string

	// notFoundAt 是进 404 的那一刻，notFoundStop 是那个每秒一跳的倒计时。
	// 两者都只由主线程读写（syncRoute 在主线程），后台那一跳只调
	// wakeHandle.invalidate，所以不违反铁律一、也没有竞态。
	notFoundAt   time.Time
	notFoundStop func()

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

	// home 是首页自己那一摊状态：三个 Tab 各自的列表、新歌速递那一块、切在哪个
	// Tab。见 homepage.go。
	//
	// ⚠️ 建在 Shell 上而不是每帧新建：三个 `list.List` 各自记着自己的滚动位置，
	// 新建一个等于每帧回到顶上，「切 Tab 回到原位」这条就永远不成立。
	home *homePage

	// toasts 是右下角那些提示，addToast / clearToasts 是唯一改它的地方。
	toastMu  sync.Mutex
	toasts   []toast
	toastSeq uint64

	// scrub 是进度条的值（0..1），held 是指针有没有按在它上面。
	// 两个都只由 view 读写——view 永远跑在主线程上。
	scrub float64
	held  bool

	// rateOpen 是倍率那个浮层开着没有。**只由 view 在主线程上读写**，
	// 见 playbar_rate.go。
	rateOpen bool

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
	//
	// ⚠️ 叫 searchKeyword 不叫 searchQuery：包级已经有一个 `searchQuery` 了
	//（searchpage.go 的「URL 里搜索词那个键」，值是 "q"），两样东西没有关系，
	// 同名却不同义最容易读错。
	searchKeyword string
	searchSuggest bool
	searchFocused bool
	// feed 是「动态更新红点」的 5 分钟节奏。假时钟在 navbar_feed.go。
	feed *feedPoller
	// hist 是搜索历史。没挂上盘时是内存里的一份（见 searchhistory.go）。
	hist *searchHistory

	// crash 是上一帧 panic 到的东西，非 nil 时这一帧画错误页。
	crash any

	// qrBitmap 是当前那张二维码的位图，qrFor 是它对应的那张二维码。
	//
	// **只由 view 读写**（view 永远跑在主线程上），所以没有锁。
	// 键是指针：换了一张码就换一个，自动重建那张 GPU 纹理。
	// 见 login.go 的 qrImage。
	qrBitmap    *ui.Bitmap
	qrDimBitmap *ui.Bitmap
	qrFor       *bili.Code

	// quitting 是「已经进了退出流程」，只由 OnBeforeQuit 置位（主线程）。
	//
	// 存在的理由是退出流程本身要靠关窗口走完：框架的 prepareQuit 遍历窗口
	// 挨个 close。所以「关掉窗口时怎么办」这个设置在退出途中必须让路，否则
	// ⌘Q 会变成「退出不了」。见 planWindowClose。
	quitting bool

	// autostart 是「开机自启在系统上的那一份」，见 lifecycle.go 里的说明。
	autostart autoStartService

	// closeOption 是最近一次从设置里读到的「关闭窗口时怎么办」。
	//
	// ⚠️ 缓存是因为关窗口时的先后顺序：OnClosed 里会调 CloseSettings，那会把
	// 设置的挂载摘掉；之后 `SettingsSnapshot()` 返回的是 DefaultSettings("")
	// 而不是用户的设置（默认 hide）。而「窗口全关」那个回调恰好跑在 OnClosed
	// 之后，于是现去问设置只会读到默认值，选了「直接退出」的用户点了关闭进程
	// 却不退出。缓存绕开这个顺序，见 installLifecycle 里的注释。
	closeOption app.CloseWindowOption

	// tray 是通知区域的图标（**只在 Windows / Linux 上有**，macOS 不建托盘，
	// 见 platform.TraySupported）。nil 表示这个平台没有托盘，或者建失败了。
	//
	// 句柄留着是为了不让它被回收：托盘图标的生命周期挂在它上面，GC 收走之后
	// 通知区域里那一个格子就没了，而界面这边看不出任何异样（见 tray.go）。
	tray *mygo.Tray
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
	s := &Shell{
		app:       a,
		router:    ui.NewRouter("/"),
		clock:     realClock{},
		images:    imgcache.New(imgcache.Options{}),
		autostart: systemAutoStart{},
	}
	s.images.AddWake(s.wake.invalidate)
	// 首页自己那一摊要在建好之后才有（它要用封面缓存），所以接在 images 后面。
	// 操作接在应用状态上、提示接自己的 Toast，接法见 widgets/list 的 AppActions。
	s.home = newHomePage(s.images, list.AppActions(a, s.addToast), s.wake.invalidate)
	s.hist = newSearchHistory("")
	s.feed = newFeedPoller(dynamicFeedInterval, nil)
	// pageBody 是画页面内容的那一支：404 兜底 + 已经有内容的页（设置页）+ 每条
	// 路由自己的空状态，三样拼在 pageContent 里（router.go）。
	// sideBody 是侧栏那一支，搬去 sidebar.go 之后从那儿接进来。
	// navBody 是顶栏那一支，搬去 navbar.go 之后从那儿接进来。
	s.pageBody = s.pageContent
	s.sideBody = s.viewSide
	s.navBody = s.viewNav
	s.draw = Guard(s.viewMain, s.onPanic)
	return s
}

// Attach 把外壳接到窗口上：装唤醒回调，装关闭策略，窗口真关掉时收尾。
//
// 一个窗口上的生命周期回调全在这儿，以后新增的（显示/隐藏、拖拽文件…）也挂这里：
// 分成两处的话，总有一个回调会在换窗口的时候漏装，而漏装之后的症状
// （「窗口没关干净」「图下完了界面不动」）都跟漏装的那一行毫无关系，极难查。
func (s *Shell) Attach(w *mygo.Window) {
	s.win = w
	s.app.SetWake(s.wake.invalidate)

	w.OnClose(func(e *mygo.CloseEvent) {
		opt := s.app.SettingsSnapshot().CloseWindowOption
		// 记一份：OnClosed 会把设置的挂载摘掉，之后再问就只剩默认值了（见
		// installLifecycle 里 OnWindowAllClosed 那段的注释）。
		s.closeOption = opt
		if planWindowClose(s.quitting, opt) != closeToBackground {
			return
		}
		e.PreventDefault()
		platform.HideToBackground(w)
	})

	// ⚠️ 下面这条是**真的关掉窗口**才跑的收尾：关封面缓存、停播放、关 App。
	// 「收起来」被上面拦在 OnClose 里了，压根到不了这里——所以关窗口时选
	// 「隐藏」，播放不会停、应用不会死。这是两条不同的路，别混。
	w.OnClosed(func() {
		if s.win == w {
			s.win = nil
		}
		// 收掉红点那个每五分钟一跳的定时器（navbar_feed.go 的 arm）。和 404
		// 那个一样：窗口真关了之后 Invalidate 对它是 no-op，留着不致命，但它会
		// 一直醒到进程退出。
		s.feed.disarm()
		// 收掉 404 那个每秒一跳的定时器。窗口真关了之后 `Invalidate` 对它是
		// no-op，留着不致命，但它会一直每秒钟醒一次到进程退出。
		//
		// ⚠️ 放这儿而不是 OnClose：选「隐藏到托盘」时 OnClosed 压根不跑（窗口
		// 只是收起来了，应用还活着），那时候倒计时该继续走——用户把窗口叫回来
		// 时本来就该看到已经回首页的样子，不是还停在一个倒计时的 404 上。
		s.disarmNotFound()
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
	// 单实例锁必须在 Run 之前拿：Run 之后事件循环才起来，那时候两个实例
	// 同时抢就是竞态，谁抢到全看调度。抢不到的那个，框架已经把它的命令行
	// 转交给第一个实例了（`platform.Lock`），自己立刻退出——**不能留在后台**
	// 当一个没有窗口的僵尸进程。
	if !platform.Lock() {
		return nil
	}

	s := New(a)
	// 搜索历史读盘是阻塞 IO，放在建窗口之前：早读进来早能用，也省得第一帧
	// 是在一个空历史上画出来的（那也是 mountSearchHistory 的用途之一）。
	s.mountSearchHistory()
	// 菜单、退出、第二个实例、deep link 全在这里装。必须在 Run 之前，
	// 否则启动命令行里的 biu:// 会在回调装好之前就被交付掉。
	s.installLifecycle()

	mygo.App.WhenReady(func() {
		// scheme 在这里登记而不是 Run 之前：登记要走系统 API，事件循环起来
		// 之后才接得住；而它又必须早于框架那次启动参数的交付——handleReady
		// 把交付排在所有 WhenReady 回调之后（`mygo/app.go:266`），所以正好。
		if err := platform.RegisterScheme(); err != nil {
			// macOS 上没打包成 bundle 就必然登记不上，这是预期的，不是启动失败。
			log.Printf("biu: 登记 %s:// 失败，从别的程序点这个链接不一定唤得起本应用：%v",
				platform.URLScheme, err)
		}
		// 托盘也在这儿建：框架建托盘要过事件循环（NewTray 在没 ready 时 panic）。
		// 托盘要在开窗口**之前**建：用户从托盘点「显示/隐藏」时窗口已经在那里，
		// 顺序反了的话头几秒点托盘是没有反应的。
		s.installTray()
		s.syncAutoStart()
		s.openWindow()
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
//
// 实现本身在 `internal/ui/guard`：控件层（widgets/list）也要给自己的 view 包
// 一层，放这里的话它就得反过来 import 界面层，那是个环。
func Guard(draw func(*ui.Context), onPanic func(any)) func(*ui.Context) {
	return guard.Wrap(draw, onPanic)
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
