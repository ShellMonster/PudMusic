// 本地音乐页（Biu 的 `/local-music`，`src/pages/local-music/index.tsx`）。
//
// ## 这一版摆什么
//
// Biu 那一页上摆的东西这里都有：目录筛选、搜索、播放全部、每行的操作
// （打开文件夹 / 删除文件）、刷新，加上一个 Biu 没有的**展示样式切换**
// （PLAN 阶段 8.4 点的 G-19）。
//
// ## 三种样式为什么是这三种
//
// 	表格  Biu 那一页的本体。七个列：序号、标题、大小、格式、时长、创建时间、操作。
// 	      本地音乐有五列元信息，卡片放不下，表格是唯一能把它们并排摆开的地方。
// 	列表  复用 `widgets/list` 的音乐卡片（封面 + 标题 + 副标题）。
// 	      本地文件没有封面，所以那一格是占位——这和 Biu 一样（见 `app.State.CoverURL`）。
// 	网格  同一个卡片，一行几首。列数按 Biu 那四个断点分档换（`widgets/list` 的 grid.go）。
//
// 三样都是**同一份数据的三种画法**，切换不重新扫也不重新读盘：筛选后的那一批
// 每一帧重算，三个视图各读自己那一份。
//
// 🔴 铁律二：本文件里的 view **不许**调 `SetTheme`（主题由 `viewMain` 在每一帧
// 开头装好，放到切换按钮的回调里只调一次会被框架静默丢掉）。
//
// 🔴 铁律四：表格的 `ListState.Key` 每一帧按 `Track.ID` 装。不装的话前面插一
// 行（加一个目录就会在前面多出一整段）会让后面所有行的状态整体错配，界面上
// 表现为「选中项变成旁边另一首」。
//
// 🔴 铁律三：后台扫描的结果到了要叫醒**所有画过这一页的窗口**（见 `invalidate`）。
// 只唤醒主窗口的话另一个窗口停在旧帧。
//
// 页面级的 recover 在 `router.go` 的 `Shell.page` 上，和其余 14 条路由共用
// 那一道 Guard，这里不用再包一层；这一页自己的 view 仍然包一层
// （`guard.Wrap`，建的时候包一次）——分页列表也是这么做的。

package ui

