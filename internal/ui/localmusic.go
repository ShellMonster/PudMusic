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
	"github.com/ShellMonster/Biu_Go/internal/service/localmusic"
	"github.com/ShellMonster/Biu_Go/internal/ui/guard"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scrollbar"
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
	// 它排在最后一个：`ui.AlertDialog` 把最后一个按钮当主按钮并自动聚焦
	// （`mygo/ui/feedback.go:198`）。
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

// 确认框的尺寸。420 是按 Biu 那一行弹窗的观感定的：路径那一行在 420 宽下
// 大概能放 45 个字符，再窄就开始一行一截地断。
//
// ⚠️ 窗口缩到 700 以下时那个 420 就占掉大半屏了，所以另有一个 70% 的上限
// （`MaxWidthPercent`）——`ui.Context` ��有宽度可问，只能给比例。
const (
	dialogWidth       = 420
	dialogMaxWidthPct = 0.7
	dialogPad         = 16
	dialogGap         = 12
)

// dialogOnDanger 是红色确认按钮上的字。
//
// 主题给的是 `Danger` 那一个颜色，没有配套的「危险底上的前景色」，而这里
// 需要的正是后者。白色在两个主题的 Danger 上都读得清（见 confirmDialog 里
// 那句关于 #dc2626 / #ef4444 的注释），所以就用白。
// dialogOnDanger 是红色确认按钮上的字。
var dialogOnDanger = ui.RGB(255, 255, 255)

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
	notify func(string)
	// pickDir 弹系统框选一个文件夹。它必须从**后台 goroutine** 调，所以
	// 调用方（`addDirectory`）自己起 goroutine，这里只管调。
	pickDir func() (string, error)

	// ── 下面这一摊只由界面线程读写（铁律一）──────────────────────────────

	// table 是表格那一档的 `ui.ListState`。**滚动位置和选中行就跟着它走**，
	// 所以它建一次、存在这里，不每一帧新建。
	table ui.ListState
	// sel 是表格里选中的那一行，-1 是没选。框架把点中的下标写进来。
	sel int

	// cards 是「歌曲」那一档的控件（`widgets/list` 的 `SwitchView`：列表 / 紧凑 /
	// 网格三个摆法在它里面分发，见文件头那一段）。它自己记着滚动位置，所以建一次
	// 存着。
	cards *list.SwitchView

	// displayView 报「这一帧歌曲该摆成什么样」。**由 Shell 传进来**而不是这一页
	// 自己去问设置：这一页只有 `*app.App`（它看得到设置），而 `displayView` 是
	// 界面层那个「唯一一处把设置翻译成画法」的地方（`displaymode.go`）——从这一页
	// 再写一份就是第二处。
	displayView func() list.View

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
	// search 是搜索框里正在敲的字。
	search string

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
	notify func(string), pickDir func() (string, error), displayView func() list.View) *localPage {
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
	p.begin(c)

	st := p.app.LocalMusicState()
	p.rows = p.filter(st.Tracks, st.Roots)

	// ⚠️ **横向那一格用 `pagePadX`**（七页共用一个数，见 pagerhythm.go）。
	ui.Column(c).Key("page:"+localMusicPath).Grow(1).Padding(16, pagePadX).Gap(10).
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

// localTableRowPx 是本地音乐那一档表格里一行的高：操作那一列那枚键（28）+ `ui.Table`
// 给每一行的上下内边距（6+6）。
//
// ⚠️ **它只用来把「顶上那一行是第几行」换算成像素**（回顶按钮的阈值是 400
// 像素）。这一页的表格自己滚，而它内部那个滚动容器的像素偏移拿不到。
//
// ⚠️ **它是量出来的那一个数**（`TestTheTableRowHeightsAreWhatTheOffsetsAssume`
// 钉着）：那一行的高由最高的格子决定（操作那一列的键），改了那一列的字号就得
// 跟着改它。
const localTableRowPx = 40

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
	ui.Row(c).AlignSelf(ui.Stretch).Center().Gap(8).Children(func() {
		ui.Text(c, "本地音乐").FontSize(scale.Font(c, pageTitleSize)).Bold()

		// 展示样式切换。⚠️ 用 `ui.Segmented` 而不是三个按钮：三档互斥、
		// 当前位置一眼可见，三个按钮的话用户得靠猜哪个是按下去的。
		ui.Segmented(c, &p.style, localStyles[:]...)

		ui.Spacer(c)
		// 上游是 `variant="flat"` 的一个文字键（`local-music/index.tsx:189`）。
		// 它旁边那颗文件夹加的图标没画：`flat.Button` 只接管文字，图标 + 文字
		// 要再开一个配方，而这一票要的是「把描边和常驻底去掉」，不是重画按钮。
		flatButton(c, "添加目录").OnClick(p.addDirectory)
	})
}

