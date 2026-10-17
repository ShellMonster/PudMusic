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
	"github.com/ShellMonster/Biu_Go/internal/media"
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

	// laterPage / historyPage 是「稍后再看」和「历史记录」这两页各自的状态
	// （见 librarypage.go）。挂在壳上而不是页面的局部变量，理由同上。
	laterPage   libraryPage
	historyPage libraryPage
	// searchWatchStop 是那个 500ms 关键词防抖的钟。醒着的时候只有一个，
	// 两页共用——用户一次只能在一页上敲字。
	searchWatchStop func()

	// search 是搜索页的状态：三个 Tab 各一份分页列表、各一套排序、各记着各自
	// 滚到哪儿（见 searchresults.go）。
	//
	// 同样是因为即时模式：这些都得跨帧活着，而局部变量下一帧就没了。
	search searchState

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

	// local 是本地音乐那一页自己的一摊状态：表格 / 列表 / 网格三档的控件、
	// 筛选条件、两道确认弹窗、以及**逐窗口**的唤醒句柄表。见 localmusic.go。
	//
	// ⚠️ 建在 Shell 上而不是每帧新建：`ui.ListState` 记着滚动位置和选中行，
	// `widgets/list` 的两个控件各记各的，新建等于每帧回到顶上。
	local *localPage

	// home 是首页自己那一摊状态：三个 Tab 各自的列表、新歌速递那一块、切在哪个
	// Tab。见 homepage.go。
	//
	// ⚠️ 建在 Shell 上而不是每帧新建：三个 `list.List` 各自记着自己的滚动位置，
	// 新建一个等于每帧回到顶上，「切 Tab 回到原位」这条就永远不成立。
	home *homePage

	// downloads 是下载记录页自己那一摊状态：那张表格、删除确认弹窗。见
	// downloadlist.go。
	//
	// 同样是建一次存着：表格记着滚动位置和行身份，每帧新建等于每帧跳回顶上。
	downloads *downloadsPage

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

	// 播放列表抽屉这一摊。**同样只由 view 在主线程上读写**。
	//
	// drawerOpen 是开没开。它不进设置：抽屉是「我现在要看队列」这一下的事，
	// 关掉窗口就该是关着的（Biu 那边的 modal store 也不持久化它）。
	drawerOpen bool

	// queueList 是抽屉里那一列的列表状态：滚动位置、行身份、行高。
	queueList ui.ListState

	// queueScroll 是那一列滚到哪儿了。**和 queueList 分开存**是因为「定位当前
	// 播放」要的是一段动画：ListState.ScrollTo 是一跳到位（`mygo/ui/list.go`
	// 的 ScrollTo），ScrollState.ScrollTo 才带 Animation。
	queueScroll ui.ScrollState

	// queueRows 是这一帧去重之后要画的那几行（连同它们在队列里的下标）。
	// queueList.Key 每帧都要读它，所以它和 rows 数得对上（见 queue_drawer.go）。
	queueRows []queueRow

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

	// 歌词这一摊。**只由 view 读写**（view 永远跑在主线程上），不跨 goroutine。
	//
	// lyricScroll 是歌词区的滚动位置和那一次「平滑滚到中间」的弹簧都在这里
	// （`ui.ScrollState`，见 lyrics.go）。
	lyricScroll ui.ScrollState
	// lyricKey 是这一帧画的是哪一首歌的歌词。换歌时它变了，滚动位置整个换掉
	// （新歌上留着上一首的滚动量，歌词一上来就停在歌的中间）。
	lyricKey string
	// lyricActive 是上一次**真的发出滚动指令**对应的那一行，-1 是还没有过。
	lyricActive int
	// lyricSeen 是这一帧认出的正在唱的那一行。和 lyricActive 分开是因为「目标
	// 够不着」那条路不会更新 lyricActive，拿它判「换行了」会让那道重试预算失效
	// ——见 followActiveLyric 里那一段。
	lyricSeen int
	// lyricPadH 是上一次跟过去时的居中留白。它变了（换歌、改字号、改窗口宽度）
	// 就得重新跟——上游那一句的依赖表就是 `[activeIndex, centerPadding]`
	// （`lyrics/index.tsx:291`）。
	lyricPadH float32
	// lyricExpect 是上一次发出去的滚动目标。滚动量停在别的地方就说明有人抢过去了
	// ——见 followActiveLyric 顶上那段。
	lyricExpect float32
	// lyricHeld 是「用户拿滚轮翻过去了，先认了」，唱到下一句才跟回去。
	lyricHeld bool
	// lyricStuck 是「连着几帧目标都够不着」的次数，用完就不再要帧了。
	lyricStuck int
	// lyricPadForced 让测试把居中留白强行按成某一个值（nil = 用真值）。
	// 见 internal/ui/lyrics.go 里 lyricPadForced 那段——它为什么必要。
	lyricPadForced *float32
	// lyricMaxY 是上一次看到的滚动范围。它变了（内容变高）就重新从头试一遍。
	lyricMaxY float32
	// lyricActiveEl 是正在唱的那一行的元素句柄，每一帧重记。它和 lyricBox
	// 一起用来算「滚到哪儿正好居中」——MyGo 的 `Bounds()` 给的是上一帧的框，
	// 所以这一帧量到的下一帧才用得上。
	lyricActiveEl ui.Element
	lyricBox      ui.Element
	// lyricBoxX/Y/W/H 是上一帧量到的歌词区那个框，lyricLineH 是上一帧
	// 量到的那一行的高度。前两个用来算居中留白（见 lyricPad），**也留给测试**：
	// `ui.Element` 的句柄一帧之后就作废了，测试没法自己去问框架要框在哪。
	lyricBoxX, lyricBoxY, lyricBoxW, lyricBoxH float32
	lyricLineH                                 float32

	// keys 是快捷键。和 hist 一样是单独一份文件（见 shortcuts.go）。没挂上盘时
	// 是内存里的一份：设置页照常能录，只是关掉之后不记得。
	keys *shortcutKeys
	// recording 是设置页上**正在录**的那一格，零值表示没有在录。
	//
	// 🔴 它同时是「焦点在哪一格」：`FocusBind` 把两者绑成同一件事，所以这一格
	// 必须可比较（见 shortcutSlot）。别在这里加指针或者切片——加了会让
	// FocusBind 直接 panic，而那条 panic 说的是「需要可比较的值」，和真因隔得
	// 十万八千里。
	recording shortcutSlot
	// rejected 是「刚才按的那个键不能录成快捷键」的那一格，描红用。按下一个合法
	// 的键或者按 Esc 就清掉——红色停在屏幕上不动的话，用户会以为那里出了什么
	// 错，而它其实只是「刚才那一下没录上」。
	rejected shortcutSlot

	// settings 是设置页自己那一摊：切在哪个 Tab。建在 Shell 上而不是每帧新建，
	// 理由和 home 一模一样：切走再切回来要停在原来那一格上。
	settings settingsPageState

	// picker 弹系统文件框挑路径（下载目录、FFmpeg）。做成字段是为了能替掉它：
	// 真的那一份要过事件循环，无头 Tester 里没有事件循环，不替掉就只能等上真
	// 系统才验得到「选回来写进设置」这条链。和 autostart 同一个道理。
	picker pathPicker

	// crash 是上一帧 panic 到的东西，非 nil 时这一帧画错误页。
	crash any

	// upSpaces 存着每个 UP 的空间页状态，键是 mid（见 upspace.go）。
	//
	// **只由 view 在主线程上读写**：它是一个普通的 map，没有锁——
	// 界面状态是主线程单点写入的（铁律一），后台那一条线只往 upSpace.box
	// 那个**带锁的信箱**里投，不碰这张表。
	upSpaces map[int64]*upSpace

	// feedPage 是「用户动态」那一页的状态（见 dynamicfeed.go）。只有一个，
	// 因为这一页没有参数。
	feedPage *dynamicFeed

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

	// media 是系统媒体会话，由 main 那边 MountMedia 递过来（见 media_mount.go）。
	//
	// ⚠️ 可以是 nil：那一次 `media.New` 没装上（`Available()` 是 false）。
	// bindMedia 判的就是这个——挂不上系统会话不该拦住开窗口，而 bindMedia
	// 在 `ensureWindow` 里，那个位置**不能**因为一个加分项开天窗。
	media *media.Session
}