import (
	"log"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/service/localmusic"
	"github.com/ShellMonster/Biu_Go/internal/ui/guard"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scrollbar"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// localMusicPath 是本地音乐那条路由，和 routes 表里登记的是同一个字符串。
//
// 拿来和 `Route.Match` 比，而不是拿 `routes[4]`：那条表一改顺序，这里跟着
// 错，而且错得很安静——这一页退回占位页，没人看得出来（和 settings_page.go
// 的 `settingsPath` 同一个理由）。
const localMusicPath = "/local-music"

// allDirs 是目录筛选里「全部目录」那一档的**显示名**。
//
// 它是一个目录名撞不上的字符串：Biu 用的是 `"all"`
// （`local-music.ts:28`，`useState<string>("all")`），这里照它。
const allDirs = "全部目录"

// # 这一页自己的档和设置里的「数据显示样式」怎么共存
//
// 这一页原来自己有三档（表格 / 列表 / 网格）。设置里那个「数据显示样式」管的也是
// 「一行一首还是一行几首」——**两个控件抢同一件事，就是「别打架」要防的那种**：
// 用户在设置里选了「网格」、这一页还停在「列表」，他不知道自己该看谁，于是两个都
// 像是坏了的。
//
// 分工改成一条线：
//
//	这一页的开关管「表格还是歌曲」——表格那一档画的是**文件**（大小 / 格式 / 创建
//	时间），它和「一行一首还是一行几首」根本不是一件事，全局设置对它没有话说；
//	设置里的「数据显示样式」管「歌曲摆成什么样」——列表 / 紧凑 / 网格。
//
// 所以这一页的开关从三档改成两档，而**选了「歌曲」之后摆成什么样完全由全局设置
// 说了算**（`p.viewStyle()`，它转手问 `Shell.displayView`）。这不是把一档功能删掉：
// 列表 / 网格 / 紧凑三样一个不少，只是「在哪选」从这一页挪到了设置里——而那一处
// 本来就是它们的家。
//
// ⚠️ **表格那一档仍然不听紧凑**：它一行里是文件大小、格式、创建时间，没有封面也
// 没有 UP 名，「紧凑」对它没有可说的事。

// localStyleCount 是展示样式有几档。
//
// ⚠️ 用它定数组长度、给 `ui.Segmented` 的标签表也是同一份长度，不是两处各
// 写一个数：少写一档而 switch 还按三档走，缺的那一屏画的是零值。
const localStyleCount = 2

// localStyles 是那两档的名字，从左到右。
var localStyles = [localStyleCount]string{"表格", "歌曲"}

// 两档的下标。**表格是默认那一档**，因为它是 Biu 那一页的本体。
//
// ⚠️ 第二档叫「歌曲」而不是「列表」：它画的是全局设置选中的那一种，摆成网格也
// 叫「歌曲」。叫「列表」的话用户切到网格那一档之后，界面上写着「列表」而画面是
// 网格——两个控件说两件事，正是这一段开头要防的那种打架。
const (
	localStyleTable = iota
	localStyleSong
)

// searchLocalLabel 是这一页搜索框的名字，兼做它空着时画的那几个字。
//
// 和顶栏那个搜索框（`navbar_search.go` 的 `searchLabel = "搜索"`）**不是**
// 同一个：这一页搜的是已经扫到本地的歌，那一句顶栏搜的是 B 站。
const searchLocalLabel = "搜索标题"

// localColumns 是表格那一档的列。宽度照 Biu 的
// `grid-cols-[40px_minmax(0,1fr)_100px_100px_100px_100px_40px]`
// （`src/pages/local-music/index.tsx:206`）：序号那一格 40、其余各 100、
// 标题吃掉剩下的。
//
// ⚠️ 最后一格是「操作」。Biu 那一格是 40（一个「⋯」按钮），我们摆两个按钮
// 所以给宽一点——摆不下的话它们会被挤到下一列去，用户点到的就不是他以为的
// 那个按钮。
var localColumns = []ui.TableColumn{
	{Title: "#", ID: "n", Width: 44, Align: ui.Center},
	{Title: "标题", ID: "title", MinWidth: 160},
	{Title: "大小", ID: "size", Width: 96, Align: ui.End},
	{Title: "格式", ID: "format", Width: 88, Align: ui.End},
	{Title: "时长", ID: "duration", Width: 88, Align: ui.End},
	{Title: "创建时间", ID: "created", Width: 140, Align: ui.End},
	{Title: "", ID: "ops", Width: 176, Align: ui.End},
}

// deleteDialogTitle / deleteDialogBody 是删文件那道确认上的两句话。
//
// ⚠️ **文案照抄 Biu 上游**（`local-music.ts:175-176`）：title 是 `删除文件`，
// description 是 `该操作会删除本地文件且不可恢复，请谨慎操作`，确认按钮 `删除`。
//
// 第二句一个字都没改是有理由的：它是用户按下按钮和文件真的没了之间**唯一**
// 的那道告知，而它是照着 Biu 校对过的措辞。换成一句更客气的说法（比如
// 「确定要移除吗」）会把这件事说得比实际轻。
const (
	deleteDialogTitle = "删除文件"
	deleteDialogBody  = "该操作会删除本地文件且不可恢复，请谨慎操作"
	// deleteConfirmLabel 是**弹窗里**那个确认按钮（Biu 是 `confirmText: "删除"`）。
	//
	// ⚠️ **它排在最后只是抄上游的写法**（上游的 `confirmText` 是最后一个）；
	// 那个框现在是**应用自绘**的（`confirmModal`，票 #176），而自绘的那一份
	// **不自动聚焦**（系统原生那个 `ui.AlertDialog` 会把最后一个按钮当主按钮
	// 并自动聚焦，`mygo/ui/feedback.go:198`）——聚焦那颗**不可逆**的键上，
	// 回车就等于直接删掉，而这里是自绘的，不聚焦反而更稳。
	deleteConfirmLabel = "删除"
	// deleteRowLabel 是**行里**那个按钮的名字。
	//
	// ⚠️ 它**不能**也写成「删除」：那一行和弹窗上会同时有两个同名按钮，
	// 读屏软件念出来分不清是哪一个，测试里 `Click("删除")` 命中的也是前一个
	//（行里的那个）。Biu 那边行里的菜单项本来就叫「删除文件」
	//（`pages/local-music/item.tsx:36`），我们照它。
	deleteRowLabel = "删除文件"
	// deleteCancelLabel 是取消那一个。
	deleteCancelLabel = "取消"
	// removeDirTitle / removeDirBody 是「移除目录」那道确认。
	//
	// ⚠️ body 照 Biu（`local-music.ts:160`）：
	// `仅移除列表中的目录，不会删除本地文件`。这一句是**必须**的——
	// 「移除目录」和「删除文件」在同一个页面上，用户分不清的时候看到这一句
	// 才不会以为自己的歌没了。
	removeDirTitle = "移除目录"
	removeDirBody  = "仅移除列表中的目录，不会删除本地文件"
)

// 弹窗里各行之间的距离。
//
// ⚠️ **它现在只服务更新弹窗那一处**（`update_dialog.go`）：确认框的间距在
// `confirmmodal.go` 的 `confirmGap` 那一处。
const dialogGap = space.Px12

// localPage 是本地音乐这一页自己的状态。建一次，每一帧调 View。
type localPage struct {
	// draw 是外面包了一层 recover 的 view（见 View）。
	draw func(*ui.Context)
	// paint 是真的画法，draw 调它。
	//
	// ⚠️ 它是一个**字段**而不是构造时就把方法值包进去：这样测试能把它换成
	// 一个会崩的画法，验「这一页自己包的那一道 recover」接不接得住
	// （`localmusic_test.go` 的 `TestAPanicInThisPageDoesNotKillTheProcess`）。
	// 生产上它永远是 `(*localPage).view`。
	paint func(*ui.Context)

	app    *app.App
	cache  *imgcache.Cache
	acts   list.Actions
	notify func(string, notice.Tone)
	// pickDir 弹系统框选一个文件夹。它必须从**后台 goroutine** 调，所以
	// 调用方（`addDirectory`）自己起 goroutine，这里只管调。
	pickDir func() (string, error)

	// ── 下面这一摊只由界面线程读写（铁律一）──────────────────────────────

	// table 是表格那一档的 `ui.ListState`。**滚动位置和选中行就跟着它走**，
	// 所以它建一次、存在这里，不每一帧新建。
	table ui.ListState
	// sel 是表格里选中的那一行，-1 是没选。框架把点中的下标写进来。
	sel int
	// selID 是选中的**那一首**的身份（铁律四：行身份是 ID 不是下标）。
	//
	// ⚠️ **那一首暂时不在列表里时也留着**：重扫期间列表是空的（`st.Tracks` 空的），
	// 那一帧按身份找不到它，但等它回来还得选中它——所以身份和下标分开记。
	selID string
	// lastSel 是上一帧框架的游标下标，用来判「**它挪了没有**」（点了哪一行 /
	// 方向键走到哪一行）。理由见 `view` 里那三句。
	lastSel int
	// keys 记「现在是键盘在管还是指针在管」（焦点环靠它，理由见
	// `widgets/list/selection.go` 的 KeyboardMode）。
	keys list.KeyboardMode
	// tableEl 是表格那个元素（画焦点环要拿它的盒子）。
	tableEl ui.Element
	// cursorCell 是**游标那一行**序号那一格的盒子：它撑满那一格（见
	// `viewRowNumber`），所以它的盒子就是那一行的位置——焦点环按它算。
	cursorCell ui.Element
	// showCursorRing 报「要不要给游标那一行画焦点环」。
	showCursorRing bool
	// tableSel 是表格那一档的「选择集」，**它永远是空的**——而它正是那一块实心
	// 主色底消失的原因（票 #197）。
	//
	// ## 为什么要有它
	//
	// 框架给**选中的行**铺一整块实心主色底，判据是 `f.chosen(i, key)`，而它问的
	// 是「这一行在**选择集**里吗」（`mygo/ui/list.go`）：
	//
	//	if sel := f.s.Selection; sel != nil { return sel.has(k) }
	//	return *f.s.Selected == i
	//
	// 也就是说：**装了选择集，就不问单选的那一份**。所以给表格装一份**永远是空的**
	// 选择集，框架就不画那一块主色底——而点击、方向键、双击全都照旧（它们走的是
	// `Selected` 和游标，不是选择集）。
	//
	// ## 为什么盖不掉
	//
	// 列表那一行能盖（那一行是我们自己的元素，铺满整行，见
	// `widgets/list/selection.go`），表格这一行**盖不掉**：框架把主色刷在**行元素**
	// 上，而格子四周那一圈内边距（`mygo/ui/table.go` 的 `Padding(Space(1.5),
	// Space(2.5))`）是行元素的地盘——我们画的每一格都在那圈缝里面。
	//
	// ⚠️ **每一帧都要清空它**：框架点一行时会往选择集里放那一份（`f.choose`），
	// 放着不管，下一帧就又画上实心主色底了。
	tableSel ui.Selection[string]

	// cards 是「歌曲」那一档的控件（`widgets/list` 的 `SwitchView`：列表 / 紧凑 /
	// 网格三个摆法在它里面分发，见文件头那一段）。它自己记着滚动位置，所以建一次
	// 存着。
	cards *list.SwitchView

	// displayView 报「这一帧歌曲该摆成什么样」。**由 Shell 传进来**而不是这一页
	// 自己去问设置：这一页只有 `*app.App`（它看得到设置），而 `displayView` 是
	// 界面层那个「唯一一处把设置翻译成画法」的地方（`displaymode.go`）——从这一页
	// 再写一份就是第二处。
	displayView func() list.View

	// justShownFn 报「这一帧是刚进这一页的第一帧」（`Shell.pageJustShown`）。
	// 由 Shell 在建完这一页时接上；为零时（单独造这一页的测试）当作「不是」。
	justShownFn func() bool

	// tip 是带延迟的悬停提示（tooltip.go）。由 Shell 在建完这一页时接上；
	// 为零时（单独造这一页的测试）退回框架那枚。
	tip tipFunc

	// noteScrollbar 把「指针压在滚动条上」转告给壳（主题下一帧据此换深一档，
	// 见 scrollbar.go）。由 Shell 在构造之后接上；为零时（测试里单独造这一页）
	// 什么也不做。
	noteScrollbar func(bool)

	// style 是当前展示样式，下标对着 localStyles。
	style int
	// dirLabel 是目录筛选里选中那一档的**显示名**（不是路径，见 dirLabels）。
	dirLabel string
	// search 是搜索框里正在敲的字，searchBox 是那一枚会展开的搜索收着还是开着。
	search    string
	searchBox searchButtonState

	// tableBox 是**这一帧**包着表格的那一层，只用来判「指针在不在表格上」。
	//
	// 表格每一行的序号要换成播放图标（上游 `item.tsx:57-64` 的 `group-hover`），
	// 而 `ui.Table` 不把「哪一行被 hover」露出来（`cell` 回调只给行列号）。
	// 所以这一格拿**上一帧的布局**自己判：表格那一层的 `PointerPosition()` 说
	// 指针在不在表里，序号那一格的 `PointerPosition()` 给出指针在行里的纵向
	// 位置。见 `viewRowNumber` / `tableRowHovered`。
	//
	// ⚠️ **它每一帧在建表格之前重新赋值**：`ui.Element` 属于建它的那一遍，
	// 跨帧留着的话下一帧 `Valid()` 就是 false（框架的「keep a ui.Handle
	// instead」那一条）。这里只在**同一帧**里用，所以没问题。
	tableBox ui.Element

	// askOpen 是删文件那道确认开着没有，askTrack 是它要删的那一项。
	askOpen  bool
	askTrack localmusic.Track
	// rmOpen 是移除目录那道确认开着没有，rmPath 是它要移的那个路径。
	rmOpen bool
	rmPath string

	// rows 是**筛选之后**那一批，表格、卡片、播放全部都读它。
	// 每一帧重算（筛选条件是即时状态，重算最简单也最不容易错）。
	rows []localmusic.Track
	// songs 是 rows 转成卡片控件要用的形状。
	songs []list.Music

	// started 是「第一遍扫描发起过了没有」。
	started bool

	// ── 跨 goroutine 的只有下面这两小摊 ──────────────────────────────────

	// wakes 是**每个画过这一页的窗口**一条（见 invalidate）。
	wakeMu sync.Mutex
	wakes  []localWake
	frame  uint64
}

// localWake 是一个窗口的唤醒句柄。
type localWake struct {
	// svc 是这个窗口的身份（认它用），wake 是叫醒它的动作。
	svc  ui.Services
	wake func()
	// seen 是这个窗口上一次登记时的帧号，用来认出「最久没画的那一个」。
	seen uint64
}

// maxLocalWakes 是记着的唤醒句柄上限：一个窗口一条，多出来的挤掉最久没画的
// 那个（它已经不画了，句柄留着是空转）。和 `widgets/list` 的那一份同值。
const maxLocalWakes = 4

// newLocalPage 建这一页的状态。
//
// a 是应用状态（这一页读它的库）、cache 是封面缓存（整个窗口共用一个）、
// acts 是操作、notify 是说一句话的地方、pickDir 是选文件夹。
//
// ⚠️ 五样都要传：卡片那两档要走封面缓存和操作，「删掉了/放不了」这些话只有
// 一个出口（Toast），而「添加目录」没有它就进不去任何一个目录。
func newLocalPage(a *app.App, cache *imgcache.Cache, acts list.Actions,
	notify func(string, notice.Tone), pickDir func() (string, error), displayView func() list.View) *localPage {
	p := &localPage{
		app: a, cache: cache, acts: acts, notify: notify, pickDir: pickDir,
		displayView: displayView,
		sel:         -1, dirLabel: allDirs,
	}
	// 这一份建一次、记着自己的滚动位置（见 struct 上那段）。它内部是一对控件
	//（列表 / 紧凑 / 网格），摆法由设置决定，见文件头那一段。
	p.cards = list.NewSwitchView(cache, acts)
	p.cards.SetEmpty("") // 空状态由这一页自己说，见 viewEmpty
	// 外面包一层 recover：view 里 panic 会一路穿出 Cocoa 事件循环把整个
	// 进程带走。建的时候包一次就够（每帧包一次是白花开闭包）。
	p.paint = p.view
	p.draw = guard.Wrap(func(c *ui.Context) { p.paint(c) }, p.onPanic)
	return p
}

// View 画这一页。每一帧调一次。
func (p *localPage) View(c *ui.Context) { p.draw(c) }

// justShown 是「刚进这一页的第一帧」那一问。壳没接上的时候一律当「不是」。
func (p *localPage) justShown() bool {
	if p.justShownFn == nil {
		return false
	}
	return p.justShownFn()
}

// tipWith 是这一页的悬停提示入口：状态挂在 Shell 上（tooltip.go），这里只是
// 把这一页拿到的那份递过去；单独造这一页的测试没有那份，退回框架那枚。
func (p *localPage) tipWith(c *ui.Context, e ui.Element, text string) ui.Element {
	return tipFrom(p.tip, c, e, text, tipDefault)
}

// onPanic 记一笔并要一帧。
//
// 要下一帧是必须的：recover 接住的是 build pass 中间，那一帧剩下的部分没建，
// 画出来是半张（`Shell.onPanic` 补 Invalidate 是同一个道理）。
//
// ⚠️ 这里**不换错误页**：坏的是这一个控件，窗口上别的东西照常用。
func (p *localPage) onPanic(rec any) {
	log.Printf("biu: 本地音乐页画崩了：%v\n%s", rec, debug.Stack())
	p.invalidate()
}

// begin 是每一帧开头做的事：登记唤醒、对一遍设置里的目录、发起第一遍扫描。
// begin 是这一页每帧要做的那些事：登记唤醒句柄、把根目录对齐到设置里那一份、
// 第一遍扫描。
//
// ⚠️ **它跑两遍是安全的，而且就是要跑两遍**：一层是 `syncLocalMusic`（每帧
// 无条件，票 #161），一层是这一页自己的 view。后者有**不经过 `Shell.View` 的
// 调用方**（测试直接画 `localPage.View`，而那一层不走 `syncUnderlay`）。
// 三样各自都幂等：`setWake` 更新的是同一条、`SetLocalRoots` 对得上就立刻返回、
// `RescanLocalMusic` 由 `started` 挡着。
func (p *localPage) begin(c *ui.Context) {
	// 🔴 铁律三：把这一帧的窗口登记进去。后台扫完了要靠它叫醒界面。
	p.setWake(c.Services())

	// 根目录以**设置里那一份**为准。对不上 `SetLocalRoots` 内部会换根并重扫，
	// 对得上它立刻返回（它幂等，所以每一帧调是安全的）。
	p.app.SetLocalRoots(p.app.SettingsSnapshot().LocalMusicDirs)

	// 第一遍扫描。⚠️ **不是**每帧一次：那是几千个文件。
	//
	// 之后的更新有两个来源：文件监听（`app.StartLocalMusicWatch`，由 `ui.Run`
	// 在 `WhenReady` 里调——`mygo.WatchFiles` 在应用没 ready 之前调会 panic），
	// 和用户按「重新扫描」。
	if !p.started {
		p.started = true
		p.app.RescanLocalMusic()
	}
}

// view 是真的画法，View 是它外面那道 recover。
func (p *localPage) view(c *ui.Context) {
	// 🔴 铁律二：主题这一页**不装**——`viewMain` 在每一帧最开头装好了。
	t := c.Theme()

	// ⚠️ **`begin` 跑两遍**（一层是这里，一层是 `syncLocalMusic`）：三样各自都
	// 幂等，而这一页的 view 有**不经过 `Shell.View` 的调用方**（测试直接画它，
	// 而那一层不走 `syncUnderlay`）。理由见 `begin` 那段注释。
	p.begin(c)

	st := p.app.LocalMusicState()
	// 🔴 **选中的那一首跟着**那一首**走**（铁律四）：行换了之后按 ID 重新对位。
	//
	// ⚠️ **为什么自己扛这一件**：框架本来会做（「游标跟着它的项走」，靠
	// `ListState.Key` + `Selected`），但它**在装了选择集的时候就不做了**
	// （`mygo/ui/list.go` 的 sync：那个条件里有 `s.Selection.has(s.selKey)`）——
	// 而我们**要**装一份选择集，正是它让框架不再给选中的行铺实心主色底
	// （票 #197，见 `tableSel`）。两件事只能自己扛一件，所以这里扛「跟着 ID 走」
	// 这一件。
	//
	// 框架的游标挪了没有（点了哪一行 / 方向键走到哪一行）：焦点环靠它（理由见
	// `widgets/list/selection.go` 的 KeyboardMode）。
	//
	// ⚠️ **要在这一页自己改 `p.sel` 之前问**：框架把点中的下标写进来是在**上一帧
	// 画完的时候**，而下面那几行是这一帧开头改的。
	p.keys.Moved(p.sel)
	// 框架的游标挪了（点了哪一行 / 方向键走到哪一行）时，先记住**那一首的身份**——
	// 它按位置记，我们按身份记（铁律四）。
	if p.sel != p.lastSel {
		p.lastSel = p.sel
		if id := p.trackIDAt(p.sel); id != "" {
			p.selID = id
		}
	}
	p.rows = p.filter(st.Tracks, st.Roots)
	p.sel = indexOfTrackID(p.rows, p.selID)
	p.lastSel = p.sel

	// ⚠️ **横向那一格用 `pagePadX`**（七页共用一个数，见 pagerhythm.go）。
	ui.Column(c).Key("page:"+localMusicPath).Grow(1).Padding(space.None, pagePadX).Gap(space.Px10).
		Children(func() {
			p.viewHead(c)
			p.viewToolbar(c, st)
			p.viewWarnings(c, t, st)
			p.viewBody(c, t, st)
		})

	// 两道确认画在内容**之后**：它们是 overlay，位置不影响浮在最上面，
	// 但放最后能保证这一帧的内容已经建完了。
	p.viewDeleteDialog(c)
	p.viewRemoveDirDialog(c)

	// 「回到顶部」那一枚。滚过 400 像素才出现（上游的数，见 `widgets/list` 的
	// `BackToTop`）。
	//
	// ⚠️ **这一页有两档**：卡片那一档（列表 / 紧凑 / 网格）走的是
	// `ScrollOffset`（行高定死，算得准）；表格那一档自己滚，而它内部那个滚动
	// 容器的像素偏移拿不到，所以那一档按行换算（见 `localTableOffset`）。
	if p.style == localStyleTable {
		list.BackToTop(c, p.localTableOffset(), p.localTableTop)
	} else {
		list.BackToTop(c, p.cards.ScrollOffset(), p.cardsTop)
	}
}

// syncLocalMusic 是本地音乐这一页每帧的副作用：登记唤醒句柄、把根目录对齐到
// 设置里那一份、第一遍扫描。
//
// ⚠️ **它原来在 `localPage.view` 的最开头**，而这一页在满屏开着的时候不画
// （票 #161）——不挪的话，用户在满屏里看频谱的那几分钟里，后台扫回来的文件
// 一直没人收，关掉满屏之后那一页还是旧的。
//
// ⚠️ **筛出「这一帧要画的那些行」不在这儿**：那是画的时候要用的数据（画的那
// 一半自己算），而后台扫描的结果落在应用层（`app.LocalMusicState`），关掉
// 满屏之后第一帧重算就是最新的。
func (s *Shell) syncLocalMusic(c *ui.Context) {
	s.local.begin(c)
}

// localTableRowPad 是 `ui.Table` 给每一行的上下内边距（6+6，行高 44 里占 12）。
const localTableRowPad = space.Px6

// localTableRowPx 是本地音乐那一档表格里一行的高：操作那一列那枚键（`iconHit`
// = 32）+ `ui.Table` 给每一行的上下内边距（6+6）。
//
// ⚠️ **它只用来把「顶上那一行是第几行」换算成像素**（回顶按钮的阈值是 400
// 像素）。这一页的表格自己滚，而它内部那个滚动容器的像素偏移拿不到。
//
// ⚠️ **它是量出来的那一个数**（`TestTheTableRowHeightsAreWhatTheOffsetsAssume`
// 钉着）：那一行的高由最高的格子决定（操作那一列的键），改了那一列的字号就得
// 跟着改它。图标键的点击区从 28 长到 32（上游那一档，G39）之后，它从 40 变成
// 44——**回顶按钮的换算也跟着变**，不改的话「滚过了多少」算错，回顶会早几行
// 或者晚几行才出现。
const localTableRowPx = 44

// localTableOffset 是表格那一档滚过了多少像素。
func (p *localPage) localTableOffset() float32 {
	first, _ := p.table.Visible()
	return float32(first) * localTableRowPx
}

// localTableTop 表格那一档回到顶上。
func (p *localPage) localTableTop() { p.table.ScrollTo(0, ui.Start) }

// cardsTop 卡片那一档回到顶上。
func (p *localPage) cardsTop() { p.cards.ScrollTo(0, ui.Start) }

// viewHead 是标题那一行：标题、展示样式切换、添加目录。
func (p *localPage) viewHead(c *ui.Context) {
	ui.Row(c).AlignSelf(ui.Stretch).Center().Gap(space.Px8).Children(func() {
		ui.Text(c, "本地音乐").FontSize(scale.Font(c, pageTitleSize))

		// 展示样式切换。⚠️ 用 `ui.Segmented` 而不是三个按钮：三档互斥、
		// 当前位置一眼可见，三个按钮的话用户得靠猜哪个是按下去的。
		ui.Segmented(c, &p.style, localStyles[:]...)

		ui.Spacer(c)
		// ⚠️ **它旁边那一枚文件夹加的图标**：上游那一枚是「图标 + 文字」的
		// flat 键（`local-music/index.tsx:189`），而 `flat.Button` 只接管文字
		// ——图标一直没画（那一票要的是「把描边和常驻底去掉」，不是重画按钮）。
		//
		// 于是它在画面上就是**一行小字**，而空状态那句话让用户去点它：按提示
		// 找过去，扫到右上角只看见一行说明文字，**很可能以为那不是能点的**
		//（报告 C2：用户于是以为要去顶栏找「打开文件」，而那是另一件事）。
		//
		// 所以这里把上游那一枚图标补上——**同一个词、同一个动作**，而它一眼就
		// 看得出是个入口。
		//
		// ⚠️ **它仍然是扁平的**（无描边、无常驻底）：那是全应用次级按钮的
		// 规矩（`flatbutton.go` 的文件头），`flatspread_test.go` 逐页钉着。
		// 「看得见」靠的是那枚图标和空状态里那枚实心主色按钮，不是给它描一圈边。
		addDir := flatButton(c, "")
		addDir.Children(func() {
			ui.Icon(c, icon(iconFolderAdd)).FontSize(14)
			ui.Text(c, "添加目录")
		})
		if addDir.Clicked() {
			p.addDirectory()
		}
	})
}

// viewToolbar 是操作那一行。
func (p *localPage) viewToolbar(c *ui.Context, st app.LocalMusicState) {
	ui.Row(c).AlignSelf(ui.Stretch).Center().Gap(space.Px8).Children(func() {
		if len(p.rows) > 0 {
			// 「播放全部」用主按钮：Biu 那一页上它就是 primary
			//（`local-music.ts:212` 的 `color="primary"`）。
			ui.PrimaryButton(c, "播放全部").OnClick(p.playAll)
			// 照上游那一枚 `IconButton`（`local-music/index.tsx:208`，tooltip
			// 「添加到播放列表」）：图标键 + 悬停说出它做什么。
			if p.tipWith(c, flatIconButton(c, iconEnqueue, "全部加入队列", iconMd).
				Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7), "全部加入队列").Clicked() {
				p.queueAll()
			}
		}

		ui.Spacer(c)

		// 搜标题：上游是 `SearchButton`——收起时一枚放大镜，点开才是一个 240 的
		// 输入框（`local-music/index.tsx:236`）。我们这一排放不下 240，展开的
		// 上限沿用原来那个输入框的 180。
		expandableSearch(c, p.tip, &p.searchBox, &p.search, searchLocalLabel, 64, 180,
			p.justShown())
		ui.Select(c, &p.dirLabel, dirLabels(st.Roots)).Width(180)
		// 移除此目录 / 重新扫描：上游是两枚 `IconButton`（`local-music/index.tsx:238,244`，
		// tooltip「刷新」「移除当前目录」）。悬停提示说的就是名字——图标键没有
		// 可见的字，提示是它唯一说得出自己做什么的地方。
		if p.currentRoot(st.Roots) != "" {
			if p.tipWith(c, flatIconButton(c, iconDeleteBin5, "移除此目录", iconMd).
				Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7), "移除此目录").Clicked() {
				p.askRemoveDir()
			}
		}
		if p.tipWith(c, flatIconButton(c, iconRefresh, "重新扫描", iconMd).
			Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7), "重新扫描").Clicked() {
			p.app.RescanLocalMusic()
		}
	})
}

