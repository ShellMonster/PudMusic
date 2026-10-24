// Package ui 是界面层：整个项目里只有它 import MyGo 的 ui 包。
//
// 本文件里的 `ui.Xxx` 一律指 MyGo 的 ui 包，不是本包——包内引用自己不需要写包名。
package ui

import (
	"embed"
	"log"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/media"
	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/guard"
	"github.com/ShellMonster/Biu_Go/internal/ui/rowkey"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// artFile 是插画那一小份 embed（错误页那张 `error.svg`）。
//
// ⚠️ **它和图标那一份分开**：图标全是单色的遮罩、走 `ui.Icon`，插画有自己
// 的灰度层次、走 `ui.Image`。合成一份 embed 的话，`iconset` 就得替插画也
// 实现一套「解析成 *ui.SVG 再交给调用方」的东西，而它对图标那套是对的、对
// 插画是多余的。
//
//go:embed art
var artFile embed.FS

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
	// 每帧问一次是跨一次 ObjC 调用（`mygo@v0.6.2/window.go:764` 的 `get`），
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

	// pageBuilds 是「路由页被建了几次」。**给测试看的探针**（理由同
	// `full.bgDraws`）：满屏开着的时候底下那一页不该再被建，而「有没有被建」
	// 在界面上看不出来——它被一层不透明的浮层整个盖着。
	//
	// ⚠️ 数的是 `Shell.page` 那一次分派（`router.go`）：换页动画期间离场的那
	// 一页也会被建一次，所以这个数在换页那一帧会跳两下——那是对的，两页都建了。
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

	// rateOpen 是倍率那个浮层开着没有。**只由 view 在主线程上读写**，
	// 见 playbar_rate.go。
	rateOpen bool

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

	// queueScroll 是那一列滚到哪儿了。**和 queueList 分开存**是因为「定位当前
	// 播放」要的是一段动画：ListState.ScrollTo 是一跳到位（`mygo/ui/list.go`
	// 的 ScrollTo），ScrollState.ScrollTo 才带 Animation。
	queueScroll ui.ScrollState

	// queueRows 是这一帧去重之后要画的那几行（连同它们在队列里的下标）。
	// queueList.Key 每帧都要读它，所以它和 rows 数得对上（见 queue_drawer.go）。
	queueRows []queueRow

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
	// tipHoverNow / tipHoverPrev 是「正拿着**悬停驱动**的那枚提示」的元素 ID。
	//
	// ⚠️ **每一遍 build 都要挪一次**（见 `Shell.View` 开头）：框架一帧跑好几遍
	// view，真正留在画面上的是最后一遍，所以「这一遍有没有悬停提示」要问上一遍
	// 登记的那一份——理由见 `tipHoverBusy`。
	tipHoverNow  uint64
	tipHoverPrev uint64
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

	// upHeaderTop 是 UP 空间头图区里「信息那一行的上沿」（算出来的，见
	// `upspace.go` 的 `upHeaderTextTop`），加上算它时的那几个输入。
	//
	// ⚠️ **它是缓存**：那一列的高度要量三次字（`c.MeasureText`），而这一页
	// 每帧都画一次。输入（字号 / 名字 / 认证 / 签名）变了才重算。
	//
	// **只由 view 读写**（view 永远跑在主线程上），所以没有锁。
	upHeaderTop         float32
	upHeaderTopScale    float32
	upHeaderTopName     string
	upHeaderTopOfficial string
	upHeaderTopSign     string
	upHeaderTopValid    bool

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

// Close 收掉这个外壳占着的一切：四个定时器、封面缓存、应用状态，以及设置 /
// 搜索历史 / 快捷键的落盘收尾。**可以重复调。**
//
// ## 为什么它得是一个方法，而不是一段挂在窗口回调里的代码
//
// 这一段收尾以前只长在 `Attach` 的 `OnClosed` 上，而 `OnClosed` 只在「挂着的那个
// 窗口真的关掉了」的时候才跑。于是**持有这些资源的其实不是窗口、是外壳**，而外壳
// 却只有一条能收尾的路：
//
//   - 快捷键的后台写循环（`shortcutKeys.loop`）、红点那个每五分钟一跳的定时器、
//     404 那个每一秒一跳的定时器、搜索防抖那个钟——一律没人收。
//   - 每一处都是**一个外壳一份**，而这条路上一个都不会走到。
//
// 真进程退出时这不算什么（进程一死它们跟着死）。真正付账的是**不退出进程的那些
// 路**：无头测试里一个外壳就是一个 `Tester`，几百条测试建几百个外壳，每一个都带
// 一个写循环和两个定时器跑到测试二进制结束；而「建了外壳但窗口最终没建出来」的那
// 些启动路径（`openWindow` 排在 `WhenReady` 队列里，ready 之前就返回）也是同一
// 个形状。
//
// 收尾因此挂在**外壳**上而不是窗口上：窗口关了就调它（`Attach` 的 `OnClosed`），
// 没建出窗口的那条路也调它，测试的 `t.Cleanup` 还是调它——三处走的是同一份代码，
// 不会再有「窗口的回调忘了谁」这种事。
func (s *Shell) Close() {
	// ⚠️ **这里不提前 return**。底下每一项各自都是幂等的（定时器那几个是
	// 「判 nil → 调 → 置 nil」，`images.Close` / `app.Close` / `hist.Close` /
	// `keys.Close` 各自都有 `closed` 判断），所以再走一遍既无害、又是必须的：
	// 万一关掉之后还有一帧画到了顶栏（无头 Tester 里 `Close` 之后再 `Frame()`
	// 就会，见 `close_while_loading_test.go`），那三个「装定时器」的地方要看
	// `s.closed` 才肯装——但路径上还有别的呢，「再走一遍」就是最后一道网。
	//
	// 早先这里写的是「`closed` 就 return」，而它挡住的正是这件事：`Close` 走过一趟
	// 之后装的定时器，第二次 `Close` 收不掉（`feed.arm` 那一处在关掉后仍会被每帧触
	// 发），于是它一路挂到测试进程结束。
	s.win = nil

	// 这一扇窗自己占着的东西——**只收这一扇的**。迷你播放器那个外壳走的是这条路
	// 而不是 `Close`：它不能停播放、不能关设置（那是整个应用的事）。
	//
	// 所以这一段被拆出来了：`Close` 收完它再收应用层那几样，而迷你窗口只调它。
	// 不拆的话迷你那一份就得把下面这四行抄一遍——抄的那份迟早和这里不一样。
	s.closePerWindow()

	// 停播放、关解码器和文件句柄、停后台时钟。
	s.app.Close()

	// 关窗就收尾设置：停掉后台写盘，并把还欠着的改动同步落完。
	//
	// MountSettings 里那个 OnWillQuit 的 Flush 兜的是「退进程」这条路；
	// 这一句兜的是「关窗口」——托盘早就在了（issue #28 已 CLOSED：
	// `tray.go` / `single_instance.go` / 开机自启），所以「关窗口」早就
	// 不等于「退进程」了，那条 OnWillQuit 并不保证会跑，两句要一起看。
	//
	// `CloseSettings` 可以重复调（内部先 LoadAndDelete），所以窗口关了之后
	// 进程再退、再调一次都不会出错。
	if err := s.app.CloseSettings(); err != nil {
		log.Printf("biu: 关窗时保存设置失败：%v", err)
	}
	// 搜索历史与快捷键那两样在 `closePerWindow` 里收了（它们是逐窗口的）。
}

// closePerWindow 收掉**这一扇窗自己**占着的东西：三个定时器、唤醒句柄、封面缓存、
// 搜索历史和快捷键的落盘。**可以重复调**（底下每一项各自幂等）。
//
// ## 为什么它和 `Close` 分开
//
// `Close` 是**整个应用**的收尾——它还 `s.app.Close()`（停播放、关解码器）、关设置。
// 而迷你播放器是**第二个窗口**，它那个外壳关掉时那些一概不能做：关掉一扇小窗就把正
// 在放的歌停掉，而主窗口明明还开着（症状是「从迷你切回来，音乐没了」，和真因隔得
// 很远）。所以它走这一条。
//
// 另一半（两个窗口共用同一个 App 那一层）压根不该由任何一个窗口来收——那是
// `App.Close` 的事，由主窗口那一侧的 `Close` 触发。
func (s *Shell) closePerWindow() {
	// 🔴 **这道闸门在这一段里，不在 `Close`**：三个「装定时器」的地方（`feed.arm`、
	// `armNotFound`、`syncSearchWatch`）看的是 `s.closed`，而迷你播放器那个外壳走的
	// 是这一段——它收完就该和主窗口那个一样「不再装任何定时器」，哪怕之后还有一帧
	// 画到（无头 Tester 里关掉之后再 `Frame()` 就会，见 `close_while_loading_test.go`）。
	s.closed = true

	// 三个定时器。全是「窗口没了之后唤醒对它是 no-op，留着不致命，但会一直醒到
	// 进程退出」那一类。
	//
	// ⚠️ 搜索防抖那个钟**不在** viewMain 的 syncSearchWatch 管辖范围内：
	// 那一句只保证「用户切走时它会停」，而「外壳没了」它管不着。
	if s.searchWatchStop != nil {
		s.searchWatchStop()
		s.searchWatchStop = nil
	}
	s.feed.disarm()    // 红点，每五分钟一跳（navbar_feed.go）
	s.disarmNotFound() // 404 倒计时，每一秒一跳（notfound.go）
	s.disarmVolIdle()  // 音量浮层那个到点叫醒界面的钟（playbar_volume.go）

	// 🔴 摘掉这一条唤醒句柄（铁律三那个逐窗口的列表）。摘掉之后调它是 no-op，
	// 而不摘不致命——但「进迷你 / 退出迷你」来回十次之后那个列表就有十条，
	// 每改一次状态就多走十趟空转。
	if s.wakeRemove != nil {
		s.wakeRemove()
		s.wakeRemove = nil
	}

	// 摘掉这一扇窗在应用层那条播报登记（理由同上面那条唤醒句柄）。
	if s.announceRemove != nil {
		s.announceRemove()
		s.announceRemove = nil
	}

	// 封面缓存：先摘掉这一扇窗的唤醒登记（借来的那一份活得比这一扇窗久），
	// 再放开自己那一份使用权。
	//
	// ⚠️ **放开的不是「关掉」**：迷你窗口借的是主窗口那份封面缓存（一个磁盘
	// 缓存目录只能有一本账），引用计数到 0 才真的关。所以主窗口那个外壳先收
	// 尾时迷你那份照样能用——不然迷你之后每要一张图都是 `ErrClosed` 永久失败
	// 态，界面上的症状只是「图不出来」。
	//
	// `imagesReleased` 保证**只放开一次**：`closePerWindow` 可以重复调（关窗、
	// 退迷你、测试收尾都会调它），多放开一次会把别人那份一起带走。
	if s.imagesWakeRemove != nil {
		s.imagesWakeRemove()
		s.imagesWakeRemove = nil
	}
	if !s.imagesReleased {
		s.imagesReleased = true
		s.images.Close()
	}

	// 🔴 摘掉「本地库变了」这一条（`New` 里登记的）。不摘的话「进迷你 / 退出迷你」
	// 来回十次之后那个列表就有十条，每扫一遍库就多走一趟空转——而这一条属于**已经
	// 收掉的**那个外壳。
	if s.localWakeRemove != nil {
		s.localWakeRemove()
		s.localWakeRemove = nil
	}

	// 搜索历史与快捷键：写盘是后台合并的，最后一下有可能还等在退避窗口里。
	//
	// ⚠️ **借来的那一份不能收**（`shared`）：它们此刻属于另一个外壳，而后台写盘
	// 循环停掉之后，主窗口在设置页改的快捷键就再也落不了盘。这里收的只是**自己开的**
	// 那两份。
	if s.shared {
		return
	}
	_ = s.hist.Close()
	_ = s.keys.Close()
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

// coverCacheDir 是封面磁盘缓存的位置：**应用自己的缓存目录**下单独一个子目录。
//
// `mygo.PathCache` 是 `os.UserCacheDir()` 拼上应用名（macOS 上是
// `~/Library/Caches/BiuGo`），系统认定的「这些都可以删」的地方——不是用户文件
// 目录，也不在应用数据目录（`Biu MyGo`）里，所以清缓存不会碰到设置和历史。
// 再套一层 `cover-images` 是为了不让别的东西混进这个「整目录都归缓存」的地方。
//
// ⚠️ **测试必须把它替成临时目录**（`newTestShell` 替了）：无头 Tester 里
// `mygo.App.Path` 照样会返回一个真的、可写的目录，不替的话每条测试都会往用户
// 真正的缓存目录里写封面，而且测试之间串数据。
//
// 拿不到缓存目录不算错误：返回空串 = 不落盘，封面照常从网上下来，只是下次启动
// 要再下一次。为缓存挡住窗口不值当。
var coverCacheDir = func() string {
	dir, err := mygo.App.Path(mygo.PathCache)
	if err != nil {
		log.Printf("biu: 拿不到缓存目录，封面这次不落盘：%v", err)
		return ""
	}
	return filepath.Join(dir, "cover-images")
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
		app:       a,
		router:    ui.NewRouter("/"),
		clock:     realClock{},
		images:    imgcache.New(imgcache.Options{Dir: imagesDir}),
		autostart: systemAutoStart{},
		pickImage: pickImageFile,
		hotkeys:   nilGlobalShortcuts{},
		// 「看得见」的默认值（见 newWindowPresence）：无头 Tester 没有窗口，
		// 还没建出窗口的那段启动路径也一样——它们都得照常画动画。
		presence: newWindowPresence(),
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
// ⚠️ **这一整套只给主窗口用**。迷你播放器窗口走 `attachMiniWindow`：它**不**能
// 装下面那个 `OnClosed`（那是「整个应用收尾」——停播放、关设置、存盘），套上去
// 的话关掉迷你窗口会把正在放的歌停掉，而主窗口明明还开着。
//
// 一个窗口上的生命周期回调全在这儿，以后新增的（显示/隐藏、拖拽文件…）也挂这里：
// 分成两处的话，总有一个回调会在换窗口的时候漏装，而漏装之后的症状
// （「窗口没关干净」「图下完了界面不动」）都跟漏装的那一行毫无关系，极难查。
func (s *Shell) Attach(w *mygo.Window) {
	s.win = w
	// 「用户看不看得到这一扇窗」从这一刻起由系统推过来（票 #156）：
	// 失焦、最小化、隐藏各自会更新 `presence`，重新看得见时补一帧。
	s.watchWindow(w)
	// 🔴 铁律三：**逐窗口**登记，App 那边挨个叫醒。只叫醒主窗口的话迷你窗口停在
	// 旧内容（实测：只投递不唤醒两个窗口各 +0 帧；只唤醒主窗口则迷你窗口停在旧内容）。
	s.wakeRemove = s.app.AddWake(s.wake.invalidate)

	// 从访达 / 资源管理器拖进来的文件：音频进本地音乐，别的说清楚为什么
	// （见 `localmusic_drop.go`）。挂在这里而不是 `New`：`Attach` 才是窗口
	// 生命周期回调的入口，而且这一套只给主窗口——迷你播放器那一扇不画本地
	// 音乐页，也没有画 toast 的地方。
	w.OnFileDrop(func(e *mygo.FileDropEvent) { s.handleFileDrop(e.Paths) })

	// 进 / 出全屏：侧栏左上角那个品牌块在 macOS 非全屏时要多留一格（上游的
	// `pt-8`，见 `sidebar.go` 的 `sideBrandTopPad`）。
	//
	// ⚠️ **改完要叫醒一帧**：这一格是画出来的，状态变了而画面没重画的话，
	// 用户看到的还是进全屏之前的样子（而窗口在进全屏那一刻本来就会重画，
	// 但**出**全屏那一帧不一定）。
	w.OnEnterFullScreen(func() { s.fullScreen = true; s.wake.invalidate() })
	w.OnLeaveFullScreen(func() { s.fullScreen = false; s.wake.invalidate() })

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
	//
	// 收尾本身是 `Shell.Close`（见它的注释：以前这一段只长在这儿，于是没挂到
	// 窗口上的外壳永远收不掉那几个定时器）。
	//
	// ⚠️ 所以这个回调**不在 OnClose** 里：选「隐藏到托盘」时 OnClosed 压根不跑
	// （窗口只是收起来了，应用还活着），那时候红点和 404 的倒计时都该继续走
	// ——用户把窗口叫回来时本来就该看到已经回首页的样子，不是还停在一个正在
	// 倒计时的 404 上。
	w.OnClosed(func() {
		if s.win == w {
			s.win = nil
		}
		// 摘掉这一条唤醒句柄（铁律三那个列表）。
		//
		// ⚠️ 这一句 `Close()` 里也有，但**这里还要有一份**：`Close` 是「整个外壳
		// 收尾」，而迷你窗口那个外壳**不能**调它（它会 `s.app.Close()` 把正在放
		// 的歌停掉）。两处走的是同一个 `s.wakeRemove`，摘掉之后第二次调是空操作。
		if s.wakeRemove != nil {
			s.wakeRemove()
			s.wakeRemove = nil
		}
		s.Close()
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
	// 真的那一份全局快捷键**在这里**装上，而不是在 `New` 里：`mygo.GlobalShortcut`
	// 的两个方法都要走事件循环，而 `New` 的调用方还有一整套测试（无头 Tester 没有
	// 事件循环，`Register` 在那儿是直接 panic 的）。`Run` 是唯一一个真的有事件循环
	// 的地方，而它是 main.go 唯一调用的那一个。
	s.hotkeys = systemGlobalShortcuts{}
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
		// 全局快捷键也在这儿登记，理由和上面两件一样：`mygo.GlobalShortcut.Register`
		// 头上有 `needsApp`，应用没 ready 时调它直接 panic（`modules.go:550`）。
		// 排在 `openWindow` 之前：窗口还没建的时候注册失败要弹的那句提示，
		// 第一帧就能画出来（提示挂在壳上，见 addToast）。
		s.syncGlobalShortcuts()
		// 本地音乐目录的文件监听。⚠️ 必须在 `WhenReady` 里：`mygo.WatchFiles`
		// 在应用还没 ready 的时候调会 panic（框架的 `needsApp`，
		// `mygo/app.go:128`），而 `Run` 之前是那一刻。
		a.StartLocalMusicWatch()
		// 后台自动查更新。**必须在 `WhenReady` 里**：第一次查询要走网络，而
		// `WhenReady` 之前事件循环还没转起来，那时候发请求等于在一条还没开始
		// 跑的路上走（框架自己也把「需要事件循环」的 API 挡在 ready 之前，见
		// 上面 `StartLocalMusicWatch` 那一条）。
		//
		// 停掉它的是 `App.Close`（`updater.close`），而 `Close` 挂在退出流程上——
		// 所以关掉应用之后不会有下一次查询在后台跑着。
		a.StartAutoChecks()
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

// appName 是应用的名字。
//
// ⚠️ **只有这一处写着它**：窗口标题（`WindowOptions`）和侧栏左上角那个品牌字
// （`sidebar.go` 的 `viewSideBrand`）写的是同一个名字——2026-10 从 `Biu MyGo`
// 改名成 `BiuGo` 时分头改了两处，漏了一处，于是同一栏里两个产品名
// （`sidebar_test.go` 的 `TestTheSidebarShowsTheAppNameExactlyOnce` 钉住它）。
//
// ⚠️ **这和 bundle id、钥匙串服务名、用户数据目录名是四件互不相干的事**
// （见 AGENTS.md §5）：改显示名不许顺手动那三样。
const appName = "BiuGo"

// WindowOptions 是主窗口的样子。view 是这个窗口每帧要画的界面。
//
// ⚠️ macOS 上要的是**保留红绿灯的隐藏标题栏**，不是 `Frameless: true`——
// `Frameless` 会把系统控件一起删掉，红黄绿就没了。Biu 是无边框、标题栏自
// 绘、系统控件留在上面，MyGo 的 `TitleBarHidden` 说的就是同一件事
// （`window.go` 里 `hiddenTitleBar = !Frameless && (TitleBarHidden || …)`）。
// `TitleBarHiddenInset` 只是把控件再往里缩一点，Biu 没做这个差别。
func WindowOptions(view func(*ui.Context)) mygo.WindowOptions {
	return mygo.WindowOptions{
		Title:     appName,
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
//
// ⚠️ **界面上的每一处都该走 `guardAt` 而不是它**，因为只有前者记得「崩在界面的
// 哪一块」，而那是错误页上「出错在哪一步」那一句的全部来源（见 `crashInfo`）。
// 这一个留给**拿不到 Shell 的调用方**——控件层，以及测试里单独验 Guard 本身。
func Guard(draw func(*ui.Context), onPanic func(any)) func(*ui.Context) {
	return guard.Wrap(draw, onPanic)
}

// crashInfo 是一次崩溃的**完整**现场。
//
// # 为什么要三个字段，而不只是崩掉的那个值
//
// 改之前错误页上只有一句 `%v`（panic 的值）。而 panic 的值大多长这样：
// `runtime error: index out of range [5] with length 3`——这句话说明了**是什么**，
// 一点也没说**在哪儿**。用户能做的只有把这句发给开发者，而开发者拿着它既复现
// 不出来，也定位不到（同一个越界能发生在七个不同的地方）。
//
// 两条「在哪儿」各有各的来源，缺一条就退化成原来那句：
//
//   - where 是**界面的哪一块**：由包住那一块的 `guardAt` 写上去。崩在侧栏和崩在
//     队列抽屉，用户看到的是完全不同的两种现象（一个是一大片空白，一个是抽屉
//     关不上）。
//   - path/title 是**当时在看哪一页**：从 `s.router` 上读，不用谁记得报上来。
type crashInfo struct {
	// where 是崩在界面的哪一块，由 `guardAt` 的调用点给。
	where string
	// path 是崩的时候正在看的那条路径（`s.router.Location()`）。
	path string
	// title 是那一页的名字（`routes` 表里的 `Title`）。
	title string
	// got 是 panic 到的东西。
	got any
}

// View 画一帧，MyGo 在需要新画面的时候调它。
func (s *Shell) View(c *ui.Context) {
	// 提示的「悬停那一枚」登记，每一遍 build 挪一次（理由见 tipHoverBusy）。
	//
	// ⚠️ **挪在每一遍的开头，不是一帧的开头**：框架一帧跑好几遍 view，真正留在
	// 画面上的是最后一遍，而「悬停提示有没有登记过」这件事在最后那一遍里要问
	// 上一遍——焦点所在的那一枚可能先被 build。
	s.tipHoverPrev, s.tipHoverNow = s.tipHoverNow, 0

	// 「内容区里当前那一个列表」每帧重新登记：这一遍开头先清空，登记的那几
	// 个会在这一遍 build 里重新填（见 focus.go 的 `registerContentFocus`）。
	//
	// ⚠️ **清空是必须的**：不清的话，切到「这一页没有列表」之后，焦点会被
	// 交给一个**不在画面上**的列表——用户看不见任何指示，方向键也收不到。
	s.contentFocus = nil

	// 每帧重新登记：句柄内部是窗口的弱引用，窗口关了自动失效，关掉重开也不用管。
	s.wake.set(c)

	// 这一帧的窗口看不看得见，交给「每帧都要一帧」的动画自己去判断（见
	// hover.SetFrameVisible 的注释）。**必须在 draw 之前**：波形是在 draw 里
	// 建出来的，而它在建的那一刻就要知道这一帧要不要再要一帧。
	//
	// ⚠️ 这一句挡的是「自发动画」，不挡「有人在要帧」：看不见的时候之所以
	// 没有帧，是因为 `wakeHandle.invalidate` 不进来了；万一有别的路径送进来
	// 一帧（比如本地库扫完那一支直接调 `Services.Invalidate`），这一帧照样
	// 画得出来，只是它**不会再要下一帧**。两处合起来才停得下来。
	hover.SetFrameVisible(s.presence.seen())

	// 从看不见恢复过来的那一帧：让那些「有中间状态」的动画一次到位（见
	// snapScroll 的注释：同一时刻的两遍 view 都算这一帧）。
	if now := c.Now(); s.snapScroll && !now.Equal(s.snapAt) {
		s.snapScroll = false
	}
	if s.presence.takeResumed() {
		s.snapScroll, s.snapAt = true, c.Now()
	}

	if s.crash != nil {
		s.viewCrash(c)
		return
	}
	s.draw(c)

	// 兑现「把焦点交给内容区」这个请求（票 #245 的 E4）。
	//
	// ⚠️ **排在 draw 之后**：内容区在 draw 里画，各张列表的登记也在那儿——
	// 兑现的时候得已经有「当前那一个列表」可交。
	//
	// ⚠️ **每一遍 build 都会走到这里**：兑现一次就把请求清掉（见
	// `applyContentFocus`），所以只有第一遍真的兑现。
	s.applyContentFocus()

	// 把攒着的那些状态变化念给读屏软件听（无障碍，见 announce.go）。
	//
	// ⚠️ **排在最后**：这一句要念的是「刚刚发生了什么」，而刚刚发生的那些事
	// 有一部分是**这一遍建视图的时候才知道的**（按了下载、按了收藏——那些话
	// 是提示，提示也是要念的）。排在前面的话它们要等到下一帧才念出去，而下一帧
	// 未必来得快（界面是按需绘制的）。
	//
	// ⚠️ **一帧建好几遍视图，这里只念一遍**：`take` 是有破坏性的（见 announce.go）。
	s.flushAnnouncements(c)
}

// guardAt 给一层 view 包 Guard，并记住**它是界面的哪一块**。
//
// 界面上的每一道 Guard 都走它（`Guard` 那个自由函数留给不需要记位置的地方，
// 比如控件层自己包的那一道）。为什么每一处都要写一句「这一块是什么」：那是错误
// 页上「出错在哪一步」那一句的全部来源，而这正是这一票要的（issue 126：「出错时
// 能看到出错在哪一步」）。
//
// ⚠️ **写「是什么」而不是「在哪儿崩的」**：调用点在崩之前执行，它知道自己在
// 画哪一块 whereas 崩在它内部的第几行它不知道——那要等栈，而栈在错误页上对
// 用户没有意义（几百行 Go 调用，用户看不懂也用不上；它已经在日志里了）。
func (s *Shell) guardAt(where string, draw func(*ui.Context)) func(*ui.Context) {
	return guard.Wrap(draw, func(r any) { s.onPanicAt(where, r) })
}

// onPanic 记下崩掉的东西并要一帧，下一帧画错误页。
//
// ⚠️ 这里补的 Invalidate 不是多余的：recover 是在 build pass **中间**接住的，
// 那一帧剩下的部分没建，画出来是半张。要下一帧才完整（实测，文档没写）。
func (s *Shell) onPanic(r any) { s.onPanicAt("界面", r) }

// onPanicAt 是 `onPanic` 加上「崩在哪一块」。
func (s *Shell) onPanicAt(where string, r any) {
	s.crash = s.newCrashInfo(where, r)
	log.Printf("biu: 界面崩溃（%s / %s），已切到错误页\n%v\n%s",
		s.crash.where, s.crash.title, r, debug.Stack())
	s.wake.invalidate()
}

// newCrashInfo 收一次崩溃的现场。
//
// ⚠️ **在哪一页从 `router` 上读，而不是让调用点报上来**：崩的那一帧 `router`
// 上就是当时在看的页，而每一处调用点各自报一遍的话，早晚会有一处忘了报——而
// 「忘了报」在界面上看起来和「崩在首页」一模一样。
func (s *Shell) newCrashInfo(where string, r any) *crashInfo {
	path := s.router.Location()
	rt := lookup(path)
	return &crashInfo{
		where: where,
		path:  path,
		title: rt.Title,
		got:   r,
	}
}

// viewMain 是正常的那一帧：上导航 + 左边栏 + 路由页 + 播放条，浮层上是播放列表
// 抽屉（开着的话）和右下角的提示。
func (s *Shell) viewMain(c *ui.Context) {
	// 滚动条的悬停状态交接：上一帧量到的这一帧用，这一帧重新量（各容器在
	// `viewMain` 后半段把它们建出来）。必须排在 `applyFrameTheme` 之前。
	s.scrollbarHot, s.scrollbarHotNow = s.scrollbarHotNow, false
	// 🔴 铁律二：每帧第一件事就是装主题，而且必须在最开头——SetTheme 只影响
	// 它之后创建的控件。不装的话 MyGo 在帧开头重置好的那份跟随系统的主题
	// 就是这一帧真正用的那份，用户选了深色也只在这一帧是对的。
	s.applyFrameTheme(c)
	// 应用内快捷键：登记这一帧窗口要处理哪些组合键，同时也就知道了这一帧有没有
	// 键打中（`Context.Shortcut` 命中就是「按了」）。见 shortcut_dispatch.go。
	//
	// 排在主题之后、其它每帧钩子之前：它自己不画任何东西，而放在最前面的话
	// 「按了快捷键 → 顺带这一帧才生效」这件事和主题那条铁律不会互相干扰。
	s.viewShortcuts(c)
	s.reportSettingsError()
	// 收「那个 500ms 防抖的钟要不要醒着」。**每帧一次**，所以用户切走时它一定
	// 会跟着停（见 syncSearchWatch）。
	s.syncSearchWatch()
	// 更新器留给界面的那一句提示（后台查到了新版本之类），取走就没了。
	s.reportUpdateNotice()
	// 「刷新数据」那一趟回来的话（成了几个 / 为什么没成 / 已经在刷了）。
	// 同样每帧取一次，取走就没了——理由见 reportFavoritesRefresh。
	s.reportFavoritesRefresh()
	// 启动时自动导入的结果，第一帧弹一条 toast 说一声。
	//
	// 为什么非弹不可：任务要求「凭据可以不迁，**要提示重新登录**」。那句提示只在
	// 设置页「配置」那一段里，而用户不会主动去那儿——他在启动时什么也没点，
	// 然后发现要重新登录，而界面上从头到尾没提过发生过一次导入。
	//
	// 排在 `syncSearchWatch` 之后：这一句会 `addToast`，而上面那一句收的是
	// 「搜索页那个钟」的状态。两句互不相干，先后无所谓，但**都必须在
	// `s.app.Snapshot()` 之前**——那一行拿的是这一帧要画的播放状态。
	s.announceMigration()

	// ⚠️ **这一帧只取一次快照**：底层那一层（`drawUnderlay`）和满屏播放器
	//（`viewFullPlayer`）看的是同一份播放状态——两次取的话两者之间播放状态可能
	// 变了，而那一帧画出来的是两份不同的状态。
	st := s.app.Snapshot()

	// 🔴 **满屏开着的时候底层那一层不画，但每帧的副作用照跑**（票 #161）。
	// 两件事拆开在 `underlay.go` 那一层：这一句跑副作用，下面那句画。
	//
	// ⚠️ **顺序不能反**：副作用在前、画在后——画的那一帧要看到副作用刚落下
	// 的状态（后台的结果、刚换好的查询）。反过来的话每一帧画的都是上一帧的
	// 状态，而用户在满屏里待久了之后关掉它，看到的是「慢了一帧」的那一份。
	s.syncUnderlay(c)
	s.drawUnderlay(c, st)

	// 抽屉是浮在侧栏和页面**上面**的一层，不是主布局里的另一块。Biu 那边它是
	// 右侧浮出来的 Drawer（`music-playlist-drawer/index.tsx` 的
	// `data-[placement=right]`），底下那一层页面照旧看得见、点得着；塞进上面
	// 那个 Row 里就成了「窗口窄一块」，页面被挤着走——那不是抽屉。
	// ⚠️ **全屏开着的时候这一处不建**：满屏底栏上那一枚「播放列表」要开的是
	// 同一个抽屉，而抽屉必须画在满屏**上面**（后建的浮层在上）。所以全屏那一支
	// 在 `viewFullPlayer` 末尾自己建（`fullplayer.go`），这里让开——两处都建的话
	// 同一个视图会在同一帧里建两遍，行 key 撞上（Tester 里当场 panic）。
	if !s.full.open {
		s.guardAt("播放列表抽屉", s.viewQueueDrawer)(c)
	}

	// 收藏夹管理那几个弹窗（新建 / 修改 / 选封面 / 裁剪 / 选收藏夹）。
	//
	// ⚠️ 排在**最后**：它们是模态的，要盖住整页；而外面这一层 Guard 不可省
	//（铁律四）——view 里 panic 会穿透 Cocoa 事件循环杀掉整个进程。
	s.guardAt("收藏夹弹窗", s.viewFolderDialogs)(c)

	// 全屏播放器排在收藏夹弹窗**之后**、Toast **之前**：它盖住整个窗口，
	// 而提示要浮在最上面（右下角那句话在满屏里也得看得见）。
	//
	// ⚠️ **递的是这一帧开头取的那份快照**（`st`），不是满屏自己再取一份——
	// 见 `viewFullPlayer` 的注释。
	s.guardAt("全屏播放器", func(c *ui.Context) { s.viewFullPlayer(c, st) })(c)

	// 歌词搜索与预览那两个弹窗（lyricssearch.go）。**排在满屏之后**：它们是模态
	// 的、要盖住整页，而满屏盖住的是整个窗口——次序反过来的话用户在满屏里按到
	// 那个旋钮会看见弹窗开在满屏底下。Toast 仍然在最上面（采用歌词之后那句
	// 「已经换上这一版」要从预览弹窗上浮过去）。
	s.guardAt("歌词弹窗", s.viewLyricDialogs)(c)

	s.viewToasts(c)

	// 记下这一帧停在哪条路由上（下一帧的「刚进这一页」就是拿它比出来的）。
	//
	// ⚠️ **必须排在最后**：页面是在 `s.viewRoutes(c)` 里画的，而那一段要用
	// 「上一帧在哪条路由」来判断「这一帧是不是刚进这一页的第一帧」——所以
	// 这个数要等到整帧画完才更新。
	s.lastPath = s.router.Path()

	// ⚠️ **离开设置页就把「指着的那一行」收掉**：那根手指是给「刚从下载记录页
	// 跳过来」的人看的（见 goToDownloadDirSetting），留着的话，用户下次自己
	// 从侧栏进设置页，「下载目录」那一行的标题还是主色——他没跳过来过。
	//
	// 排在最后是安全的：这一帧的页面已经画完了（那一格已经被指着画过一次），
	// 而设置页自己那一格是**切走**才清的（见 syncSettings）。
	if s.lastPath != settingsPath {
		if s.settings.focus != settingsFocusNone {
			s.settings.focus = settingsFocusNone
		}
		// ⚠️ **那 16 像素的留白也别留着**：它是给「刚跳过来」这件事的——用户
		// 跳过来之后立刻走掉的话，下一次进设置页时它补在别的地方，页面就无故
		// 差了 16 像素（见 syncSettings）。
		s.settings.focusMargin = false
	}
}

// pageJustShown 报「这一帧是刚进这一页的第一帧」。
//
// 路由切走再切回来时，框架会把焦点还给「离开这一页时有焦点的那个元素」
// （`Router.focusPage`，`mygo@v0.6.2/ui/router.go:726`）。收藏夹/local 那一页
// 的「会展开的搜索」正好是那个元素——于是用户回来的时候，键盘焦点被塞进一个
// 他没打开过的框里，他在别处敲的字就落进那里（评审第 5 条）。
//
// 所以刚进这一页的那一帧，**那个输入框先不建**：没有它，框架就没有焦点可还
// （下一帧它照常展开，词还摆在那儿）。
func (s *Shell) pageJustShown() bool {
	return s.lastPath != s.router.Path()
}

// viewFolderDialogs 画收藏夹管理那几个弹窗，**并收掉后台回来的结果**。
//
// 每帧开头**先**收结果再画：`TakeFolderResult` 是「取走而不是读」，取过就不再
// 有，而收结果这一趟会改 `submitting`——先画后收的话，界面上会有一帧是
// 「提交完了但按钮还在转」，而用户看到的就是「点了没反应」。
func (s *Shell) viewFolderDialogs(c *ui.Context) {
	s.flushFolderResults()
	s.flushCoverPick()
	s.syncFolderDialogs()

	s.viewFolderEdit(c)
	// 裁剪弹窗画在编辑弹窗**外面**（不是里面）：`ui.Modal` 自己带一层遮罩，
	// 嵌在另一个 Modal 的面板里的话遮罩只有面板那么大，裁剪台周围那一圈点不
	// 到——而那一圈正是「点外面想关掉」的地方。
	if e := s.folderEdit; e != nil && e.crop.open {
		s.viewCoverCrop(c, &e.crop)
	}
	s.viewFolderSelect(c)
	// 侧栏收藏夹的「删除」二次确认（sidebar_favorites.go）。同样排在这里：
	// 它是模态的，要盖住整页，而侧栏是折叠的时候就不画东西的那一块。
	s.viewFavoriteDelete(c)
}

// syncFolderDialogs 是那些「每一帧都要问一次后台」的地方。
//
// ⚠️ **在 flushFolderResults 之后调**：列表换掉之后 `flushFolderResults` 才拿得到
// 刚回来的那一批勾选状态，反过来的话这一帧的勾是按旧列表算的。
func (s *Shell) syncFolderDialogs() {
	if f := s.folderSelect; f != nil {
		s.syncFolderList(f)
	}
}

// viewCrash 是崩溃后那一帧。Biu 原来的 ErrorBoundary 也是这么做的：程序活着，
// 出事的那块换一张说明页。
//
// # 这一页要说的三件事，少一件都不算「能看的错误页」
//
//  1. **出在哪儿**：哪一块（侧栏 / 队列抽屉 / 页面「下载记录」）和当时在看哪一页。
//     这一句是用户拿去跟开发者说的第一句，也是这一票要的（issue 126：「出错时
//     能看到出错在哪一步」）。它下面那一行原始错误是第二句。
//  2. **插画**：上游那一页中间摆一张 480 宽的 `error.svg`
//     （`components/error-fallback/index.tsx:11-27`），而我们原来只有两行字。
//     「一片白」和「一张说明页」在用户那边差得远——他说不清出事了，而这里
//     一眼就看得出来。
//  3. **走得出去**：重新载入（回同一页）、回到首页、以及**反馈**——上游那枚
//     描边按钮把用户带到 GitHub issues。没有反馈的话用户只能重启然后在心里
//     骂一句。
//
// ⚠️ **不装主题的深色下会亮成一张白纸**（和错误页同一段：它是崩溃后那一帧，
// 而 `View` 在崩着的时候不会先走 `viewMain`，那个开头装主题的一步没跑过）。
func (s *Shell) viewCrash(c *ui.Context) {
	s.applyFrameTheme(c)

	t := c.Theme()
	cr := s.crash
	ui.Column(c).Fill().Center().Gap(space.Px10).Padding(space.Px24).Children(func() {
		// 上游那张 480 宽的插画。它是 `currentColor` 的（见 art/error.svg
		// 顶上那一段），所以深浅两套主题里它各自是「在这套底色上看得清」的
		// 那个色——不给它定死颜色是既定行为，和图标那一批同一条理由。
		if v := errorArt; v != nil {
			ui.Image(c, v).Height(errorArtHeight)
		}
		// ⚠️ **字重不写**（400）：标题那一档是 400（DESIGN.md §二）。
		ui.Text(c, "界面出错了，播放器已经停在这一页").FontSize(scale.Font(c, scale.Size18))
		// 「出错在哪一步」这一块：三行，各自说清一件事。
		ui.Column(c).Key("crash-where").Gap(space.Px2).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "出错的地方："+cr.where).FontSize(scale.Font(c, scale.Size13)).TextColor(t.Text)
			// 路径和页面名分开两行：用户在地址里看到的是路径，而跟开发者
			// 说的时候说的是页面名（「下载记录那一页」）。只给一个的话，另一
			// 边的人听不懂。
			// ⚠️ **路径空着的时候整句不画**：路由器上还没有路径（测试里
			// 手摆的崩溃现场就是这样），而「当时在看的路径：」这一句后面
			// 空着，比不说更让人以为程序自己也不知道自己在哪儿。
			switch {
			case cr.path == "":
			case cr.title == "" || cr.title == notFound.Title:
				// 404 那一页没有「页面名」可言（`notFound` 的 Title 是兜底
				// 的），只说路径。
				ui.Text(c, "当时在看的路径："+cr.path).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
			default:
				ui.Text(c, "当时在看的页面："+cr.title+"（"+cr.path+"）").
					FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
			}
		})

		ui.Textf(c, "%v", cr.got).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).MaxLines(3)
		ui.Text(c, "细节已经写进日志。点下面的按钮再试一次。").FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
		ui.Row(c).Gap(space.Px10).Justify(ui.Center).Children(func() {
			// 反馈是**描边**的那一枚、主操作是实心的那一枚：抄上游的
			// `components/error-fallback/index.tsx:11-27`（「反馈」描边去
			// GitHub issues、「回到首页」主色）。
			if ui.Button(c, "反馈").Clicked() {
				s.openFeedback(c)
			}
			if ui.PrimaryButton(c, "回到首页").Clicked() {
				s.navigate("/")
			}
			// 崩的是某一页，不一定是首页。得给一条走得出去的路，
			// 不然错误页里连侧栏都点不到，只能干看着。
			if ui.Button(c, "重新载入这一页").Clicked() {
				s.crash = nil
			}
		})
	})
}

// errorArt 是错误页中间那张插画。
//
// 它和图标那一批**不是一回事**：图标走 `ui.Icon`（画成当前文字色的遮罩），
// 而这张是一张有自己灰度层次的插画，所以走 `ui.Image` 画它自己的颜色。
// 它照样**不写死颜色**——`art/error.svg` 用的是 `currentColor` 加几档透明度，
// 于是它跟着主题走（`drawSVG` 的 current 参数就是祖先的文字色）。
var errorArt = func() *ui.SVG {
	b, err := artFile.ReadFile("art/error.svg")
	if err != nil {
		// 编译进去的文件读不到 = 有人在重构里挪了目录，而那要在第一次启动
		// 就炸出来，而不是画出一张空白的错误页。
		panic("ui: 错误页那张插画读不到（art/error.svg）：" + err.Error())
	}
	return ui.MustParseSVG(b)
}()

// errorArtHeight 是那张插画在错误页上的高度。
//
// ⚠️ **按高度不按宽度**：上游那张是 480 宽，而窗口宽度是变的（900 ~ 1440），
// 按宽度摆的话窄窗口上那张插画会把三行字挤到屏幕外面去。按高度摆它在任何
// 窗口宽度上都占同样的一行，而它的宽高比（480:220）自己会把宽度收在 187 左右
// ——那正是「一张插画」该有的尺寸。
const errorArtHeight = 96

// openFeedback 把用户送到 issue 区——和头像下拉里那个「问题反馈」是同一个地址
// （`navbar_avatar.go` 的 `feedbackURL`）。
//
// ⚠️ **用 `c.OpenURL` 而不是 `Window.Open`**：后者收到 nil 会直接把进程带走
// （见 navbar_avatar.go 那一段注释），而这一页是在崩溃之后画出来的，那一刻
// 什么都是不可信的。
//
// ⚠️ 它**同时记一笔**（`lastFeedbackURL`）：无头 Tester 里 `OpenURL` 走不了事件
// 循环，于是「点得动」这件事除了「按钮在」之外一句都验不到。记一笔是给测试的
// 那一件证据，生产上多写一个字段的代价可以忽略。
func (s *Shell) openFeedback(c *ui.Context) {
	s.lastFeedbackURL = feedbackURL
	c.OpenURL(feedbackURL)
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