// bindMedia 把系统媒体会话接到这个窗口上。
//
// **只有 Windows 那一边看它**：那边的媒体会话是按窗口算的（`GetForWindow`），
// 任务栏缩略图上的按钮更是直接长在某个 HWND 上，而窗口要到这里才有句柄。
// macOS 那边是全局的，绑定是一次空操作——所以它不能在 MountMedia 里做
// （那时候窗口还不存在）。
//
// 重复调安全：`Session.BindWindow` 会先把上一个窗口收干净。
func (s *Shell) bindMedia(w *mygo.Window) {
	if s.media == nil || w == nil {
		return
	}
	// NativeHandle 在 macOS 上是 NSWindow*、Linux 上是 GtkWindow*，
	// 那边 bindWindow 原样忽略（见 media.windowBinder）。
	s.media.BindWindow(w.NativeHandle())
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
	// 下载记录页同理（downloadlist.go）：两个「打开」接系统那一份，它们在无头
	// Tester 里会 panic，测试建完外壳就换成记动作的桩；记录那一条接应用层。
	s.downloads = newDownloadsPage(a.DownloadSnapshots, systemOpenDir, mygo.Shell.ShowItemInFolder)
	// 本地音乐页同理。⚠️ 传进去的唤醒是 `s.local.invalidate` 而不是
	// `s.wake.invalidate`：后台扫完了要把**所有画过这一页的窗口**都叫醒，
	// 只叫醒这一个外壳的窗口的话，另一个停在旧帧（铁律三）。
	s.local = newLocalPage(a, s.images, list.AppActions(a, s.addToast),
		s.addToast, s.pickDir)
	s.hist = newSearchHistory("")
	s.keys = newShortcutKeys("")
	s.picker = systemPathPicker{}
	s.feed = newFeedPoller(dynamicFeedInterval, nil)
	s.buildLibraryPages()
	// 搜索页那三个 Tab 各建一份分页列表。建在这里而不是第一次画到搜索页时：
	// 翻页列表要在**第一帧之前**就建好（它的滚动位置、翻页状态全在里头），
	// 等到画到那一页再建就是每进一次搜索页从头滚一次。
	s.search.init(s)
	// pageBody 是画页面内容的那一支：404 兜底 + 已经有内容的页（设置页）+ 每条
	// 路由自己的空状态，三样拼在 pageContent 里（router.go）。
	// sideBody 是侧栏那一支，搬去 sidebar.go 之后从那儿接进来。
	// navBody 是顶栏那一支，搬去 navbar.go 之后从那儿接进来。
	s.pageBody = s.pageContent
	s.sideBody = s.viewSide
	s.navBody = s.viewNav
	s.draw = Guard(s.viewMain, s.onPanic)
	// 本地音乐这一摊建好之后才能装唤醒回调（`SetLocalWake` 要一个能叫醒
	// **所有**画过这一页的窗口的函数，而那正是 `s.local.invalidate`）。
	a.SetLocalWake(s.local.invalidate)
	return s
}