// viewWarnings 把「有目录读不到」和「有格式我们放不了」摆出来。
//
// ⚠️ 第二条是 PLAN §5.4 点名的：**本地音乐格式覆盖不完整**是一个真实的
// 功能缺口，不能含糊过去（上游原话）。把一个 `.wma` 列出来却不说「我们放不了」，
// 用户会以为文件坏了。
func (p *localPage) viewWarnings(c *ui.Context, t *ui.Theme, st app.LocalMusicState) {
	if st.Err != "" {
		ui.Text(c, "有目录读不到："+st.Err).
			FontSize(scale.Font(c, 12)).LineHeight(smallCopyLeading).
			TextColor(t.Danger).AlignSelf(ui.Stretch)
	}
	if names := unplayableFormats(st.Tracks); len(names) > 0 {
		// ⚠️ **浅色下这个警告橙当字色是 2.04:1**（旧的那一档是 3.19:1）——
		// 上游那个色相自带的代价，理由和数都在 `theme.go` 的语义色那一段。
		ui.Text(c, "这个目录里有我们还放不出的格式（"+strings.Join(names, " ")+
			"），它们会列出来但放不了。").
			FontSize(scale.Font(c, 12)).LineHeight(smallCopyLeading).
			TextColor(t.Warning).AlignSelf(ui.Stretch)
	}
}

