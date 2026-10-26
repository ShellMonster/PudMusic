// Package ui 是界面层：整个项目里只有它 import MyGo 的 ui 包。
//
// 本包里的 `ui.Xxx` 一律指 MyGo 的 ui 包，不是本包——包内引用自己不需要写包名。
//
// 这个文件是外壳的**本体**：`Shell` 结构体（它全部的状态字段）、装配它的那几段
// （`New` / `newShellWithImages` / `appName`），外加两个一行的小工具
// （`now` / `visibleFlag`）。
//
// ⚠️ **字段必须待在这一个文件里**：Go 的一个 struct 的字段只能写在同一处，所以
// 这一票只搬**方法**、不搬字段。真要把字段拆成子结构（`s.side` / `s.drawer`…），
// 队列抽屉那一块散在 6 个文件、80 多处的引用就全得跟着改——而那一摊正在被连续
// 改（#380 / #387 / #196 / #220 / #366），现在动它是最糟的时机。
//
// 按职责分开的另外四个文件（**纯搬文件**：方法挪过去，字段和引用一处都没改）：
//
//   - `shell_window.go` —— 窗口与生命周期（`Attach` / `Close` / `Run` / …）
//   - `shell_view.go`   —— 视图管线（`View` / `viewMain` / …）
//   - `shell_crash.go`  —— 崩溃兜底与错误页（`Guard` / `viewCrash` / …）
//   - `shell_mount.go`  —— 挂载与杂项（`mountSearchHistory` / `coverCacheDir` / …）
package ui

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/media"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/rowkey"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// Shell 是一个窗口的全部东西：状态、画法、唤醒句柄。
type Shell struct {
	app *app.App
	win *mygo.Window

	// draw 是外面包了一层 recover 的 viewMain。
	draw func(*ui.Context)

	// wake 是这个窗口的唤醒句柄，由 view 每帧重新登记。
	wake wakeHandle

	// presence 是「用户现在看得到这一扇窗吗」（见 presence.go）。
	//
	// 窗口自己的生命周期回调改它；`wake.invalidate` 拿它决定这一帧要不要画；
	// `View` 拿它决定这一帧的逐帧动画要不要继续要下一帧。
	presence windowPresence

	// visible 是「这一扇窗看不看得见」那一个**活的**开关，给「每帧都要一帧」
	// 的动画用（波形那一格，见 `hover.SetVisibleFlag`）。
	//
	// ⚠️ **它逐窗口一份**：主窗口失焦不等于迷你播放器看不见（见 presence.go）。
	// ⚠️ **它是活的**：波形在**画的时候**读它（那一帧只重画、不重建，票 #392），
	// 所以窗口失焦的那一刻就得改它——`noteWindow` 那条路改的就是它。
	//
	// ⚠️ **零值是 nil，而零值 Shell 也得能用**：仓库里有几处手搓 `&Shell{}`
	// （测试里）。所以**别直接解引用它**——用 `visibleFlag()`（见下）。
	visible *atomic.Bool

	// snapScroll 是「这一帧是从看不见恢复过来的那一帧」，snapAt 是它的时刻。
	//
	// ⚠️ **不能只取一次**：MyGo 一帧会跑两遍 view（第一遍摆、第二遍留），而
	// 真正留在画面上的是**第二遍**。一次性取走的话第一遍把它用掉，第二遍就
	// 按普通路径滚了——症状正好是「恢复之后从旧位置慢慢追」。
	//
	// 所以按**时刻**认帧：`View` 在 `c.Now()` 变了的时候把它清掉，同一时刻的
	// 两遍都算这一帧。真窗口每帧一个时刻（`runtime.go` 的 `runFrame` 取一次
	// 墙钟）；Tester 的钟站着不动，于是它会多保持几帧——那没有副作用（目标
	// 就是当前位置，1ns 的那一下原地不动）。
	//
	// 歌词用它一次跳到位（见 lyricsView.centre 的 snap 参数）。
	snapScroll bool
	snapAt     time.Time

	// router 是 Biu 那 14 条路由的调度，带前进/后退历史。
	router *ui.Router

	// clock 是界面层等东西用的时钟（clock.go）。测试会换成假的，于是「404 页
	// 3 秒后回首页」不用真睡 3 秒。
	clock clockSource

	// tint 是封面取色的状态（见 covertint.go）。
	tint coverTint

	// shownPath 是上一帧画的是哪一页，syncRoute 拿它认出「换页了」。
	shownPath string

	// lastPath 是**上一帧**停在哪条路由上。
	//
	// 「刚进这一页的第一帧」就是拿它和这一帧的路由比出来的（`pageJustShown`）：
	// 框架切页时会把焦点还给「离开那一页时有焦点的那个元素」，而那枚会展开的
	// 搜索正是它——不挡一下，用户回来时键盘焦点就在一个他没打开过的框里。
	lastPath string

	// scrollbarHot / scrollbarHotNow 是「指针压在某个滚动条上」的上一帧与这一帧
	// （见 scrollbar.go）。主题每帧开头只能知道上一帧的：滚动容器这一帧还没建
	// 出来。两个字段都是主线程单点写，和别的界面状态一样。
	scrollbarHot    bool
	scrollbarHotNow bool

	// theme 是主题的缓存（见 theme.go 的 `themeCache`）。
	//
	// ⚠️ **逐窗口一份**：主题本身是全局的那一套（设置 + 系统深浅 + 桌面偏好），
	// 但每一扇窗各自缓存一份更简单——迷你播放器换主窗口时外壳是新建的（见
	// `toggleMiniPlayer`），而重建外壳本来就是「一切从零开始」的时候。
	//
	// **只由 view 在主线程上读写**，和别的界面状态同一条规矩。
	theme themeCache

	// laterPage / historyPage 是「稍后再看」和「历史记录」这两页各自的状态
	// （见 librarypage.go）。挂在壳上而不是页面的局部变量，理由同上。
	laterPage   libraryPage
	historyPage libraryPage
	// laterClearOpen 是「清除已看完」那道确认开着没有（页头右上角那一枚按钮，
	// 见 `later.go` 的 `viewLaterHeader`）。
	//
	// ⚠️ **它得跨帧活着**：即时模式每一帧都重画一遍，局部变量下一帧就没了。
	laterClearOpen bool
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

	// openExternal 是「打开一个外部网址」的那一下（把一个 B 站页面交给浏览器）。
	//
	// ⚠️ **默认是 `systemOpenLink`，而无头 Tester 里那一句会 panic**（`mygo.Shell`
	// 的 `needsApp` 见到 `App.Run` 没跑过就直接 panic，见 `mygo/app.go:128`）——
	// 所以**测试会把它换成记下地址的桩**。这一栏就是那个桩的插口。
	//
	// ⚠️ **别改成包级变量**：包级的可变全局会让两个窗口（主窗口 + 迷你）共享
	// 一个桩，而这一栏是**每扇窗一份**的。
	openExternal func(url string) error

	// images 是网络封面缓存。整个窗口共用一份；迷你窗口借的是主窗口那一份
	// （`adoptShared`），因为落盘的磁盘缓存目录只能有一个 writer/一本账，
	// 两个实例同时写同一个目录会各算各的字节数，上限就守不住了。
	images *imgcache.Cache

	// imagesWakeRemove 是摘掉「封面好了，叫醒这一扇窗」那条登记的动作。
	//
	// 和 `wakeRemove` 同一个理由：借来的那一份缓存活得比这一扇窗久，
	// 退迷你之后不摘，那个回调列表每进一次迷你就多一条。
	imagesWakeRemove func()

	// imagesReleased 记着这一扇窗有没有放开过封面缓存那一份使用权。
	//
	// ⚠️ 必须记：`closePerWindow` 可以重复调（关窗、退迷你、测试收尾各一次），
	// 而封面缓存是**引用计数**的——多放开一次会把还活着的那一份一起带走。
	imagesReleased bool

	// avatar 是顶栏右上角那一格的状态（见 navbar_avatar.go）。
	//
	// 建在 Shell 上而不是每帧新建：那一格跨帧要记「已经问过了没有」，每帧
	// 新建等于每帧都当成没问过——那是一帧一个请求。
	avatar avatarState

	// avatarMenuOpen 是头像下面那一层下拉开着没有（见 navbar_avatar.go）。
	//
	// ⚠️ **它是跨帧的**：即时模式每一帧都重画一遍，局部变量下一帧就没了。
	avatarMenuOpen bool
	// dynMenuFor 是「哪一条动态的「⋯」菜单开着」（空串 = 都没开）。
	//
	// ⚠️ **它记的是那一条动态的编号，不是「第几行」**：动态是往尾部追加的
	// （翻页追加），而刷新时新动态会插到**前面**——记行号的话，用户盯着
	// 那一格菜单会突然跑到别的条目上去。
	//
	// ⚠️ **建在 Shell 上而不是每帧新建**：即时模式每一帧都重画一遍，局部
	// 变量下一帧就没了（和 `avatarMenuOpen` 同一个理由）。
	dynMenuFor string
	// dynMenuOpen 是「那一格「⋯」菜单开着没有」。
	//
	// ⚠️ **它和 `dynMenuFor` 分开记**：框架的 `PopoverBase` 要的是一个
	// `*bool`（点外面关、按 Esc 关都写回那一个），而「开着的是哪一条」是
	// 另一件事——合成一个字段的话，框架写回 false 的时候会把「哪一条」也
	// 抹掉（那样倒也行，但两件事挤在一个字段里，改一处就容易改错另一处）。
	dynMenuOpen bool
	// avatarMenuIndex 是「当前是第几项」（键盘走到的 / 鼠标悬停的那一项）。
	//
	// ⚠️ **鼠标和键盘共用这一个下标**：两个下标的话，画面上会有两项同时亮着
	// （鼠标移到第三项上的那一帧，键盘那一项还亮着）。
	//
	// ⚠️ **-1 = 什么也没选中**：上游 HeroUI 打开下拉时不高亮任何一项，而
	// 「一打开就亮着第一项」会让回车直接进扫码登录页（那一下用户根本不是
	// 想登录）。
	avatarMenuIndex int
	// avatarMenuHover 是**上一帧**指针停在第几项（-1 = 不在菜单项上）。
	//
	// ⚠️ **它是为了让「指针不动的时候键盘说了算」**：每一帧都让悬停改一遍
	// 「当前是第几项」的话，键盘刚走的那一格会被指针改回去——「键盘能上下
	// 走」于是只在指针不在菜单项上的时候才成立。框架自己那个下拉也是这么
	// 做的（`mygo/ui/base.go` 的 `selectParts.Item`）。
	avatarMenuHover int
	// avatarLogoutAsk 是「退出登录」那道确认开着没有。
	//
	// ⚠️ **跨帧**：确认框要活过一帧（用户得看着它按「确认」或「取消」）。
	avatarLogoutAsk bool

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

	// follow 是「我的关注」那一页的状态：左边那一列分组、右边那个网格、切在
	// 哪个分组、搜索框里正在敲的字。见 follow.go。
	//
	// 同样是建一次存着：网格和分组列各记各的滚动位置，每帧新建等于每帧回到顶上。
	follow *followPage

	// downloads 是下载记录页自己那一摊状态：那张表格、删除确认弹窗。见
	// downloadlist.go。
	//
	// 同样是建一次存着：表格记着滚动位置和行身份，每帧新建等于每帧跳回顶上。
	downloads *downloadsPage

	// toasts 是右下角那些提示，addToast / clearToasts 是唯一改它的地方。
	toastMu  sync.Mutex
	toasts   []toast
	toastSeq uint64

	// announcements 是攒着待播报的那些句子（无障碍），见 announce.go。
	//
	// ⚠️ **它和 toasts 是两件事**：toast 是**画出来**的那张卡片（看得见的），
	// 而这一栏是**念出来**的那些话（读屏软件听得见的）。两者碰巧常常是同一句
	// ——提示弹出来的时候也念一遍——但「下载完成」那类**根本没有卡片**的事
	// 只有这一栏说得出去。
	announcements announceQueue

	// announceRemove 是摘掉这一扇窗在应用层那条播报登记的动作。
	//
	// 摘掉的那一半在 `closePerWindow` 里调：不摘的话「进迷你 / 退出迷你」来回
	// 十次之后应用层那个列表就有十条，每发生一件事就多走十趟空转。
	announceRemove func()

	// migrationAnnounced 是「启动时的迁移/导入结果已经弹过一次 toast 了」。
	//
	// 只由界面线程读写（`viewMain` 第一帧），所以不需要锁。挂在 Shell 上而不是
	// 包级的 map 上，是因为「每个窗口各弹一次」才是对的：迷你播放器窗口是**另
	// 一个窗口**，它的第一帧不该因为主窗口已经弹过就跳过（它也有自己的屏幕可
	// 以弹）。见 `announceMigration`。
	migrationAnnounced bool

	// fullScreen 是主窗口此刻是不是全屏。
	//
	// ⚠️ **它由窗口的回调推过来，不在 view 里问**：`win.IsFullScreen()`
	// 每帧问一次是跨一次 ObjC 调用（`mygo@v0.6.3/window.go:766` 的 `get`），
	// 而答案在**进全屏那一刻**就定了——事件推过来既省事又准。
	//
	// 它只给侧栏那个品牌块用（macOS 非全屏时要多留一格，见 `sidebar.go` 的
	// `sideBrandTopPad`）。只由界面线程读写（窗口回调都在主线程上）。
	fullScreen bool

	// scrub 是进度条的值（0..1），held 是指针有没有按在它上面。
	// 两个都只由 view 读写——view 永远跑在主线程上。
	scrub float64
	held  bool

	// fullScrub / fullHeld 是**全屏播放器上那条**进度条自己的一份。
	//
	// ⚠️ 不能和上面那一份共用：两处会各自按住、各自 `ScrubStart`/`ScrubEnd`，
	// 而 `ScrubEnd` 是一次真的 seek。共用的话在全屏里拖一下进度条，底下那条
	// 也会跟着跳（而它这会儿压根没被按着）。
	fullScrub float64
	fullHeld  bool

	// full 是全屏播放器自己那一摊，见 fullplayer.go。
	// **只由 view 在主线程上读写**，不跨 goroutine（铁律一）。
	full fullPlayer
	// vv 是满屏里视频画面那一边（见 fullvideo.go）。
	vv videoView
	// videoMadeFull 是系统全屏是视频的「屏幕全屏」键让进的。
	videoMadeFull bool

	// pageBuilds 是「路由页被建了几次」。**给测试看的探针**（理由同
	// `full.bgDraws`）：满屏开着的时候底下那一页不该再被建，而「有没有被建」
	// 在界面上看不出来——它被一层不透明的浮层整个盖着。
	//
	// ⚠️ 数的是 `Shell.page` 那一次分派（`router.go`）。**换页那一次只建一页**：
	// 我们不做换页动画（`router.go` 的 `newRouter`），离场页不会被重建，
	// 所以这个数换页前后都只跳一下。
	pageBuilds int

	// like 是长按封面 2.5 秒一键三连那一摊，见 like.go。
	// **同样只由 view 在主线程上读写**。
	//
	// ⚠️ 挂在壳上而不是每帧新建：即时模式下一帧一新建，「按了多久」每次都从
	// 零开始，那 2.5 秒永远到不了。
	//
	// ⚠️ **播放条那张封面和满屏那张封面共用这一份**：开着满屏的时候播放条整个
	// 不建（见 fullplayer.go 文件头），所以任一帧只有其中一个在喂它。
	like likeGesture

	// halo 是「正在取流」时绕着播放键转的那一圈光，见 loadhalo.go。
	//
	// ⚠️ **只有起点这一个字段**：角度是画的时候按钟算的，而起点得记着——
	// 播放条每 50ms 重建一次，每帧都拿「此刻」当起点的话光圈每 50ms 就
	// 跳回 12 点（那是在抖，不是在转）。
	//
	// ⚠️ **只由 view 在主线程上读写**，和别的界面状态同一条规矩。
	halo loadHalo

	// rateOpen 是倍率那个浮层开着没有。**只由 view 在主线程上读写**，
	// 见 playbar_rate.go。
	rateOpen bool

	// sleepOpen 是睡眠定时那个浮层开着没有。只由 view 在主线程上读写，
	// 见 playbar_sleep.go。
	sleepOpen bool

	// spaceAt 是上一次空格触发播放/暂停的帧时刻，用来同一帧去重。
	spaceAt time.Time

	// 音量浮层那一摊。**只由 view 在主线程上读写**，见 playbar_volume.go。
	//
	// volOpen 是那个浮层开着没有（悬停图标就开，指针走远了就收）。
	volOpen bool
	// volIdleSince 是「指针既不在图标上、也不在浮层里」的那一刻。零值表示此刻
	// 指针还在其中之一上面。
	volIdleSince time.Time
	// volPanelHovered 是**上一帧**指针在不在浮层里。
	//
	// ⚠️ **只能记上一帧的**：浮层在**覆盖层**里建，而覆盖层是主树**之后**才建的
	// ——主树里问不到「浮层这一帧的悬停」。
	volPanelHovered bool
	// volIdleStop 是那个「到点叫醒界面」的钟（见 `armVolIdle`）。
	//
	// ⚠️ **没有它浮层永远不会自己收掉**：不播放的时候没有任何东西在要帧，
	// 「指针离开了」只在那一帧被算一次，浮层于是永远留在画面上。
	volIdleStop func()
	//
	// ⚠️ **这里没有「静音之前的音量」了**：静音是应用层自己的一格
	// （`app.App.ToggleMute`），而音量是要落盘的（票 #311）。以前这一份记在
	// 壳上，跟着窗口走，进程一退就没了——落盘的音量不能是它。

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
	// sideRowKeys 记着侧栏收藏夹每一行的身份（**预先装箱好的那一份**）。
	//
	// ⚠️ 装箱那一步是要分配的（`runtime.convTstring`），现算的话每帧每行一次
	// （票 #313）。**它自己知道「数据换了」**（见 `rowkey.Memo` 的文件头），
	// 所以拖完排序、换收藏夹都不用记得重建。
	sideRowKeys [favGroups]rowkey.Memo[int64, any]

	// favPanelOpen 是**折叠态**那枚「收藏夹」图标点开之后的那层浮层开没开。
	//
	// ⚠️ 它只属于折叠态：展开态的收藏夹本来就在侧栏里，不需要浮层（见
	// sidebar_favorites.go 的 viewFavoritesCollapsed）。
	favPanelOpen bool

	// 播放列表抽屉这一摊。**同样只由 view 在主线程上读写**。
	//
	// drawerOpen 是开没开。它不进设置：抽屉是「我现在要看队列」这一下的事，
	// 关掉窗口就该是关着的（Biu 那边的 modal store 也不持久化它）。
	drawerOpen bool

	// drawerPhase 是抽屉现在处在进场的哪一段。**和 drawerOpen 不是一回事**：
	// 退场的那 200ms 里面板还在树上（滑出去了才拆），所以「关掉」不是「不建」。
	// 理由和那一组状态机见 queue_drawer.go 的 drawerPhase。
	drawerPhase drawerPhase

	// queueList 是抽屉里那一列的列表状态：滚动位置、行身份、行高。
	queueList ui.ListState
	// queueRowKeys 记着抽屉里每一行的身份（**预先装箱好的那一份**）。
	//
	// ⚠️ 理由同 `sideRowKeys`（票 #313）。
	queueRowKeys rowkey.Memo[string, any]
	// queueRowIDs 记着抽屉里每一行的**那两串身份**：判重标识（`Item.Identity()`）
	// 和行身份（`queueRowKey` 拼出来的那一份）。
	//
	// ⚠️ **它们每帧每一行都要，而现拼每帧每行两次**（票 #339）：判重标识是
	// 「local:」+ 路径，行身份是「queue-row:」+ 标识——二十首的队列就是四十来次
	// 分配。而 `queueRowKeys` 记着的那一份只是**装进 `any` 的那一步**（装箱），
	// 拼字符串那一步它没省。
	//
	// ⚠️ **它自己看得出「数据换了」**（见 `rowkey.Memo` 的文件头）：队列一改
	// （加歌、删歌、重排）它自己就重算，调用方一处都不用记得重建。
	queueRowIDs rowkey.Memo[queueRowRaw, queueRowIdentity]
	// queueRowOps 记着抽屉里每一行行尾那枚「⋯」的**无障碍名**。
	//
	// ⚠️ **它每帧每一行都要，而现拼每帧每行一次**（票 #249）：那个名字是
	// 「「」+ 歌名 + 「」的操作」，拼一次就是一个新字符串——抽屉一屏十来行就是
	// 十来次分配，而**队列抽屉那一档的每帧分配上限只比基线多 4 次**
	//（`framebudget_test.go`：44 次 / 基线 40 次）。现拼的话那一档直接顶穿。
	//
	// ⚠️ **理由和 `queueRowIDs` 一模一样**（票 #313 / #339）：它自己看得出「歌名
	// 换了」——原料就是歌名，歌名变了就重算，调用方一处都不用记得重建。
	// 列表那一档早就是这么记的（`widgets/list` 的 `opText`）。
	queueRowOps rowkey.Memo[string, string]
	// queueDrags 记着每一行那个**拖动值**（拖拽重排用的）。
	//
	// ⚠️ **它记的是指针**：框架把拖动值装进 `any`，装指针不分配——而每一行
	// 每帧都要传一次拖动值（见 queue_drag.go 的说明）。
	queueDrags rowkey.Memo[string, *queueDragValue]

	// queueDropAt 是**落点那一行**在抽屉里的行号（-1 = 没有落点）。
	//
	// ⚠️ **它由每一行自己记**（见 queue_drawer.go 的 viewQueueRow）：每一行都
	// 知道指针是不是在它身上、在它的哪一半——指针在谁身上，谁就是落点。
	queueDropAt int
	// queueDropUpper 是落点在那一行的**上半**（true）还是下半（false）。
	queueDropUpper bool
	// queueDragging 是「有东西正被拖着经过这一列」。
	//
	// ⚠️ **它比指针慢一帧**：它是**上一帧**的结论——插入线是在建行的时候画的
	// （见 viewQueueRow），而这一帧的结论要等这一列的行都建完才知道。差一帧
	// 看不出来——插入线晚一帧出现，而拖动本来就不是一帧的事。
	queueDragging bool
	// queueDraggingNow 是「**这一帧**有没有行是拖动源」。
	//
	// ⚠️ **每一行自己报**（见 viewQueueRow）：那一行本来就在问自己是不是拖动
	// 源（`rowEl.Dragging()`，拖着的那一行要画成半透明），拿它报「有东西正被
	// 拖着」不花钱——而框架的 `DragOver` 每问一次都要新建一个闭包（每帧一次
	// 分配，见 queue_drawer.go 里那一句）。
	queueDraggingNow bool

	// queueScroll 是那一列滚到哪儿了。**和 queueList 分开存**是因为「定位当前
	// 播放」要的是一段动画：ListState.ScrollTo 是一跳到位（`mygo/ui/list.go`
	// 的 ScrollTo），ScrollState.ScrollTo 才带 Animation。
	queueScroll ui.ScrollState

	// queueRows 是这一帧去重之后要画的那几行（连同它们在队列里的下标）。
	// queueList.Key 每帧都要读它，所以它和 rows 数得对上（见 queue_drawer.go）。
	queueRows []queueRow

	// queueClearOpen 是「清空播放列表」那道二次确认开着没有（抽屉头右上角那枚
	// 垃圾桶，见 queue_drawer.go 的 viewQueueClearConfirm）。
	//
	// ⚠️ **它得跨帧活着**：即时模式每一帧都重画一遍，局部变量下一帧就没了——
	// 那一帧弹窗还没画出来，状态就已经没了。
	queueClearOpen bool

	// 播放条上那个分集抽屉的一摊。**同样只由 view 在主线程上读写**。
	//
	// partOpen 不进设置，理由和 drawerOpen 一样：抽屉是「我现在要看分集」这一下
	// 的事（而 Biu 那边是 Popover 的 `isOpen`，也不持久化）。
	partOpen bool
	// partList 是浮层里那一列的列表状态，partRows 是这一帧要画的那几行。
	// 两个字段和 `s.full` 那一组（fullplayer.go）同义不同份——两处是两个浮层，
	// 各自记着自己滚到哪儿；行的**内容**是同一份（partRowsFor）。
	partList ui.ListState
	partRows []partRow
	// partRowKeys 记着分集抽屉里每一行的身份（**预先装箱好的那一份**）。
	//
	// ⚠️ 理由同 `sideRowKeys`（票 #313）。
	partRowKeys rowkey.Memo[string, any]

	// errDetailOpen 是「错误详情」那块面板开着没有（票 #320，见 playbar_err.go）。
	// **同样只由 view 在主线程上读写**。
	//
	// ⚠️ **它跟着错误一起收**：错没了（下一首放起来了）而它还开着的话，下一句
	// 错一上来，面板是开着的——用户会看到一块不属于这句错的浮层。
	errDetailOpen bool

	// 顶栏这一摊。**同样只由 view 在主线程上读写**。
	//
	// searchFocused 是搜索框有没有焦点，顶栏用它把输入区的底色抬到 30%
	// （上游 `group-data-[focus=true]:bg-…/30`）。浮层**不**靠它开
	// （见 navbar_search.go 里那一段：焦点被 MyGo 还回来是个假信号）。
	//
	// ⚠️ 叫 searchKeyword 不叫 searchQuery：包级已经有一个 `searchQuery` 了
	//（searchpage.go 的「URL 里搜索词那个键」，值是 "q"），两样东西没有关系，
	// 同名却不同义最容易读错。
	//
	// ⚠️ **它挂在壳上而不是每帧的局部变量**：即时模式每一帧都重画一遍，局部变量
	// 下一帧就没了（只有 view 里显式声明并被元素认下的状态才留得住）。壳上
	// 好几处状态都是同一个理由（设置页那一摊、日期范围那个弹层……）。
	searchKeyword string
	// searchKeywordFor 是「`searchKeyword` 是从哪个关键词填上去的」。
	//
	// ⚠️ **它是「不覆盖用户正在敲的字」那一条的判据**：框里那个字挂在壳上，
	// 所以它跨页活着——而路由上的关键词换了的时候，框里那个就是上一个词留下的
	// 旧东西（用户搜了另一个词，框里却还是上一个词）。判据是「框里那个字是从
	// 哪个词填上去的」：路由上的词没变 → 框里的是用户自己的（哪怕已经被他改
	// 得面目全非、或者删干净了），不动；变了 → 换掉。
	//
	// 页内那个输入框 2026-10-22 拍板删了之后，这一对是**顶栏那个框**和路由上
	// 的关键词之间的关系（见 navbar_search.go 的 `syncSearchKeyword`）。
	searchKeywordFor string
	// searchKeywordFilled 是「框里那个词是**填上去的**，不是用户自己敲的」。
	//
	// 填上去的那一种（深链接进来的 /search?q=…、翻到另一条历史）有个陷阱：用户
	// 点进框里敲一个字，期望的是「就搜这个字」，而实际得到的是「旧词 + 新字」。
	// 所以用户刚拿到焦点的时候把整个词选上（见 navbar_search.go 的
	// `viewSearchBox`）——接着敲的字就把它盖掉了。
	//
	// ⚠️ **用户自己敲的词不许全选**：他在框里敲到一半要改，替他全选上等于把他
	// 敲的字吃掉。
	searchKeywordFilled bool
	searchSuggest       bool
	searchFocused       bool
	// suggestShown 是「**这一场**进场已经播过了」。
	//
	// ⚠️ **它是「这一场」，不是「面板此刻在不在画面上」**：新的一趟请求会把上一批
	// 词清空（`internal/app/searchsuggest.go`），于是**「没有搜索历史」的用户每敲
	// 一个字面板都会空掉一帧**（框架不摆那个 panel）——按「此刻在不在」判的话，
	// **每一次按键都会重播一遍 200ms 的进场**（票面 AC 第 4 条要挡的正是这个）。
	//
	// ⚠️ **它只在浮层关掉的时候复位**（见 `navbar_search.go` 的 `viewSuggest`）：
	// 那是「用户重新开了一次浮层」，那一下**就该**重新播。
	//
	// ⚠️ **「用户敲了字、开关翻成 true 的那一帧面板可能还空着」**：那时候**没有
	// 面板可播**；等词回来、面板第一次出现在画面上，那一下才是用户看得见的那一下。
	suggestShown bool
	// tipCandidates 是这一遍 build 里登记下来的那些提示（tooltip.go）。
	//
	// 🔴 **同一时刻只出一枚提示**（票 #420）：登记的是「有可能要画的」，整遍
	// build 走完之后才挑出那一枚（见 `pickTip`）。
	//
	// ⚠️ **它按元素 ID 记的那一份状态在 `tips` 里**（下一条），而这里记的是
	// 「这一遍谁登记过」。
	tipCandidates []tipCandidate
	// tipPanel 是这一遍 build 出来的那一块提示面板（还没填内容）。
	//
	// ⚠️ **建在「第一个要出提示的那一处」**，不是最后——理由见 `tooltip.go` 的
	// `newTipPanel`：浮层按键的优先级是「最后建的先拿到」。
	tipPanel ui.Element
	// contentFocus 是「内容区里当前那一个能被方向键走位的」。
	//
	// **每帧开头清空**，每一帧由各个列表的登记重新填（见 focus.go 的
	// `registerContentFocus`）。只由界面线程读写（铁律一）。
	contentFocus focusTarget
	// focusIntoContent 是「用户要求把焦点交给内容区」。
	//
	// 侧栏在 build 的时候收到请求，而内容区**在侧栏之后才画**——所以这里记
	// 下来，等这一遍画完了再兑现（见 focus.go 的 `applyContentFocus`）。
	focusIntoContent bool
	// sideHandle 是侧栏那一栏的焦点句柄（方向键的入口，见 sidebar.go）。
	sideHandle ui.Handle
	// sideAnchor 是侧栏第一项的焦点句柄（焦点回侧栏时用它）。
	//
	// 逐窗口一份：壳是逐窗口的（主窗口、迷你播放器各一个），而框架的
	// `Handle.Focus` 不传窗口时用的是「唯一绑定着的那个窗口」。
	sideAnchor ui.Handle
	// tips 是带延迟的悬停提示的状态（tooltip.go），按元素 ID 记。
	// **只由界面线程读写**，和上面那几位同一条规矩。
	tips map[uint64]*tipState
	// terms 是浮层里「联想词」那一档的防抖状态，见 navbar_suggest.go。
	//
	// 挂在壳上而不是局部变量：即时模式每一帧都重画，「敲到哪儿了、从哪天起等的
	// 那 500ms」下一帧就得还在。
	terms suggestTerms
	// feed 是「动态更新红点」的 5 分钟节奏。假时钟在 navbar_feed.go。
	feed *feedPoller
	// hist 是搜索历史。没挂上盘时是内存里的一份（见 searchhistory.go）。
	hist *searchHistory

	// collection 是歌单内容页（`/collection/{id}`）此刻的那一份状态。
	// 换容器就整个换掉，见 `collectionFor`。
	collection *collectionPage

	// lyric 是歌词区自己那一摊：滚动位置、上一帧量到的两个框、正在滚向哪一句、
	// 以及右下角那两个浮层开着没有。见 lyrics.go。
	//
	// ⚠️ **逐窗口一份**（同 rateOpen / drawerOpen 的理由）：两个窗口画的是同一
	// 首歌，但歌词滚到了哪儿是各人各眼的事——迷你播放器窗口停在用户上次看到的那
	// 一句，主窗口跟着歌往前走，这才是对的。
	lyric lyricsView

	// fullLyric 是**满屏播放器那一屏**的歌词区自己那一摊。
	//
	// ⚠️ **不是 `lyric` 那一份**：同一首歌，迷你播放器那一页和满屏这一屏都画
	// 它的歌词，而「滚到了哪儿」是各人各眼的事——共用一份的话，在满屏里翻一下
	// 歌词，迷你播放器那一页也跟着被拽回去（反过来也一样）。
	fullLyric lyricsView

	// lyricSearch 是「搜索歌词」和「歌词预览」那两个弹窗自己那一摊：搜索框里
	// 正在敲的字、待发的那一次搜索、切在哪个源、候选表。见 lyricssearch.go。
	//
	// ⚠️ **逐窗口一份**（同 rateOpen / drawerOpen 的理由）：两个窗口画的是同一首
	// 歌，可「正在敲的是哪个词、切在哪个源」是各人各眼的事。
	lyricSearch lyricSearchUI

	// folderEdit 是「新建 / 修改收藏夹」那个弹窗（folderedit.go）。
	// nil 表示从没开过。
	//
	// ⚠️ 挂在窗口上而不是每帧新建：表单里每一个字段都是用户敲的，
	// 下一帧没了就等于用户敲一个字丢一个（铁律一）。
	folderEdit *folderEdit

	// folderSelect 是「把这一首放进哪些收藏夹」那个弹窗（folderselect.go）。
	folderSelect *folderSelect

	// folderDelete 是侧栏收藏夹「删除」那个二次确认要跨帧活着的状态
	// （sidebar_favorites.go）。
	//
	// 零值是「没在问」。和 folderEdit 一样只能由界面线程读写（铁律一）。
	folderDelete favoriteDeleteAsk

	// leave 是「刚被删掉的那个容器，等下一帧在界面线程上走开」那一个交接槽。
	//
	// ⚠️ **它带锁，是因为写它的是后台 goroutine**：删收藏夹那一趟跑在那边
	// （`app.DeleteCollectionFolder` 立刻返回），而 `navigate` 和
	// `s.collection = nil` 都只能由界面线程动（铁律一）。直接在回调里调，
	// `-race` 会报，而真机上的症状是「点删除有时候跳、有时候不跳」。
	leave pendingLeave

	// dynLikes 是动态卡片上「赞」的那一份状态（dynamiccard.go）。挂在壳上
	// 而不是页面状态里：动态流和 UP 空间是两个页面，但同一条动态在两边是同一
	// 个赞——放两份的话，用户在动态流赞完、切到那个 UP 的空间里看到的是没赞。
	dynLikes dynLikes

	// coverPick 是「后台把那个封面文件读出来、解好了」的那个交接槽。
	// 带锁，写它的是后台 goroutine、读它的是界面线程（见 folderpick.go）。
	coverPick coverPick

	// pickImage 是「弹文件框选一张封面图」那个**接缝**（folderpick.go）。
	//
	// ⚠️ 真实现要去系统里弹面板，而那一条在无头 Tester 里会 panic（`mygo`
	// 的 needsApp：`mygo.App.Run` 没跑过）。测试建完外壳换成桩，于是
	// 「选中的图有没有被解码、有没有被裁成 16:9」能自动验。
	//
	// ⚠️ **和上面那个 `picker`（pathPicker）是两回事，别合成一个**：那一个挑
	// 的是「一条路径」（下载目录、FFmpeg），要的是标题加一句问话；这一个挑
	// 的是「图片」，要的是**格式白名单**。合并的话有一边要带一个它不用的参数，
	// 而那个参数以后会有人传错。
	pickImage func(*mygo.Window) ([]string, error)

	// keys 是快捷键。和 hist 一样是单独一份文件（见 shortcuts.go）。没挂上盘时
	// 是内存里的一份：设置页照常能录，只是关掉之后不记得。
	keys *shortcutKeys
	// onShortcut 是「这条命令要做的事」，默认是 runShortcut（见 shortcut_dispatch.go）。
	//
	// 做成字段是为了能替掉它：真的那一份要音频设备（`App.Toggle` 在没有播放器时
	// 什么都不做），而「按了键之后走到哪一条命令」这件事在无头 Tester 里完全量得到
	// ——测试换成一个记 id 的函数就行。
	//
	// ⚠️ 应用内分发（viewShortcuts，帧里）和全局分发（syncGlobalShortcuts 登记的
	// 回调，主线程上）**走的是同一个它**，所以两条路测到的是同一段代码。
	onShortcut func(id string)
	// hotkeys 是「向系统登记一个全局快捷键」的那一半，见 shortcut_global.go。
	// 做成字段是为了能替掉它（理由和 autostart / pickImage 一样）。
	hotkeys globalShortcutService
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
	// recorderShown 是「上一帧画没画快捷键录制格」，由 viewShortcuts 每帧读一次再
	// 清掉、由 settingsShortcuts 画到时置回来。
	//
	// 它是为了兜住一件必须兜住的事：**`recording` 不能永远成立**。它一成立，
	// 窗口这一帧一个组合键都不登记（见 viewShortcuts 的注释），而用户是**看不到**
	// 「为什么现在按什么都不响」的。所以那一页不画了就把它收掉。
	recorderShown bool
	// registeredFor 是「上一次把全局快捷键登记到系统上时的录制状态」。它变了就得
	// 重新登记：录制期间全局那几个组合键必须从系统上撤下来（Carbon 热键到不了
	// 焦点上的元素，见 shortcut_global.go），退出录制之后又要回来。
	//
	// 由 syncGlobalShortcuts 自己写，viewShortcuts 只读。
	registeredFor shortcutSlot

	// settings 是设置页自己那一摊：切在哪个 Tab。建在 Shell 上而不是每帧新建，
	// 理由和 home 一模一样：切走再切回来要停在原来那一格上。
	settings settingsPageState

	// update 是更新弹窗自己那一摊，见 update_dialog.go。
	//
	// ⚠️ 它挂在 Shell 上而不是 `App` 上，所以**每个窗口各有一份**。今天这个
	// 应用只有一个窗口（`openWindow` 的注释写着「绝不开第二个」），于是「这一份」
	// 和全局的那一份是同一件事。写成这样是为了**将来**：真开了第二个窗口，弹窗
	// 在哪个窗口开着就是个该由那个窗口自己回答的问题，而更新器那一摊状态仍然
	// 只有一份（它在 `App` 上）。
	update struct {
		// wanted 是「用户按了检查更新 / 有更新可看，弹窗该开」。
		wanted bool
		// notes 是渲染更新日志的那份 markdown 选项。**建一次存着**：
		// `markdown.Options` 里有一张图片缓存表（`images sync.Map`），每帧新建
		// 等于每帧丢掉缓存，而更新日志是在下载中反复重画的那一块。
		notes *markdown.Options
		// scroll 是更新日志那一段的滚动位置。换一版更新日志要回到顶上。
		scroll ui.ScrollState
		// shownNotes 是上一帧画的是哪一版日志，用它认出「换版了」。
		shownNotes string
	}

	// about 是「关于」面板自己那一摊，见 about.go。
	//
	// ⚠️ 它挂在 Shell 上而不是 `App` 上，理由和下面那个 `update` 一样：
	// 面板是**画在某一扇窗里**的东西，而状态归那一扇窗。
	about struct {
		// wanted 是「用户按了应用菜单里「关于布丁音乐」，面板该开」。
		//
		// ⚠️ **它不是「菜单开着」**：菜单是系统画的，MacOS 上点菜单项的那一下
		// 是系统的事件，我们只知道自己被点了——所以这里记的是「该开面板了」，
		// 画不画、什么时候画，由界面这一层每一帧自己决定。
		wanted bool
	}

	// picker 弹系统文件框挑路径（下载目录、FFmpeg）。做成字段是为了能替掉它：
	// 真的那一份要过事件循环，无头 Tester 里没有事件循环，不替掉就只能等上真
	// 系统才验得到「选回来写进设置」这条链。和 autostart 同一个道理。
	picker pathPicker

	// crash 是上一帧 panic 到的东西，非 nil 时这一帧画错误页。
	//
	// ⚠️ 它记的**不只是 panic 的值**：还有崩在界面的哪一块（`where`）和当时
	// 在看哪一页。用户要拿这句话去跟开发者说，而「界面出错了」这句话谁也复现
	// 不出来——「崩在侧栏」和「崩在这一页的列表」是两件完全不同的事。见 crashInfo。
	crash *crashInfo

	// lastFeedbackURL 是上一次点「反馈」要去的地方。见 openFeedback 的注释。
	lastFeedbackURL string

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

	// loginAfter 是「登录成功了该去哪」——进登录页时写进去，登录成功时兑现。
	//
	// ⚠️ **空串 = 首页**（`homePath`）：用户 2026-10-25 原话是「登录成功后应该
	// 提示并且回到首页」。从「用户动态」那一路进去的写 `dynamicFeedPath`。
	//
	// ⚠️ **它记的是「进登录页那一次」的承诺**：进登录页时写，登录成功时读出来
	// 用掉，然后清空（见 `goLogin` / `finishLogin`）。
	//
	// **只由界面线程读写**（进登录页和登录成功都在主线程上）。
	loginAfter string

	// lastLoginPhase 是**上一帧**的登录相位。
	//
	// 它是「**刚刚**登录成功那一次」的判据（见 `syncLoginSuccess`）：登录成功
	// 是异步的状态变化，而界面是每帧重画的——只看「现在是已登录」的话，那
	// 一下会**每帧做一次**。
	//
	// **只由界面线程读写**（就在那一层里）。
	lastLoginPhase app.LoginPhase

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

	// mini 是迷你播放器那个窗口的外壳，nil = 没开。见 miniplayer.go。
	//
	// ⚠️ **只有主窗口的外壳有它**：两个窗口是互斥的（Biu 就是主窗口 hide() +
	// 开 mini），所以迷你窗口自己不会再开第二个。而迷你窗口那一侧靠 `main`
	// 指回来，关掉自己之后把主窗口的 `mini` 清成 nil。
	mini *Shell

	// main 是「我是被主窗口开出来的那个」这条边，只在迷你窗口那一侧有值。
	// 见 `attachMiniWindow`。
	main *Shell

	// shared 记着「封面缓存、快捷键和搜索历史是**主窗口那一份**」，不是自己开的。
	//
	// ⚠️ 它管的是**收尾**：那几样各自有一份后台资源（封面缓存有 writer 和解码
	// worker，快捷键 / 搜索历史有写盘循环），而它们此刻**属于另一个外壳**——
	// 迷你窗口关掉时把主窗口那份 `Close()` 了，主窗口后面在设置页改快捷键就再也
	// 落不了盘（症状是「设置页改了，重启之后没记住」，而界面上看不出异样），
	// 封面缓存关掉之后主窗口的新封面永远停在占位。
	// 详见 `adoptShared`。
	shared bool

	// localWakeRemove 是摘掉那一条「本地库变了」唤醒的动作（`App.AddLocalWake` 给的）。
	//
	// 🔴 **必须摘**：`New` 每建一个外壳就登记一条，而迷你窗口是「进/退迷你来回新建
	// 外壳」——不摘的话那个列表单调增长，每扫一遍本地库就多走一趟空转。
	// 只由 `App.Close()`（进程退出）才清空，那时已经太晚了。
	localWakeRemove func()

	// wakeRemove 是摘掉这一条唤醒句柄的动作（`App.AddWake` 给的）。
	//
	// 🔴 窗口真关掉时必须调：`App` 那边挨个叫醒，而摘掉的那一条调了是 no-op
	// （`wakeHandle` 内部是窗口的弱引用），不摘不致命，但「进迷你 / 退出迷你」
	// 来回十次之后那个列表就有十条，每改一次状态就多走十趟空转。
	wakeRemove func()

	// toggleMini 是迷你窗口上那个「退出」键要调的东西（= 主窗口的
	// `toggleMiniPlayer`）。做成字段是为了能替掉它：无头 Tester 里没有事件循环，
	// 真窗口在测试里也建不出来，所以「点那个键之后两个窗口会不会换」只能靠它
	// 量（和 autostart / pickImage 同一个道理）。
	toggleMini func()

	// miniScrub / miniHeld 是迷你窗口上那条进度条自己的一份。
	//
	// ⚠️ 它和上面那 `scrub` / `held` **不是同一扇窗的**：`scrub` 挂在主窗口那个
	// 外壳上，迷你窗口是**另一个外壳**（另一个 `Shell` 实例），天然各有一份，
	// 两个窗口的拖动不可能串到对方。所以这一对不是「防串」，而是和
	// `fullScrub` / `fullHeld` 同一个理由：一条进度条的值和「按住没有」是**它自己
	// 的**状态，写在别的名字底下，下一个人读 `scrub` 会以为那是唯一的那个。
	//
	// 真正会串的是应用层那一个：`ScrubStart`/`ScrubEnd` 是 `App` 上的全局方法，
	// 谁按都调它那一个（见 `viewMiniProgress` 那句「按住的时候值归用户，松手才
	// seek」——那一半就靠这对标记守住）。
	miniScrub float64
	miniHeld  bool
	// builds 记着「各个界面一共建了几遍」。**给测试看的**（见 builds.go 与
	// framebudget_test.go）：「一帧建两遍」是静默的坏处——画面上看着没差，
	// 而帧成本翻倍。
	builds buildTally

	// closed 记着这个外壳收过尾了。它是**「关掉之后不许再装定时器」那道闸门**，
	// 不是「收尾只走一遍」——`Close` 可以重复调，而底下每一项各自幂等。
	//
	// 为什么要有这道闸门：三个装定时器的地方（`feed.arm`、`armNotFound`、
	// `syncSearchWatch`）都是**每帧**都会到的，而 `Close` 收掉的是**某一个**已经
	// 装上的定时器。关掉之后只要还有一帧画到（无头 Tester 里 `Close` 之后再
	// `Frame()` 就会），它们就会再装一个，而那一个没人收。真窗口里 OnClosed 之后
	// 不再有帧，可这条不该靠「恰好不会」来对。
	//
	// 只由 `Close` 写。不用锁：它只在主线程上被读（view 跑在主线程），而
	// `Close` 也在主线程（窗口的 OnClosed、测试的 t.Cleanup）。
	closed bool
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
func New(a *app.App) *Shell { return newShellWithImages(a, coverCacheDir()) }

// newShellWithImages 是 New 的本体。imagesDir 空 = 封面缓存只在内存里。
//
// ⚠️ 迷你窗口那一份用**空目录**：它建好之后马上就把封面缓存换成主窗口那一份
// （`adoptShared`）。要是先按真目录建一份，每进一次迷你就会白扫一遍缓存目录
// （建目录即启动清扫），还可能按自己刚扫出来的账删掉几个主窗口正在用的文件
// ——两个实例各有一本账，谁都算不准对方。
func newShellWithImages(a *app.App, imagesDir string) *Shell {
	s := &Shell{
		app:    a,
		router: newRouter("/"),
		clock:  realClock{},
		images: imgcache.New(imgcache.Options{
			Dir: imagesDir,
			// 封面字节也走全应用唯一的网络层：代理设置对它生效，超时保留
			// 缓存自己那一档（图等不起，和取色同一个道理）。
			Client: func() *http.Client {
				c := newCredentialTransport().Client(nil)
				c.Timeout = 15 * time.Second
				return c
			}(),
		}),
		autostart: systemAutoStart{},
		pickImage: pickImageFile,
		hotkeys:   nilGlobalShortcuts{},
		// 「看得见」的默认值（见 newWindowPresence）：无头 Tester 没有窗口，
		// 还没建出窗口的那段启动路径也一样——它们都得照常画动画。
		presence: newWindowPresence(),
		// 「这一扇窗看不看得见」那一个**活的**开关（见上面那个字段的注释）。
		// 零值必须是「看得见」——理由同 `presence` 那两句。
		visible: hover.NewVisibleFlag(),
	}
	// 唤醒句柄问的是**这个外壳**的可见性（wake.go 的规则三）。
	s.wake.seen = s.presence.seen
	// 快捷键那两条路的动作都落到它。方法值在这儿绑一次就够：runShortcut 只碰
	// s.app 和 s.router，两个都是建好之后就一直在的。
	s.onShortcut = s.runShortcut
	s.imagesWakeRemove = s.images.AddWake(s.wake.invalidate)
	// 首页自己那一摊要在建好之后才有（它要用封面缓存），所以接在 images 后面。
	// 操作接在应用状态上、提示接自己的 Toast，接法见 widgets/list 的 AppActions。
	s.home = newHomePage(s.images, s.listActions(), s.wake.invalidate)
	// 新歌速递那两块翻页键带提示；提示的状态挂在壳上（tooltip.go），所以这儿
	// 把入口递进去。单独造那一块的测试拿到的是 nil，退回框架那枚。
	s.home.newGrid.tip = s.tip
	// 首页那一摊的取数接线（三个 Tab 的推荐单 + 新歌速递）：规则见
	// homepage_feed.go。这里只递「要拉什么」，不管什么时候拉。
	s.home.initLoads(homeLoads{
		mounted: func() bool { return s.app.RecommendOf().Mounted() },
		tab:     s.fetchHomeTab,
		new:     s.fetchHomeNewMusic,
		selfMid: s.app.SelfMid,
		wake:    s.wake.invalidate,
	})
	// 下载记录页同理（downloadlist.go）：两个「打开」接系统那一份，它们在无头
	// Tester 里会 panic，测试建完外壳就换成记动作的桩；记录那一条接应用层。
	s.downloads = newDownloadsPage(a.DownloadSnapshots, systemOpenDir,
		mygo.Shell.ShowItemInFolder, systemOpenLink)
	s.downloads.tip = s.tip
	// 本地音乐页同理。⚠️ 传进去的唤醒是 `s.local.invalidate` 而不是
	// `s.wake.invalidate`：后台扫完了要把**所有画过这一页的窗口**都叫醒，
	// 只叫醒这一个外壳的窗口的话，另一个停在旧帧（铁律三）。
	// ⚠️ 用 `s.listActions()` 而不是 `list.AppActions(...)`：同一份列表操作给
	// 每一处用（见 folderpick.go 里那个函数）。本地文件收藏不了（`favTarget`
	// 只认在线的），所以「收藏」那一项在本地音乐这一页会走「说清楚为什么」
	// 那一支——那也是对的答案。
	s.local = newLocalPage(a, s.images, s.listActions(), s.addToast, s.pickDir, s.displayView)
	// 键盘交接（票 #245 的 E4）：这一页自己画列表（`ui.Table`），所以「登记这一页
	// 的方向键目标」和「焦点交回侧栏」得自己装进去——理由见 `localPage` 那两个
	// 字段的注释。
	s.local.focusSink = s.registerContentFocus
	s.local.focusSide = s.focusSide
	s.local.noteScrollbar = s.noteScrollbar
	s.local.tip = s.tip
	// 「刚进这一页的第一帧」那一问（框架切页时会把焦点还给离开时有焦点的那个
	// 元素——这一页的「会展开的搜索」正是它，见 `Shell.pageJustShown`）。
	s.local.justShownFn = s.pageJustShown
	s.hist = newSearchHistory("")
	s.keys = newShortcutKeys("")
	s.picker = systemPathPicker{}
	// ⚠️ **唤醒那一路在这里就装好**（不是每帧装）：见 navbar_feed.go 里
	// `feedPoller.wake` 那一格——每帧装一次既是竞态，又会让第一帧发出的那一趟
	// 读到一个 nil 的唤醒（那个点于是要再等一个 5 分钟才亮）。
	s.feed = newFeedPoller(dynamicFeedInterval, s.askNewDynamics, s.askCatchUp, s.wake.invalidate)
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
	s.draw = s.guardAt("整帧", s.viewMain)
	// 本地音乐这一摊建好之后才能装唤醒回调（`AddLocalWake` 要一个能叫醒
	// **所有**画过这一页的窗口的函数，而那正是 `s.local.invalidate`）。
	//
	// ⚠️ **每个窗口建外壳都要登记一次**（`AddLocalWake` 是追加不是替换），
	// 而这一句在 `New` 里——所以迷你窗口建外壳时也会登记它那一条（它不画这一页，
	// 那一条是空转）。写成替换的话第二个外壳会把主窗口那条顶掉，症状是
	// 「后台扫完本地库之后主窗口再也不刷新」，而界面上看不出任何异样。
	//
	// 摘掉它的那一支存在 `localWakeRemove` 上，由 `closePerWindow` 调——迷你窗口
	// 「进/退」每回来回新建一个外壳，不摘的话那个列表单调增长。
	s.localWakeRemove = a.AddLocalWake(s.local.invalidate)
	// 播报也是**逐窗口**的一条登记：应用层那几件「后台发生了一件事」（播放失败、
	// 下载完成、歌放起来了）要把话送到每一扇窗，让它念给读屏软件听。
	//
	// ⚠️ **每个窗口建外壳都要登记一次**（和上面那条唤醒一样），而摘掉的那一支
	// 由 `closePerWindow` 调。
	s.announceRemove = a.AddAnnounce(s.announce)
	return s
}

// now 是界面层的「现在」。给那些要判「几天前」的地方用（投稿时间那一列）。
//
// ⚠️ **传 `s.now` 这个方法值，别传 `s.clock.Now`**：后者在取的那一刻就把
// 当时那个时钟接口定住了，而测试是在外壳建好之后才把 `s.clock` 换成假时钟
// 的——传值的话「今天 / 昨天」那一档永远跟着墙钟走。
func (s *Shell) now() time.Time { return s.clock.Now() }

// visibleFlag 是「这一扇窗看不看得见」那一个活的开关，**建好了就返回它**。
//
// ⚠️ **零值 Shell 也要能用**：仓库里有几处手搓 `&Shell{}`（测试里），而那一处
// 以前是能用的——所以这里**不假设它已经建好**：缺了就现建一个「看得见」的
// （零值必须是「看得见」，理由见 `hover` 那一包的 `frameVisible`）。
//
// ⚠️ **只在这一条路上建**（主线程）：`View` 和 `noteWindow` 都在主线程上
// （铁律一），而那两处是仅有的两个调用者。
func (s *Shell) visibleFlag() *atomic.Bool {
	if s.visible == nil {
		s.visible = hover.NewVisibleFlag()
	}
	return s.visible
}

// appName 是应用的名字。
//
// ⚠️ **只有这一处写着它**：窗口标题（`WindowOptions`）和侧栏左上角那个品牌字
// （`sidebar.go` 的 `viewSideBrand`）写的是同一个名字——2026-10 从 `Biu MyGo`
// 改名成 `BiuGo` 时分头改了两处，漏了一处，于是同一栏里两个产品名
// （`sidebar_test.go` 的 `TestTheSidebarShowsTheAppNameExactlyOnce` 钉住它）。
//
// ⚠️ **这和 bundle id、钥匙串服务名、用户数据目录名是四件互不相干的事**
// （见 AGENTS.md §5）：改显示名不许顺手动那三样。
const appName = "布丁音乐"

// appNameShort 是折叠态那一片摆的那一个字（侧栏折叠后只剩 72 像素，
// 「布丁音乐」四个字放不下——见 `viewSideBrand`）。
const appNameShort = "布"