// pickDir 是本地音乐页「添加目录」要用的那一次系统对话框。
//
// 界面上没有它就进不去任何一个目录，而这一页的第一句空状态就是「点右上角
// 「添加目录」选一个文件夹」——那句话和这个函数是同一件事的两半。
//
// ⚠️ 返回的路径**不保证**是绝对路径（macOS 的对话框有时给相对路径），
// `app.SetLocalRoots` 那边会自己补成绝对路径。
func (s *Shell) pickDir() (string, error) {
	return platform.PickDirectory(s.win)
}

// viewLocalMusic 是本地音乐页。
//
// 页面级的 recover 在 `Shell.page` 上（和其余 14 条路由共用那道 Guard），
// 这一页自己的 view 另外还包了一层（`localPage` 构造时包，见 localmusic.go）。
func (s *Shell) viewLocalMusic(c *ui.Context, _ *ui.Route) {
	s.local.View(c)
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
		// 快捷键同理。
		if err := s.keys.Close(); err != nil {
			log.Printf("biu: 关窗时保存快捷键失败：%v", err)
		}
	})
}

// Run 开出一个窗口并跑起来。窗口骨架归界面层管：以后要加迷你播放器、
// 要改标题栏、要调窗口尺寸，都在这一处。
//
// sess 是 `MountMedia` 递过来的那个系统媒体会话，可以是 nil（那一轮没装上，
// 或者调用方不想要）。**它得在这一层而不是在 MountMedia 里绑窗口**：
// Windows 的媒体会话是按窗口算的，而窗口在这里才建出来——见 bindMedia。
func Run(a *app.App, sess *media.Session) error {
	// 单实例锁必须在 Run 之前拿：Run 之后事件循环才起来，那时候两个实例
	// 同时抢就是竞态，谁抢到全看调度。抢不到的那个，框架已经把它的命令行
	// 转交给第一个实例了（`platform.Lock`），自己立刻退出——**不能留在后台**
	// 当一个没有窗口的僵尸进程。
	if !platform.Lock() {
		return nil
	}

	s := New(a)
	s.media = sess
	// 搜索历史读盘是阻塞 IO，放在建窗口之前：早读进来早能用，也省得第一帧
	// 是在一个空历史上画出来的（那也是 mountSearchHistory 的用途之一）。
	s.mountSearchHistory()
	s.mountShortcuts()
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
		// 本地音乐目录的文件监听。⚠️ 必须在 `WhenReady` 里：`mygo.WatchFiles`
		// 在应用还没 ready 的时候调会 panic（框架的 `needsApp`，
		// `mygo/app.go:128`），而 `Run` 之前是那一刻。
		a.StartLocalMusicWatch()
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

// mountShortcuts 把快捷键挂到盘上。和搜索历史同一套规矩：拿不到应用数据目录不算
// 错误——记一条日志，接着用内存里那一份，设置页照常能录，只是关掉之后不记得。
func (s *Shell) mountShortcuts() {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("biu: 拿不到应用数据目录，快捷键这次关掉就没了：%v", err)
		return
	}
	if err := s.keys.Close(); err != nil {
		log.Printf("biu: 收尾内存里的快捷键失败：%v", err)
	}
	s.keys = newShortcutKeys(shortcutPath(dir))
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

// viewMain 是正常的那一帧：上导航 + 左边栏 + 路由页 + 播放条，浮层上是播放列表
// 抽屉（开着的话）和右下角的提示。
func (s *Shell) viewMain(c *ui.Context) {
	// 🔴 铁律二：每帧第一件事就是装主题，而且必须在最开头——SetTheme 只影响
	// 它之后创建的控件。不装的话 MyGo 在帧开头重置好的那份跟随系统的主题
	// 就是这一帧真正用的那份，用户选了深色也只在这一帧是对的。
	s.applyFrameTheme(c)
	s.reportSettingsError()
	// 收「那个 500ms 防抖的钟要不要醒着」。**每帧一次**，所以用户切走时它一定
	// 会跟着停（见 syncSearchWatch）。
	s.syncSearchWatch()

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

	// 抽屉是浮在侧栏和页面**上面**的一层，不是主布局里的另一块。Biu 那边它是
	// 右侧浮出来的 Drawer（`music-playlist-drawer/index.tsx` 的
	// `data-[placement=right]`），底下那一层页面照旧看得见、点得着；塞进上面
	// 那个 Row 里就成了「窗口窄一块」，页面被挤着走——那不是抽屉。
	Guard(s.viewQueueDrawer, s.onPanic)(c)
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