// viewBody 是内容那一块。
func (p *localPage) viewBody(c *ui.Context, t *ui.Theme, st app.LocalMusicState) {
	switch {
	case len(p.rows) == 0 && st.Scanning:
		// ⚠️ 第一遍扫完之前**不能**说「这里没有歌」：那一屏上什么都还没有，
		// 而扫描还在跑（几百上千个文件的 stat）。说「这里没有歌」是在报一件
		// 还没发生的事。
		//
		// ⚠️ **走 `drawEmpty` 那一个出口**（和这一页的「没有结果」同一套画法），
		// 而这一页的「没有结果」也走它——原来这一页一个手写一个 13pt 淡灰的
		// 「正在扫描…」，一个是左对齐的两行字，用户等的那几秒里那两样对不上
		// （票 #138）。
		drawEmpty(c, "local:loading", loadingState("正在扫描…"), emptyPagePad)
	case len(p.rows) == 0:
		p.viewEmpty(c, st)
	case p.style == localStyleTable:
		p.viewTable(c, t)
	default:
		// 🔴 设置里的「数据显示样式」在这一帧生效（`displaymode.go` 里
		// `displayView` 的注释写了为什么每帧都要调）。这一页的卡片控件是**一对**，
		// 网格 / 紧凑 / 列表在它里面分发（`widgets/list/switch.go`）。
		p.cards.SetView(p.viewStyle())
		p.cards.SetSongs(p.cardsOf())
		// ⚠️ **表头在列表外面**（`rowheader.go` 文件头）：它是 `View` 的兄弟，所以列表
		// 往下滚它不动。网格那一档不摆（判断在 `list.SwitchView.Header` 里）。
		//
		// 表格那一档走的是上面那个 `case`（`ui.Table` 自带表头，`localColumns`），所以
		// 走不到这里来。
		p.cards.Header(c, "localmusic:header")
		p.cards.View(c)
		if p.noteScrollbar != nil {
			p.noteScrollbar(p.cards.Hot())
		}
	}
}