// viewToolbar 是操作那一行。
func (p *localPage) viewToolbar(c *ui.Context, st app.LocalMusicState) {
	ui.Row(c).AlignSelf(ui.Stretch).Center().Gap(8).Children(func() {
		if len(p.rows) > 0 {
			// 「播放全部」用主按钮：Biu 那一页上它就是 primary
			//（`local-music.ts:212` 的 `color="primary"`）。
			ui.PrimaryButton(c, "播放全部").OnClick(p.playAll)
			// 照上游那一枚 `IconButton`（`local-music/index.tsx:208`，tooltip
			// 「添加到播放列表」）：图标键 + 悬停说出它做什么。
			if p.tipWith(c, flatIconButton(c, iconEnqueue, "全部加入队列", iconMd).
				Size(iconHitMd, iconHitMd).Padding(0, 5), "全部加入队列").Clicked() {
				p.queueAll()
			}
		}

		ui.Spacer(c)

		// ⚠️ `Label` 和 `Placeholder` 都得给：`Label` 是这个控件的**名字**
		//（无障碍念它、测试的 `Click`/`Find` 按它找），`Placeholder` 只是
		// 空着时画的那几个字。少了 Label 这个框就没有可点的名字。
		ui.TextInput(c, &p.search).Label(searchLocalLabel).Placeholder(searchLocalLabel).Width(180)
		ui.Select(c, &p.dirLabel, dirLabels(st.Roots)).Width(180)
		// 移除此目录 / 重新扫描：上游是两枚 `IconButton`（`local-music/index.tsx:238,244`，
		// tooltip「刷新」「移除当前目录」）。悬停提示说的就是名字——图标键没有
		// 可见的字，提示是它唯一说得出自己做什么的地方。
		if p.currentRoot(st.Roots) != "" {
			if p.tipWith(c, flatIconButton(c, iconDeleteBin5, "移除此目录", iconMd).
				Size(iconHitMd, iconHitMd).Padding(0, 5), "移除此目录").Clicked() {
				p.askRemoveDir()
			}
		}
		if p.tipWith(c, flatIconButton(c, iconRefresh, "重新扫描", iconMd).
			Size(iconHitMd, iconHitMd).Padding(0, 5), "重新扫描").Clicked() {
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
			Hint:  "点右上角「添加目录」选一个文件夹，它的第一层里的歌会出现在这里。",
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

// viewTable 是表格那一档。
func (p *localPage) viewTable(c *ui.Context, t *ui.Theme) {
	// 🔴 铁律四：Key 和 Label 每一帧都重装，装的是 ID（不是序号）。
	//
	// 为什么这一页特别需要：往目录里加一个文件夹，列表前面就多出一整段，
	// 后面所有行的下标全部错位。不按 ID 装的话，选中项会跳到旁边那一首。
	p.table.Key = func(i int) any { return p.rows[i].ID }
	p.table.Label = func(i int) string { return p.rows[i].Title }
	p.table.Selected = &p.sel

	table := ui.Table(c, &p.table, localColumns, len(p.rows), func(row, col int) {
		p.viewCell(c, t, row, col)
	}).
		Grow(1).FillWidth().
		OnSubmit(func() { p.playRow(p.sel) })
	if p.noteScrollbar != nil {
		p.noteScrollbar(scrollbar.Hot(table))
	}
}

// viewCell 画表格的一个格子。
func (p *localPage) viewCell(c *ui.Context, t *ui.Theme, row, col int) {
	tr := p.rows[row]
	switch col {
	case 0:
		ui.Text(c, strconv.Itoa(row+1)).FontSize(scale.Font(c, 12)).TextColor(t.TextMuted)
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
		Size(iconHit, iconHit).Padding(0, 7), "在访达里显示").Clicked() {
		mygo.Shell.ShowItemInFolder(tr.Path)
	}
	if !p.isPlaying(tr) {
		if p.tipWith(c, flatIconButton(c, iconDeleteBin5, deleteRowLabel, iconSm).
			Size(iconHit, iconHit).Padding(0, 7).
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