// viewStyle 是这一帧「歌曲」那一档该摆成什么样。
//
// ⚠️ 没装接线时退回列表（`list.ViewOf` 那一支的零值）：这一页有一处测试是直接
// `newLocalPage` 造出来的，没给接线——那时候摆成网格会让那一批断言读到另一个布局。
func (p *localPage) viewStyle() list.View {
	if p.displayView == nil {
		return list.ViewList
	}
	return p.displayView()
}

// viewEmpty 是筛选之后一首都没有时说的那句话。
//
// 分两种，因为「为什么空」和「怎么才能不空」两句话在两种情况下不一样：
// 一个目录都没加过 vs 加了但（筛选之后）没有。
//
// ⚠️ **走 `drawEmpty` 那一个出口**（和全应用其余的空状态同一套画法）：这一页
// 原来是自己写的一列左对齐的字，而这一页的「正在扫描…」也是自己写的一行
// 13pt 淡灰——**同一页上两句话两个样子**，而全应用其余七页的空状态早就是
// 居中带插画的那一套了（票 #138）。
func (p *localPage) viewEmpty(c *ui.Context, st app.LocalMusicState) {
	e := localEmptyState(st)
	// ⚠️ **一个目录都没加过时，这一格带一枚按钮**：空状态那句话让用户去点
	// 右上角那枚按钮，而那一枚离这句话三百多像素、还只是一行小字（报告 C2：
	// 「用户按提示找过去，扫到右上角只看见一行小字，很可能以为那只是说明文
	// 字」）。
	//
	// 所以这里**同一个词、同一个动作**再摆一枚，就在那句话下面——用户读到
	// 哪儿就在哪儿动手。
	if len(st.Roots) == 0 {
		e.Action = &emptyAction{Label: "添加目录", Do: p.addDirectory}
	}
	drawEmpty(c, "local:empty", e, emptyPagePad)
}

// localEmptyState 是这一页空着的时候说的那两句话。
//
// 分两种，因为「为什么空」和「怎么才能不空」在两种情况下不一样：
// 一个目录都没加过 vs 加了但（筛选之后）没有。
func localEmptyState(st app.LocalMusicState) emptyState {
	if len(st.Roots) == 0 {
		return emptyState{
			Title: "还没有添加音乐目录",
			Hint:  "点下面的按钮选一个文件夹，它的第一层里的歌会出现在这里。",
			Icon:  iconEmptyDisk,
		}
	}
	return emptyState{
		Title: "这里没有音频文件",
		Hint: "这一页只列所选目录第一层里的音频文件（" +
			strings.Join(localmusic.Extensions, " ") + "），不会往子目录里翻。",
		Icon: iconEmptyDisk,
	}
}

// trackIDAt 是第 i 行那一首的身份，越界是空串。
//
// ⚠️ **读的是上一帧那一份行**（`p.rows` 在 `view` 开头才换）：框架把点中的下标
// 写进 `p.sel` 是在**上一帧画完的时候**，那时候 `p.rows` 还是旧的——所以这里
// 读到的正是用户点的那一首。
func (p *localPage) trackIDAt(i int) string {
	if i < 0 || i >= len(p.rows) {
		return ""
	}
	return p.rows[i].ID
}

// indexOfTrackID 是 ID 为 id 的那一首的下标，没有是 -1。
//
// ⚠️ **认的是 ID 不是位置**：行换了之后同一首挪了位置（铁律四）。按位置找的话，
// 「插到最前面」之后选中的就变成旁边那一首——而画面上什么异样都看不出来。
func indexOfTrackID(rows []localmusic.Track, id string) int {
	if id == "" {
		return -1
	}
	for i := range rows {
		if rows[i].ID == id {
			return i
		}
	}
	return -1
}

// viewTable 是表格那一档。
func (p *localPage) viewTable(c *ui.Context, t *ui.Theme) {
	// 🔴 铁律四：Key 和 Label 每一帧都重装，装的是 ID（不是序号）。
	//
	// 为什么这一页特别需要：往目录里加一个文件夹，列表前面就多出一整段，
	// 后面所有行的下标全部错位。不按 ID 装的话，选中项会跳到旁边那一首。
	p.table.Key = func(i int) any { return p.rows[i].ID }
	p.table.Label = func(i int) string { return p.rows[i].Title }
	p.table.Selected = &p.sel
	// 装一份**永远是空的**选择集：框架于是不再给选中的行铺实心主色底（票 #197，
	// 理由见 `tableSel` 那一段）。
	p.table.Selection = &p.tableSel
	// ⚠️ **先清空它**：框架点一行时会往里面放那一份（`f.choose`），放着不管下一帧
	// 就又画上实心主色底了。
	p.tableSel.Clear()

	// 外面这一层是「表格的那一块」：`PointerPosition` 拿它判指针在不在表里
	//（见 tableBox 的注释）。它自己什么也不画，尺寸跟着里面的表格走。
	box := ui.Column(c).Grow(1).FillWidth()
	p.tableBox = box
	box.Children(func() {
		// ⚠️ **游标那一行的盒子每帧要重新记**：游标不在画面上时没有那一格，
		// 留着上一次的那个会让环画在老地方。
		p.cursorCell = ui.Element{}
		table := ui.Table(c, &p.table, localColumns, len(p.rows), func(row, col int) {
			p.viewCell(c, t, row, col)
		}).
			Grow(1).FillWidth().
			OnSubmit(func() { p.playRow(p.sel) }).
			// ⚠️ **听一听输入**：只为了知道「上一次是键盘还是指针」（焦点环靠它，
			// 理由见 `widgets/list/selection.go` 的 KeyboardMode）。
			//
			// 返回 false 是刻意的：自己不吃这个事件，方向键挪游标、点击选行、双击
			// 播放都还在。
			HandleInput(p.keys.Input)
		p.tableEl = table
		// 焦点环画不画：**键盘在管**而且**表格有焦点**。
		//
		// ⚠️ **表格有焦点是第二半**：方向键挪的是表格的游标，焦点不在表格上时按
		// 方向键挪不动它（挪的是别的东西），那一行也就不是「键盘走到的那一行」。
		//
		// ⚠️ **要在表格建好之后才算**：句柄是建表格的时候绑的（`mygo/ui/list.go` 的
		// buildList），之前问「表格有没有焦点」问的是上一帧那个元素。
		p.showCursorRing = p.keys.On() && list.Focused(c, &p.table)
		// 游标那一行的**焦点环**。
		//
		// ⚠️ **画在表格自己的空间里**：格子四周那圈内边距（`mygo/ui/table.go` 的
		// `Padding(Space(1.5), Space(2.5))`）是**行元素**的地盘——画在格子里会被
		// 裁掉，而框架把主色刷在行元素上（那一块实心主色底票 #197 去掉了）。
		table.DrawOver(func(cp *ui.Painter, r ui.Rect) {
			p.drawCursorRing(cp, r, t)
		})
		if p.noteScrollbar != nil {
			p.noteScrollbar(scrollbar.Hot(table))
		}
	})
}

// drawCursorRing 给**游标那一行**画一圈焦点环。
//
// # 为什么自己画
//
// 框架给表格的游标那一行也画圈环（`mygo/ui/list.go`：多选时「键挪到的那一行」），
// 但它认的是框架那个 `focusVisible`——**指针按一下会把它清掉，方向键不会设回来**
// （理由见 `widgets/list/selection.go` 的 KeyboardMode）。于是「点一行 → 按 Down」
// 那一路上，框架那一圈不画，而表格的游标在画面上就一个标记都没有了。
//
// # 那圈环画在哪
//
// **游标那一行的整行**：行元素是框架的（画不到），但**序号那一格的盒子是我们的**，
// 而它撑满那一格（`viewRowNumber`）——所以按它算出那一行的位置，再往外扩到整行
// 的宽（表格的宽减掉两边的内边距）。
//
// # 什么时候画
//
// **键盘在管的时候**：指针点过之后那一行什么标记都没有（上游点一行不改变它的样子），
// 但**再按方向键走位时环要回来**。
func (p *localPage) drawCursorRing(cp *ui.Painter, r ui.Rect, t *ui.Theme) {
	if !p.showCursorRing || !p.cursorCell.Valid() || !p.tableEl.Valid() {
		return
	}
	cell := p.cursorCell.Bounds()
	tb := p.tableEl.Bounds()
	// 框架给每一格左右留的内边距（`mygo/ui/table.go` 的 `Padding(Space(1.5),
	// Space(2.5))`）：行是那些格子的地盘，环要贴着行的边。
	padX := t.Space(2.5)
	// 表格的盒子在它**父元素**的空间里（`DrawOver` 给的 `r`），而盒子的
	// `Bounds()` 是**窗口**坐标——两者的差就是「相对表格左上角的位置」。
	rect := ui.Rect{
		X: r.X + cell.X - tb.X - padX,
		Y: r.Y + cell.Y - tb.Y - localTableRowPad,
		W: tb.W - 2*padX,
		H: localTableRowPx,
	}
	// 往里收 3 像素，和框架画行焦点环时同一个数（`mygo/ui/list.go`）。
	cp.FocusRing(ui.Rect{X: rect.X + 3, Y: rect.Y + 3, W: rect.W - 6, H: rect.H - 6},
		[4]float32{0, 0, 0, 0})
}

// viewCell 画表格的一个格子。
func (p *localPage) viewCell(c *ui.Context, t *ui.Theme, row, col int) {
	tr := p.rows[row]
	switch col {
	case 0:
		p.viewRowNumber(c, t, row)
	case 1:
		ui.Text(c, tr.Title).SingleLine()
	case 2:
		ui.Text(c, fileSize(tr.Size)).FontSize(scale.Font(c, 12)).TextColor(t.TextMuted)
	case 3:
		// 放不了的格式用警示色，而不是省略号或者别的字样：Biu 那一格画的是
		// `data.format?.toUpperCase() || "-"`（`item.tsx:71`），我们多给一层
		// 颜色，说的是同一件事（「列出来了，但这一格有毛病」）。
		e := ui.Text(c, strings.ToUpper(strings.TrimPrefix(tr.Format, "."))).FontSize(scale.Font(c, 12))
		if tr.Playable() {
			e.TextColor(t.TextMuted)
		} else {
			e.TextColor(t.Warning)
		}
	case 4:
		ui.Text(c, durText(tr.Duration)).FontSize(scale.Font(c, 12)).TextColor(t.TextMuted)
	case 5:
		ui.Text(c, createdText(tr.Created)).FontSize(scale.Font(c, 12)).TextColor(t.TextMuted)
	case 6:
		p.viewRowOps(c, tr)
	}
}

// localPlayIconLabel 是行 hover 时那一枚播放图标的无障碍名字。
//
// 它只在 hover 的那一行画出来，所以它同时是测试认「这一下 hover 生效了没有」
// 的凭据：画面上一共有几个这个名字，就是有几行正被指着。
const localPlayIconLabel = "播放这首"

// viewRowNumber 是表格序号那一格：平时是序号，指针压在这一行上时换成播放图标。
//
// 出处是上游 `local-music/item.tsx:57-64`：`group-hover:hidden` 藏序号、
// `hidden group-hover:inline` 亮 `RiPlayFill`（16）。那一行是双击播放的，所以
// 这个图标是「这里点得动」的提示，不是另一颗按钮——**不给它点击处理**：
// 单击选行、双击播放这件事由表格自己做（`OnSubmit`）。
//
// ⚠️ **`ui.Table` 没有「这一行 hover 了」的回调**（`cell` 只给行列号），所以
// 这里拿**上一帧的布局**自己判：表格在不在指针下面（`tableBox`），以及指针在
// 序号这一格的纵向范围里（`PointerPosition` 给的 y 不受横向位置影响——它在
// 指针压着别的列时照样算得出相对坐标）。
//
// 那个盒子是**固定尺寸**的：换图标不改变行高与列宽，否则指针一上去整行都跳。
func (p *localPage) viewRowNumber(c *ui.Context, t *ui.Theme, row int) ui.Element {
	h := float32(localTableRowPx - 2*localTableRowPad)
	cell := ui.Row(c).FillWidth().Height(h).Center().Justify(ui.Center)
	onRow := p.tableRowHovered(cell)
	// ⚠️ **游标那一行的盒子要留着**：焦点环按它算出那一行的位置（理由见
	// `drawCursorRing`）。它是**撑满那一格**的（`FillWidth` + `Height`），所以它的
	// 盒子就是那一行的位置。
	if row == p.sel {
		p.cursorCell = cell
	}
	cell.Children(func() {
		if onRow {
			ui.Icon(c, icon(iconPlayFill)).FontSize(16).TextColor(t.Text).Label(localPlayIconLabel)
			return
		}
		ui.Text(c, strconv.Itoa(row+1)).FontSize(scale.Font(c, 12)).TextColor(t.TextMuted)
	})
	return cell
}

// tableRowHovered 报这一格的纵向位置是不是压在指针下面。
//
// 判据分两半：**指针在表格里**（否则页面左边、侧栏里同一条横线上的那一格也会
// 亮）和**指针在这一格的高度之内**。两样读的都是上一帧的盒子——即时模式里
// 布局是这一帧开头量完的，`Bounds` / `PointerPosition` 给的正是它。
func (p *localPage) tableRowHovered(cell ui.Element) bool {
	if !p.tableBox.Valid() {
		return false
	}
	if _, _, over := p.tableBox.PointerPosition(); !over {
		return false
	}
	_, y, _ := cell.PointerPosition()
	// ⚠️ **判据要算上那一行的上下内边距**：格子（内容区）只有 32 高，而那一行是
	// 44 高（上下各 6 是 `ui.Table` 给每一行的内边距）——指针压在那 6 像素上也算
	// 「压在这一行」。只按 32 算的话，行的上/下各 6 像素是死区，图标会在指针
	// 上下移动时闪一下（上游的 `group-hover` 是**整行**都亮的）。
	return y >= -localTableRowPad && y < cell.Bounds().H+localTableRowPad
}

// viewRowOps 是表格每一行最后那一格的两个按钮。
//
// ⚠️ **正在放的那一首不给「删除」**：照 Biu 上游
// （`local-music.ts:57` 的 `items = isPlaying ? menus.filter(m => m.key !== "delete")`）。
// 删掉正在放着的那个文件，播放器这一路会一直播到它自己结束（文件句柄还在），
// 用户看到的是「歌还在放，但列表里没了，再按下一首就报错」——一个说不清的状态。
// 不给这个按钮，好过给一个按下去会变成那个状态的按钮。
func (p *localPage) viewRowOps(c *ui.Context, tr localmusic.Track) {
	// 两个图标键。上游那一格是一枚「⋯」菜单（`operation.tsx:38`）；我们这里是
	// 两个直接点得到的动作——一行的操作一共就两件，藏进菜单多一次点击。
	//
	// ⚠️ 删除那一颗的红色靠 `TextColor`：图标继承祖先的文字色，而 `flat` 那个
	// 配方不给颜色——不给的话它和打开文件夹那枚一样是灰的，「这一下是不可撤销的」
	// 那层意思就没了。
	if p.tipWith(c, flatIconButton(c, iconFolder, "打开文件夹", iconSm).
		Size(iconHit, iconHit).Padding(space.None, space.Px9), "在访达里显示").Clicked() {
		mygo.Shell.ShowItemInFolder(tr.Path)
	}
	if !p.isPlaying(tr) {
		if p.tipWith(c, flatIconButton(c, iconDeleteBin5, deleteRowLabel, iconSm).
			Size(iconHit, iconHit).Padding(space.None, space.Px9).
			TextColor(c.Theme().Danger), deleteRowLabel).Clicked() {
			p.askTrack, p.askOpen = tr, true
		}
	}
}

// cardsOf 把 rows 转成卡片控件要的形状。
//
// ⚠️ `Owner` 那一格放的是「这个格式我们放不了」——卡片控件把它画在标题下面
// 当副标题，而本地文件的封面永远是空的（见 `app.State.CoverURL`），所以那一
// 格本来是空的，正好用来放一句用户需要知道的话。
func (p *localPage) cardsOf() []list.Music {
	p.songs = p.songs[:0]
	for _, tr := range p.rows {
		m := list.Music{
			ID:       tr.ID,
			Title:    tr.Title,
			Duration: tr.Duration,
			Item:     app.LocalItem(tr.Path),
			// ⚠️ **没有 View，而且永远不会有**：文件没有播放量。B 站那份数说的是
			// 「多少人在 B 站听过」，给本地文件挂上等于编一个数。所以网格卡片封面
			// 底部那一条渐变上**左半边不画**（理由见 `list.Music.View`）。
		}
		if !tr.Playable() {
			m.Owner = "这个格式我们放不了"
		}
		p.songs = append(p.songs, m)
	}
	return p.songs
}
