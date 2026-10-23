// 分页行列表：「稍后再看」和「历史记录」两页共用的那一份。
//
// # 为什么不用 `widgets/list` 里的 `list.PagedList`
//
// 这两页的行比 `card.go` 那一行**多两列**：稍后再看有播放量，历史记录有播放进度 +
// 观看时间。`list.Music` 上没有这些字段，而这一票明令 `widgets/list/` **只读不改**
// ——所以画法只能自己带一份，行也就只能自己建。这一份是照着 `list/paged.go` 的三重
// 保障写的，等那个包重新可写时要么把 `render` 做成它的一个参数、要么把这一份整个挪
// 进去，**别让它长期和那一份并存**：两套翻页逻辑并排，过两个月就有一半是错的。
//
// 复用的部分一个没少：行模型嵌入 `list.Music`、操作走 `list.AppActions`、
// 「在 B 站打开」走 `list.BiliLink`、封面走 `imgcache`、每行的兜底走 `guard.Wrap`。
// 跨 goroutine 的那两小摊（信箱 + 逐窗口唤醒）单独在 `library_wake.go`。
//
// # 三重保障（抄 `list/paged.go` 的那三条，理由在那边逐条写着）
//
//	① 首帧就查容器填没填满　② 末尾哨兵进了画面就拉下一页　③ 锁一放再查一遍
//
// 最容易漏的是①③合起来那条：**筛选后只剩两页**。用户没滚过，容器没填满，底下留
// 一大片空白还一直转圈。
//
// 🔴 铁律一（状态只在界面线程写）、三（投递完逐窗口唤醒）、四（行身份是 `Music.ID`）
// 在这一层都成立。
package ui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/guard"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scrollbar"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// libraryRow 是这两页的一行 = `list.Music` 加上那几列它画不出来的。
//
// ⚠️ 嵌入而不是另立一个结构体：共用着那几组（ID / 标题 / 封面 / UP 名 / 时长 /
// 队列项）就一份定义，页面画行时拿到的是同一个 `list.Music`。
type libraryRow struct {
	list.Music

	// Progress 看了多少。历史记录那一列就是它（见 history.go）。
	Progress time.Duration
	// ViewedAt 什么时候加进稍后再看 / 什么时候看的。
	ViewedAt time.Time
	// View 是播放量。**只有稍后再看有**，历史记录的接口不给。
	View int64
	// CanPlay 报这一条能不能放。放不了的行界面上不给「播放」。
	CanPlay bool
	// CannotPlay 是放不了的理由（空串 = 放得了）。
	//
	// 界面上菜单项按不动的时候把它摆在旁边：**按不动而不说为什么**，
	// 用户只会以为这一行坏了。理由由应用层给（`app.LibraryItem.CannotPlay`），
	// 界面不自己编——那一句要能回答「为什么」和「还能怎么办」。
	CannotPlay string
}

// libraryQuery 是拉一页要用的那一份筛选条件。
//
// ⚠️ **它必须是界面线程上取好的一份快照**，不是让 fetcher 回去读界面状态（关键词和
// 日期范围都在界面上，用户随时改着）。后台 goroutine 去读那些字段就是数据竞争，
// 而且这种错在界面上几乎看不出来：多半是**某一次筛选的结果配上了另一次的词**。
type libraryQuery struct {
	keyword  string
	from, to time.Time
	// on 报现在有没有在按日期筛。零值的两端就是「不筛」，服务层那一句 addRange
	// 会把它们一个都不发。
	on bool
}

// libraryFetcher 拉一页。page 从 1 起。
//
// ⚠️ 它在**后台 goroutine 里**调：除了 ctx / page / loadedBefore / q 这四个参数
// （全是界面线程上取的），它**不许再碰任何界面状态**——要通知界面就往返回值里放。
// 它要是 panic 了，这一页就当失败处理（见 fetchOne）。
//
// loadedBefore 是**拉这一页之前已经加载了几行**，用来算「还有没有」。
type libraryFetcher func(ctx context.Context, page, loadedBefore int, q libraryQuery) (rows []libraryRow, hasMore bool, err error)

// maxWakes 是记着的唤醒句柄上限：一个窗口一条，多出来的挤掉最久没画的那个。
const maxWakes = 4

// gridGap 是网格那一档格子之间的间距。
//
// ⚠️ **取 `list.GridGap` 而不是另写一个数**：Biu 那个网格组件在同一个窗口宽度下
// 摆出来的格子宽度是唯一的，而这一份和 `widgets/list` 的 `Grid`（收藏夹 / 搜索
// 那一堆页面的网格那一档）在同一个应用里摆的是同一批歌——间距差几个像素，用户
// 看到的就是「这两个页面对不上」。
const gridGap = list.GridGap

// pagedRows 是一个会自己往下翻页的行列表。
//
// 建一次，存进页面自己的状态里，每一帧调 View。
type pagedRows struct {
	// draw 是包了 recover 的画法，见 View。建的时候包一次就够。
	draw func(*ui.Context)

	// state 是这一份列表的滚动位置和选中项（铁律四的关键，见 view）。
	state ui.ListState
	sel   int

	// hot 是上一帧画完后量到的「指针压在滚动条那一带」（见 `Hot`）。
	hot bool

	// render 画这一行并**把它交回来**（菜单要挂在它身上，见 viewRow）。收整个
	// ListRow 而不是光一个下标，是为了让页面能看见 `r.Selected()`——选中的那一行要
	// 盖掉框架那块实心主色底，键盘走到它上面时还要有一圈焦点环
	//（`widgets/list/selection.go`）。menu 是这一行的右键菜单。两者都由页面给。
	render func(c *ui.Context, r ui.ListRow) ui.Element
	menu   func(m *ui.Menu, i int)

	// renderCell 画网格那一档的**一格**（不是一行），给页面用。
	//
	// ⚠️ **为 nil 时那一档退回列表**（见 SetView）：这一份翻页列表是照着
	// `list/paged.go` 另写的，而网格那一档要页面自己画一格——没给就不画，
	// 画一个空框等于告诉用户「这里什么都没有」。
	renderCell func(c *ui.Context, i int, cell float32, selected bool) ui.Element

	// layout 是设置里的「数据显示样式」那一档，见 SetView。零值是列表。
	//
	// 名字用 layout 而不是 view：这一份已经有一个 `view` 方法（真的画法）和一个
	// `View` 方法（外面那道 recover），字段再叫 view 就三个同名的东西了。
	layout list.View

	// width / cols 是网格那一档算列数用的两个数：width 是**上一帧**量出来的容器
	// 宽（`Bounds` 量的是上一帧的盒子，见 `widgets/list/grid.go` 的同一句），
	// cols 是上一帧的列数。行数要靠它算（`rowCount`），而行数在画之前就得给
	// `ui.List`，所以晚一帧只能晚一帧。
	width float32
	cols  int

	// empty 是一行都没有时说的那两句话（见 SetEmpty）。
	empty emptyState

	fetch libraryFetcher
	acts  list.Actions

	// query 取「这一页要用什么条件去拉」。**由 load 在界面线程上调**，返回值是
	// 一份快照（见 libraryQuery）。做成一个回调而不是两个字段，是因为它读的那几样
	// 东西分散在 `libraryPage` 上（词在那儿、日期在 dateFilter 上）。
	query func() libraryQuery

	// ── 下面这一摊只由界面线程读写（铁律一）────────────────────────────────

	// rows 是已经拿到的全部行，**追加在尾部**。
	rows []libraryRow
	// seen 是 rows 里已有的 ID。翻页重叠是常事（B 站翻页本来可能重复），不去重
	// 就是两个一样的身份共用一份状态——选中一个另一个也亮。
	seen map[string]bool

	page    int
	hasMore bool
	loading bool
	err     error
	// blocked 记着「上一页拉失败了，自动补屏先停在这儿」。没有它就是自己打自己
	// 的循环：失败 → 解锁 → 下一帧看见容器还是没填满 → 又拉。
	blocked bool
	// gen 是代次。换查询时 +1：在途请求的结果回来时代次对不上就扔掉。
	gen    uint64
	cancel context.CancelFunc
	// why 是这一页是被哪一条保障叫起来的，只是为了出错时说得清是哪一条。
	why trigger

	scroll   ui.ScrollState
	rev      int
	shownRev int
	// failed 是画崩了的行（按 ID 记），下一帧不重试：同一行每帧崩一次会刷爆日志、
	// 拖垮帧率，比「这一行画不出来」糟得多。
	failed map[string]bool

	// keys 记「上一次输入是键盘还是指针」（理由见 `widgets/list/selection.go` 的
	// KeyboardMode）。
	//
	// ⚠️ **焦点环靠它**：框架那个 `focusVisible` 只被 Tab 设回来，方向键不会——
	// 于是「鼠标点过一行之后再按方向键」那一路上，键盘走到哪一行都没有指示。
	keys list.KeyboardMode

	// ── 跨 goroutine 的只有下面这两小摊 ──────────────────────────────────

	// wakes 是**每个画过这个列表的窗口**一条，deliver 全部叫醒。
	wakeMu sync.Mutex
	wakes  []wakeEntry
	frame  uint64

	// mu 只护着信箱：后台往里投，界面线程在 view 开头取。
	mu  sync.Mutex
	box []mailboxItem
}

// newPagedRows 建一个会自己翻页的行列表。
//
// ⚠️ **不在这里拉第一页**：它可能在界面线程之外被调，那时候还没有唤醒句柄也没有
// 画面。第一页由第一帧的 view 拉。
func newPagedRows(render func(*ui.Context, ui.ListRow) ui.Element, menu func(*ui.Menu, int),
	fetch libraryFetcher, acts list.Actions, query func() libraryQuery) *pagedRows {
	p := &pagedRows{
		render: render, menu: menu,
		fetch: fetch, acts: acts, query: query,
		sel: -1,
		// 「还有没有」要等有人问过才知道，在那之前一律当作还有——不然第一帧会以为
		// 到底了，连页脚那一行都不画。
		hasMore: true,
		seen:    map[string]bool{},
	}
	// 「↑ 到顶时把焦点交回侧栏」那一档（票 #245 的 E4）。理由同
	// `widgets/list` 的 `New`。
	p.keys.SetUpAtTop(acts.FocusOut)
	p.draw = guard.Wrap(p.view, p.onPanic)
	return p
}

// Rows 是已经拿到的行。页面拿它画表头下面的东西、拿选中项做别的操作。
func (p *pagedRows) Rows() []libraryRow { return p.rows }

// Selected 返回选中的那行，没有选中是 ok=false。返回的是**行**不是序号。
func (p *pagedRows) Selected() (libraryRow, bool) {
	if p.sel < 0 || p.sel >= len(p.rows) {
		return libraryRow{}, false
	}
	return p.rows[p.sel], true
}

// PagedStatus 是这一页列表此刻的状态。测试照它断言，页面上照它说话。
type PagedStatus struct {
	Page    int
	Loaded  int
	HasMore bool
	Loading bool
	Err     error
}

// Status 返回此刻的状态。
func (p *pagedRows) Status() PagedStatus {
	return PagedStatus{
		Page: p.page, Loaded: len(p.rows), HasMore: p.hasMore,
		Loading: p.loading, Err: p.err,
	}
}

// Reload 换查询：关键词变了、日期范围变了。
//
// 停掉在途请求、清空已加载的行、回顶，然后从第 1 页重来。**旧结果作废**靠这里
// bump 的代次——光 cancel 不够：请求已经回来了、正在往信箱里投的时候 cancel 拦不
// 住它。
//
// ⚠️ 只能由界面线程调。
func (p *pagedRows) Reload() {
	p.stop() // 含 p.loading = false
	p.gen++
	p.rows = nil
	p.seen = map[string]bool{}
	p.page = 0
	p.hasMore = true
	p.err = nil
	p.blocked = false
	p.rev++
	p.ScrollTop()
	// 当场拉第一页，不等下一帧：Reload 有可能是在 view 之外调的，那时候不一定还
	// 有下一帧。
	p.load(trigFirstRender)
}

// ScrollTop 回到顶上。切路由和点回顶按钮都走它。
//
// 两条都写是有原因的：`scroll` 是这一帧记下的偏移，列表自己按行的身份算锚点
// （mygo 的 ListState：「A List keeps its place by its rows instead」），两条都给
// 才不管列表是刚建的还是一直在画的都是顶上。
//
// ⚠️ 只能由界面线程调。
func (p *pagedRows) ScrollTop() {
	p.scroll.Y = 0
	p.state.ScrollTo(0, ui.Start)
}

// SetView 换摆法（设置里的「数据显示样式」）。**每一帧都要调**。
//
// 三档：列表 / 紧凑 / 网格。前两档走 `render`（紧凑只是行内尺寸变矮，见
// `metricsOf`），网格那一档走 `renderCell`。**没给 `renderCell` 就退回列表**——
// 见那个字段的注释。
func (p *pagedRows) SetView(v list.View) {
	if v == list.ViewCard && p.renderCell == nil {
		v = list.ViewList
	}
	p.layout = v
}

// Layout 返回此刻的摆法。页面画表头时照它决定要不要摆（网格里没有表头，理由见
// `rowheader.go` 文件头）。
func (p *pagedRows) Layout() list.View { return p.layout }

// SetCell 装网格那一档的画法（页面在 init 里给）。
func (p *pagedRows) SetCell(fn func(c *ui.Context, i int, cell float32, selected bool) ui.Element) {
	p.renderCell = fn
}

// isGrid 报此刻是不是网格那一档。
func (p *pagedRows) isGrid() bool { return p.layout == list.ViewCard }

// SetEmpty 换掉「一行都没有时说的那两句话」。
//
// **由页面把 `routes` 表里那一条的 `Empty` 原样递进来**，不在这儿另抄一份：出处
// （上游哪个文件）写在路由表那一条的注释上，抄一份就多一个会对不上的来源。
//
// ⚠️ 只能由界面线程调。
func (p *pagedRows) SetEmpty(e emptyState) { p.empty = e }

// Drop 丢掉某一行的本地副本（用户在菜单里删掉了一条之后调它）。
//
// 只动内存里已经拿到的那些，**不重新拉**——重新拉会把滚动位置、用户刚选的那一行
// 全冲掉，而删掉的那一条本来就要从屏幕上消失。
//
// ⚠️ 只能由界面线程调。找不到就静默忽略：那是调用方传了一个屏幕上没有的 ID。
func (p *pagedRows) Drop(id string) {
	i := slices.IndexFunc(p.rows, func(r libraryRow) bool { return r.ID == id })
	if i < 0 {
		return
	}
	p.rows = slices.Delete(p.rows, i, i+1)
	delete(p.seen, id)
	if p.sel > i {
		p.sel--
	} else if p.sel == i {
		p.sel = -1
	}
	p.rev++
}

// View 画这一页。页面在每一帧开头调一次，给它一块 Grow(1) 的地方。
//
// 🔴 外面包着 recover（`guard.Wrap`，建的时候包一次）：view 里 panic 会一路穿到
// Cocoa 事件循环上去把整个进程带走。
func (p *pagedRows) View(c *ui.Context) {
	// 登记「内容区里当前那一个能被方向键走位的」（票 #245 的 E4）。
	//
	// ⚠️ **这一份是界面层自己画的**（不走 `widgets/list` 的 `List`，理由见
	// 这个类型的文件头），所以登记得自己写一遍——焦点句柄是同一个框架机制
	// （`ui.ListState.Handle`）。
	if p.acts.Focus != nil {
		p.acts.Focus(p)
	}
	p.draw(c)
}

// TakeFocus 把键盘焦点接到这份行列表上，并把光标放到第一行。
//
// ⚠️ **和 `widgets/list` 的 `List.TakeFocus` 是同一件事**（票 #245 的 E4）：
// 用户是「从侧栏走进内容区」的，进来就停在第一首上，才对得起「走进来就在
// 第一首歌上」。
func (p *pagedRows) TakeFocus() {
	if len(p.rows) > 0 {
		p.sel = 0
	}
	p.state.Handle.Focus()
}

// onPanic 记一笔并要一帧。
//
// 要下一帧是必须的：recover 接住的是 build pass 中间，那一帧剩下的部分没建，画出
// 来是半张。**这里不换错误页**：坏的是这一个控件，页面上别的东西照常用。
func (p *pagedRows) onPanic(rec any) {
	log.Printf("biu: 分页行列表画崩了：%v\n%s", rec, debug.Stack())
	p.invalidate()
}

// Sync 是这一份翻页列表每帧的**副作用**：登记唤醒句柄、收后台投回来的结果、
// 起该起的那一拉。
//
// ⚠️ **它和 `view` 的分工**：收结果和起第一拉不能跟着画一起停——满屏播放器
// 开着的时候底层那一页不画（票 #161），而这一份列表的结果一直落在信箱里没人
// 取的话，用户关掉满屏看到的是「打开那一刻的样子」。
//
// ⚠️ **二重保障（②滚到底、③容器没填满）不在这里**：那两条都要用户先滚过去，
// 而满屏开着的时候用户滚不动底下那一页。它们留在 `view` 里，跟着画一起停。
//
// ⚠️ **副作用**：它们要读 `ListState.Visible()`，而那一句会置框架的 `read` 标志
// （`mygo/ui/list.go:225`）——不读它，框架就不会因为「可见区变了」自动补那一帧
// （`mygo/ui/list.go:1394-1400`）。满屏期间那一帧本来就是白补（画的是满屏），
// 而恢复之后的第一帧 `Visible()` 报的还是满屏之前的旧行号，「末尾还在画面外」
// 那个判断会晚一帧才成立。
func (p *pagedRows) Sync(c *ui.Context) {
	// 🔴 铁律三：把这一帧的窗口登记进去。后台投递完结果要靠它叫醒界面，不唤醒就是
	// 「结果到了、画面停在旧帧」（实测 0 帧刷新）。
	p.setWake(c.Services())

	// 先收后台投回来的页再画：这样这一帧画的就是**已经加上**的那几行。
	p.adopt()

	// ① 首帧。**必须在画之前发出去**：画完再发的话这一帧画出来的是「这里还没有
	// 内容」，要等这一页回来才换掉，用户第一眼看到的是空的。
	if p.page == 0 {
		p.load(trigFirstRender)
	}
}

// view 是真的画法，View 是它外面那道 recover。
func (p *pagedRows) view(c *ui.Context) {
	p.Sync(c)

	if p.isGrid() {
		p.drawGrid(c)
	} else {
		p.drawList(c)
	}

	// ②③ 在这里跑（必须排在画完之后：`Visible` 报的是**上一帧**的行，
	// mygo 的 ListState.Visible 注释：「the row the list showed in the last frame」）。
	p.afterFrame()
	p.viewBackToTop(c)
}

// syncState 把这一帧的行身份、名字和选中项装进 ListState。
//
// 🔴 铁律四：**Key 每一帧都重装，装的是 Music.ID**。不装的话 MyGo 拿「父 ID + 兄弟
// 序号」当身份，前面插一行会让后面所有行的状态整体错配。⚠️ 别改成序号。
func (p *pagedRows) syncState(cols int) {
	// ⚠️ 网格那一档 `i` 是**行号**不是行的下标，所以行的身份取「这一行第一首的
	// ID」而不是 `p.rows[i].ID`（理由同 `widgets/list` 的 Grid.State.Key）。
	first := func(i int) int { return i }
	if p.isGrid() {
		first = func(i int) int { return i * cols }
	}
	p.state.Key = func(i int) any {
		j := first(i)
		if j >= len(p.rows) {
			return footerIdentity{}
		}
		return p.rows[j].ID
	}
	p.state.Label = func(i int) string {
		j := first(i)
		if j >= len(p.rows) {
			return "加载更多"
		}
		return p.rows[j].Title
	}

	// 🔴 **网格那一档不装行选中**。`state.Selected` 收的是**行下标**，而这一份的
	// `sel` 记的是**第几首歌**（格子才是用户点的东西，`widgets/list` 的 Grid 也是
	// 自己管选中的）。两套下标塞进同一个 int 的后果有一个，而且很难查：
	//
	//   - 点在格子之间的空地上时框架写进来一个**行号**，于是「选中的那一首」变成
	//     另一首。
	//
	// 选中由每一格自己画（`viewCellGuarded` 的 OnClick），和 `widgets/list` 的 Grid
	// 同一个做法。
	//
	// ⚠️ **框架那一层实心主色底本来还会让网格看起来像「选中了一行」**——那一条
	// 在票 #197 之后不存在了：网格那一档不装行选中，框架不画；而列表那一档的
	// 那一块由选中的行自己刷底色盖掉（`widgets/list/selection.go`）。
	p.state.Selected = nil
	if !p.isGrid() {
		p.state.Selected = &p.sel
	}
}

// footerIdentity 是末尾那一行的身份。它**不是字符串**：行的 ID 是 B 站给的
// 用户数据，理论上能撞上一个同名的；类型撞不上。
type footerIdentity struct{}

// showFooter 报这一帧末尾那个哨兵行在不在。
//
// ⚠️ **一行都还没有、第一页还在路上的时候不摆它**——这不是省事，是必需：MyGo 的列表
// 是**按行记位置**的（`ListState`：「A List keeps its place by its rows instead」）。
// 那一帧画面上只有哨兵这一行，它就成了锚点；第一页回来之后锚点仍然是它，框架把视口
// 挪到它身上——**列表一打开就停在最底下**，用户看见的是第 30 首而不是第 1 首（实测
// 40 行时首帧就落在 31..40）。那一帧不摆哨兵就没有锚点要记，列表自然从顶上开始。
func (p *pagedRows) showFooter() bool {
	if len(p.rows) == 0 && p.loading {
		return false
	}
	return p.hasMore || p.blocked
}

// emptyStateFor 是这一帧列表里那一格空状态，nil 是「什么都不说」。
//
// 列表和网格**共用这一段**：两个控件的空状态长得不一样的话，用户切一下设置就会
// 看到「同一个空列表，换了个说法」——而那不是两件事。
//
// ⚠️ **返回的是整条 `emptyState` 而不是一句字**：插画位也是这一格的一部分，
// 而页面上不只有「一行都没有」这一种空——筛选完是空的（`downloadlist`）、一页
// 刚发出去还没回来（`loading`）都是，而它们说的话和画的东西不一样。
func (p *pagedRows) emptyStateFor(t *ui.Theme) *emptyState {
	switch {
	case len(p.rows) == 0 && p.loading:
		// 一行都还没有：第一页在途。这时候「正在加载…」就是列表全部的内容，
		// 哨兵先收起来——同一句摆两遍（空状态一句 + 页脚一句）是噪音。
		//
		// ⚠️ **不摆插画**：这一格说的是「还没好」，摆一枚「这里什么都没有」的
		// 图形等于说反了（见 `emptyState.Icon`）。
		//
		// ⚠️ **走 `loadingState`（「加载中」那一档）**：它和「没有结果」走同一个
		// 出口（`drawEmpty`），而**不是同一档东西**——没有插画、没有第二句、
		// 字是淡的（票 #138）。
		loading := loadingState("正在加载…")
		return &loading
	case len(p.rows) == 0 && p.blocked:
		// 一行都没有还失败了：页面上就留那一个重试按钮，一句话都不多说。
		return nil
	case len(p.rows) == 0:
		return &p.empty
	}
	return nil
}

// viewEmptyState 画那句空状态和它下面那句提示。列表和网格共用。
//
// ⚠️ **走的是 `drawEmpty` 那一个出口**，不是自己再写一遍：这一格和整页那一格
// （`viewEmpty`）摆在一起是同一件事，字体、间距、主次色、插画位都得一样；各写
// 一份的话，改一次留白两处就会漂开。
//
// ⚠️ **它不装主题**：这一格画在页面里面，而页面那一层已经装过了（铁律二）。
func (p *pagedRows) viewEmptyState(c *ui.Context, t *ui.Theme) {
	e := p.emptyStateFor(t)
	if e == nil {
		return
	}
	drawEmpty(c, "", *e, emptyRowPad)
}

// drawList 画那个虚拟列表（列表和紧凑两档）。
func (p *pagedRows) drawList(c *ui.Context) {
	t := c.Theme()
	n := p.listRows()

	p.syncState(1)

	// 选中挪了没有？挪了而且不是指针挪的，就是键盘挪的（焦点环靠它，理由见
	// `widgets/list/selection.go` 的 KeyboardMode）。
	//
	// ⚠️ **要在画行之前问**：焦点环是画在行上的，这一句晚了那一帧就少一圈环。
	//
	// ⚠️ **只在这一档问**：网格那一档 `p.sel` 记的是**第几首歌**（格子才是用户
	// 点的东西），和这里的**行下标**不是一回事，拿它比就是拿两套下标比。
	p.keys.Moved(p.sel)

	e := ui.List(c, &p.state, n).
		Grow(1).
		FillWidth().
		// ⚠️ **听一听输入**：只为了知道「上一次是键盘还是指针」（焦点环靠它，
		// 理由见 `widgets/list/selection.go` 的 KeyboardMode）。
		//
		// 返回 false 是刻意的：自己不吃这个事件，方向键挪游标、点击选行、Tab 换
		// 焦点都还在。
		HandleInput(p.keys.Input).
		// 双击这一行 = 播放它（Biu 的 `onDoubleClick`，不是单击）。
		OnSubmit(func() { p.playFrom(p.sel) }).
		// ⚠️ **焦点环只留在「还没选中任何一行」的时候**（票 #197）：框架默认给**整张
		// 列表**画一圈焦点环，而票面要的是「键盘走到**哪一行**都要有可见焦点指示」
		// ——指示落在那一行上（见 `widgets/list/selection.go`）。两处都画就是两圈环。
		//
		// 理由和那一处逐字一样：**还没选中行时环在列表上**（Tab 刚进来的那一瞬间，
		// 焦点在列表上），**选中行之后环跟着那一行走**。
		FocusRing(p.sel < 0).
		Children(func() { p.viewEmptyState(c, t) }).
		Rows(func(r ui.ListRow) { p.viewRow(r) }).
		TrackScroll(&p.scroll)

	// 量这一帧的容器宽，下一帧用。理由同 `drawGrid` 末尾那一句；列表这一档
	// 拿它判「稍后再看的投稿时间那一列放不放得下」（`laterTimeCols`）。
	if bw := e.Bounds().W; bw > 0 {
		p.width = bw
	}

	// 量滚动条的悬停（界面层下一帧据此换深一档，见 scrollbar.go）。
	p.hot = scrollbar.Hot(e)
}

// Hot 报上一次画时指针是不是压在这个列表的滚动条上。见 `widgets/list` 的
// `List.Hot`。
func (p *pagedRows) Hot() bool { return p.hot }

// drawGrid 画网格那一档：一行几首。
//
// ## 列数按**容器**的宽分档，不按窗口的
//
// 理由和 `widgets/list/grid.go` 那一句完全一样：页面左边还有侧栏和页内边距，窗口
// 宽不等于列表宽。用窗口宽算的话用户在同一窗口宽度下会在两页看到两种列数。
//
// ## 和 `widgets/list` 的 `Grid` 为什么是两份
//
// 这一份翻页列表整个是照着 `list/paged.go` 另写的（文件头写明了为什么），而行是
// 页面自己画的（多两列，见 `libraryRow`）。把网格接进 `list.PagedList` 就得先把它
// 那几列搬进 `list.Music`——那是更大的改动，不在这一票里。
func (p *pagedRows) drawGrid(c *ui.Context) {
	t := c.Theme()
	cols := p.columns(c)
	items := p.listRows()
	rows := (items + cols - 1) / cols

	p.syncState(cols)

	// 列数先记下来再画：`rowCount`（三重保障那一支）和下一帧的 `columns` 都要用它。
	p.cols = cols

	e := ui.List(c, &p.state, rows).
		Grow(1).
		FillWidth().
		// 双击一格 = 从那一首往后播（列表那一行是同一个意思，Biu 的
		// `onDoubleClick`）。网格里没有「双击哪一格」的问题——双击的那一格就是
		// 选中的那一格。
		OnSubmit(func() { p.playFrom(p.sel) }).
		Children(func() { p.viewEmptyState(c, t) }).
		Rows(func(r ui.ListRow) { p.viewGridRow(r, cols) }).
		TrackScroll(&p.scroll)

	// 量这一帧的容器宽，下一帧用。`Bounds` 读的是**上一帧**量好的盒子
	//（`ui/api_gen.go` 里 Bounds 的注释原话），所以窗口改宽之后列数晚一帧换。
	if bw := e.Bounds().W; bw > 0 {
		p.width = bw
	}

	p.hot = scrollbar.Hot(e)
}

// columns 是这一帧排几列。头一帧还没量到容器时先拿**列表那一块**的宽（`c.Size`
// 是窗口，比容器宽，估出来偏大）——宁可宽一帧也不要窄一帧，反过来会先闪一下两列。
func (p *pagedRows) columns(c *ui.Context) int {
	w := p.width
	if w <= 0 {
		w, _ = c.Size()
	}
	return list.GridColumnsFor(w)
}

// viewGridRow 画网格的一行：cols 首歌。
//
// 🔴 行里 panic 会一路穿出 Cocoa 事件循环把整个进程带走，所以整行兜一层
// （理由同 `viewRow`）。和列表那一行不同，网格这一行的兜法记在 `failedRow`：
// 键是这一行**第一首**的 ID。
func (p *pagedRows) viewGridRow(r ui.ListRow, cols int) {
	c, first := r.Context, r.Index*cols

	// 哨兵**独占一行**时（`len(p.rows)` 正好是列数的整数倍）：这一行里没有歌。
	if first >= len(p.rows) {
		ui.Grid(c).AlignSelf(ui.Stretch).Columns(cols).Gap(gridGap).Children(func() {
			ui.Box(c).Grow(1).AlignSelf(ui.Start).Children(func() { p.viewFooter(c) })
		})
		return
	}
	m := p.rows[first]
	if p.failed[m.ID] {
		ui.Text(c, "这一行画不出来").FontSize(scale.Font(c, scale.Size13)).TextColor(c.Theme().TextMuted).
			Padding(space.Px8).MaxLines(1)
		return
	}
	// ⚠️ **格子宽度要算出来交给页面**：封面按格子宽画（`widgets/list` 的
	// `viewCell` 就是这么做的），而页面自己不知道容器有多宽。写死一个数的话，
	// 窗口一窄封面就顶出格子——界面上看得出坏了，而量宽度的断言全绿。
	cell := list.CellWidth(p.width, cols)
	guard.Wrap(func(c *ui.Context) {
		// ⚠️ Stretch：格子要撑满这一行的高，选中那一格的底色才是一整块
		//（`widgets/list/grid.go` 的 viewRow 里同一句）。
		ui.Grid(c).AlignSelf(ui.Stretch).Columns(cols).Gap(gridGap).Children(func() {
			for k := range cols {
				i := first + k
				// 越过最后一格，或者这一格是「行画完了、又没装哨兵」的那一格：空着。
				// ⚠️ 第二个条件不能省，理由逐字见 `widgets/list/grid.go` 的 `viewRow`
				// 里那一段（不挡会越界取 `p.rows[i]`，崩在画这一行上）。
				if i > len(p.rows) || (i == len(p.rows) && !p.showFooter()) {
					return
				}
				// 🔴 **哨兵落在这一行的第 k 格**（不是第 0 格时），理由和
				// `widgets/list/grid.go` 的 `viewRow` 里那一支逐字一样：行是按列数
				// 切出来的，`len % cols` 不为 0 时哨兵就在某一行的中间，只判「整行从
				// 哨兵开始」会**看不见它**——症状是翻页失败之后页面上什么都不剩，
				// 而 `blocked` 闩住的是自动重试。
				if i == len(p.rows) {
					p.viewFooterCell(c)
					continue
				}
				p.viewCellGuarded(c, i, cell)
			}
		})
	}, func(any) { p.markFailed(m.ID) })(c)
}

// viewFooterCell 画末尾哨兵的那一格。
//
// ⚠️ 宽度 `Grow(1)` 而不是算出来的格子宽：这一格是**塞进一行里的**，而这一行的宽度
// 是那一行的歌决定的；写死一个宽会和旁边的格子对不上（这是 BMG-56 那条
// 「别量时长」的同一个坑——被推走的是左边，右边几列的 x 一个像素没变）。
func (p *pagedRows) viewFooterCell(c *ui.Context) {
	ui.Box(c).Grow(1).AlignSelf(ui.Start).Children(func() { p.viewFooter(c) })
}

// viewCellGuarded 画一格，画崩了只废掉那一格。
//
// ⚠️ **按格兜而不是按行兜**：一行有六格，一张坏封面要带走五首好的——这句话是
// `widgets/list` 里已经付过账的结论，这里照抄。
func (p *pagedRows) viewCellGuarded(c *ui.Context, i int, cell float32) {
	m := p.rows[i]
	if p.failed[m.ID] {
		ui.Text(c, "这一格画不出来").FontSize(scale.Font(c, scale.Size13)).TextColor(c.Theme().TextMuted).
			Padding(space.Px8).MaxLines(1)
		return
	}
	guard.Wrap(func(c *ui.Context) {
		// ⚠️ 菜单挂在**这一格**上（和列表挂在行上是同一件事）。
		//
		// ⚠️ i 在这一帧是**真的**：菜单被点中的那一帧框架会用新的一帧重跑每一格
		// 的回调，闭包里的 i 因此总是用户按下去那一格的下标（理由同 `viewRow`）。
		// `renderCell` 已经把那一格建出来了（和 `render` 同一个约定：收的是已经
		// 建好的元素），所以菜单和点击都挂在**建好的那一格**上，而不是外面再套一层
		// ——套一层的话菜单会被最里层那个带菜单的元素接管（`ui/menu.go` 的
		// menuTarget），而点击区会多出一圈空白。
		// 这一串只是往建好的那一格上挂菜单和点击，返回的元素没人要——和 `viewRow`
		// 里 `p.render(c, r).ContextMenu(...)` 同一个写法。
		p.renderCell(c, i, cell, p.sel == i).
			ContextMenu(func(m *ui.Menu) { p.menu(m, i) }).
			// 点一下选中这一首。**单击只选中不放**——理由和列表那一行一样（桌面端
			// 单击就换队列太容易误触，播放走双击和右键菜单）。
			OnClick(func() { p.sel = i })
	}, func(any) { p.markFailed(m.ID) })(c)
}

// viewRow 画一行。
//
// 🔴 行里 panic 会一路穿出 Cocoa 事件循环把整个进程带走，所以每一行自己兜
// 一层，**只废掉这一行**。
func (p *pagedRows) viewRow(r ui.ListRow) {
	c, i := r.Context, r.Index
	if i >= len(p.rows) {
		p.viewFooter(c)
		return
	}
	m := p.rows[i]
	if p.failed[m.ID] {
		ui.Text(c, "这一行画不出来").FontSize(scale.Font(c, scale.Size13)).TextColor(c.Theme().TextMuted).
			Padding(space.Px8).MaxLines(1)
		return
	}
	guard.Wrap(func(c *ui.Context) {
		row := i
		// ⚠️ 菜单挂在**这一行**上：MyGo 取最里层那个带菜单的元素
		//（`ui/menu.go` 的 menuTarget），挂到列表外面整列共用一份菜单。
		//
		// i 在这一帧是**真的**：菜单被点中的那一帧框架会用新的一帧重跑每一行
		// 的回调（`ui/menu.go` 里 `rt.consumed = true` 那一句），闭包里的 i
		// 因此总是用户按下去那一刻那一行的下标。行的身份由 Music.ID 担着，
		// 这里 i 只用来读当行。
		p.render(c, r).ContextMenu(func(m *ui.Menu) { p.menu(m, row) })
	}, func(any) { p.markFailed(m.ID) })(c)
}

// markFailed 记下画崩过的那一行，下一帧不重试。
func (p *pagedRows) markFailed(id string) {
	if p.failed == nil {
		p.failed = map[string]bool{}
	}
	p.failed[id] = true
}

// viewFooter 画末尾那一行：转圈，或者失败了让人点重试。
func (p *pagedRows) viewFooter(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).AlignSelf(ui.Stretch).Center().Gap(space.Px8).Padding(space.Px8).
		Children(func() {
			if p.blocked {
				// 行里的重试（不是错误页上那颗主按钮）：扁平配方，悬停才有一层淡底。
				if flatButton(c, "加载失败，点此重试").FontSize(scale.Font(c, scale.Size13)).Clicked() {
					p.blocked = false
					p.load(trigRetry)
				}
				return
			}
			if p.loading {
				ui.Spinner(c).Label("加载更多")
				ui.Text(c, "正在加载…").FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted)
			}
		})
}

// viewBackToTop 画回顶按钮：滚过 400 像素才出现。
//
// ⚠️ **画法在 `widgets/list` 的 `BackToTop` 那一个出口**：这一枚原先在这个文件和
// `widgets/list/paged.go` 里各写了一份，两份的数一样、代码不一样——改一份就有
// 一页对不上。那四个数（阈值 400、距右 40、距下 120、圆角全圆）是照上游抄的，
// 理由写在那一个出口的文件头上。
func (p *pagedRows) viewBackToTop(c *ui.Context) {
	// MaxY 是这一帧量出来的最大滚动量：0 就是内容还没列表高，压根滚不动。
	if p.scroll.MaxY <= 0 {
		return
	}
	list.BackToTop(c, p.scroll.Y, p.ScrollTop)
}

// playingMark 是「这一行正在放」那个标记。
//
// ⚠️ **直接用 `list.PlayingMark` 而不是另写一遍**：这两个页面自己画行、
// 不走 `widgets/list` 的卡片，可它们和那一套说的是同一件事——同一个应用里
// 两套列表写着两种说法，用户会以为是两个意思。
const playingMark = list.PlayingMark

// isPlaying 报这一行这一首是不是正在放的那首。
//
// 判据和理由全在 `list.Actions.IsPlaying` 那一段（按 `app.Item.Identity`，
// 不是行的序号也不是 `Music.ID`）。没装判据时一律当不在放。
func (p *pagedRows) isPlaying(m libraryRow) bool {
	if p.acts.IsPlaying == nil {
		return false
	}
	return p.acts.IsPlaying(m.Item)
}

// viewPlayingMark 是「这一格正在放」那个标记，画在副标题那一行的最下面。
//
// ⚠️ **列表那一档已经不用它了**：序号位置换成了跳动的波形（上游
// `music-list-item` 那一套，见 `rowheader.go` 的 `viewRowIndex`），而**网格那一档
// 还留着**——格子里没有序号那一列，副标题那一行本来就是「UP 名 + 时长」，把时长换成
// 「正在放」正好是同一件事（换掉而不是加在后面，理由见 `widgets/list/grid.go`）。
// 两种说法不是两种意思：网格那一句和波形认的是同一个 `hover.PlayingMark`。
//
// ⚠️ **无障碍标签带上了歌名**：读屏软件读的是标签而不是画出来的字，光说
// 「正在播放」的话用户听到的是一句没头没尾的话。顺带它让这一行在 Tester 里
// 可认（理由同 `widgets/list` 的 `card.go`）。
func viewPlayingMark(c *ui.Context, t *ui.Theme, title string) {
	// 主色当**字**：浅色下换成与底色 ≥4.5:1 的变体（理由见 `widgets/ink`）。
	ui.Text(c, playingMark).Label(title + " " + playingMark).
		FontSize(scale.Font(c, scale.Size11)).TextColor(ink.AccentOn(t)).MaxLines(1)
}

// playFrom 从第 i 行往后接进队列并开始播。
//
// 给的是**从这一行起的全部**，和 Biu 的「在 B 站打开这一首从这儿听」一致：点
// 了一首，接下来的队列就该是它和它后面那些，不是整个列表从头。
func (p *pagedRows) playFrom(i int) {
	if i < 0 || i >= len(p.rows) {
		return
	}
	items := make([]app.Item, len(p.rows)-i)
	for k := i; k < len(p.rows); k++ {
		items[k-i] = p.rows[k].Item
	}
	if p.acts.Play != nil {
		p.acts.Play(items)
	}
}

// RowActions 是右键菜单里「对这一行做点什么」的那几样。两页共用，所以放在
// 这里——**不要在两个页面的菜单里各写一份**：改漏一边的后果是「列表里能播、
// 网格里放不出来」那种最难查的错（BMG-10 的收尾说明点名过同一件事）。
type RowActions struct {
	Play func(i int) // 从第 i 行往后接进队列并开始播
	// PlayNext 把第 i 行排在**正在放的那首后面**（「下一首播放」）。
	//
	// 和 Add 的分界就是插在哪：Add 接在队尾（得等整张列表放完），这一条接在
	// 当前的下一位（这一首放完就是它）。
	PlayNext func(i int)
	Add      func(i int) // 把第 i 行追加到队列末尾
	Open     func(i int) // 在系统里打开第 i 行的来处
	// FavSelect 把第 i 行放进哪些收藏夹（开那个选择弹窗）。
	FavSelect func(i int)
	// Download 把第 i 行排进下载队列。
	Download func(i int)
}

// actions 返回这几个动作（两页的菜单拿它建菜单项）。
func (p *pagedRows) actions() RowActions {
	return RowActions{
		Play:      p.playFrom,
		PlayNext:  p.playNextOne,
		Add:       p.addOne,
		Open:      p.openOne,
		FavSelect: p.favSelectOne,
		Download:  p.downloadOne,
	}
}

// Row 是第 i 行。越界是 ok=false——菜单被点中的那一帧用户可能已经翻过页了。
func (p *pagedRows) Row(i int) (libraryRow, bool) {
	if i < 0 || i >= len(p.rows) {
		return libraryRow{}, false
	}
	return p.rows[i], true
}

// addOne 把第 i 行追加到队列末尾，不打断正在放的那首。
func (p *pagedRows) addOne(i int) {
	row, ok := p.Row(i)
	if ok && p.acts.Add != nil {
		p.acts.Add([]app.Item{row.Item})
	}
}

// playNextOne 把第 i 行排在正在放的那首后面。
//
// ⚠️ **底下那一层可能是 nil**：这一层不认接线是谁给的（测试里那份
// `list.Actions{}` 什么都没有，`FavSelect` 也是同一个处境，见 `favSelectOne`）。
// 判空之后什么也不做——**调用一个 nil 函数是当场崩**，而这一崩会一路穿出
// Cocoa 事件循环把进程带走（铁律四）。
func (p *pagedRows) playNextOne(i int) {
	row, ok := p.Row(i)
	if ok && p.acts.PlayNext != nil {
		p.acts.PlayNext(row.Item)
	}
}

// openOne 在系统里打开第 i 行的来处（B 站上那一条）。
func (p *pagedRows) openOne(i int) {
	row, ok := p.Row(i)
	if ok && p.acts.Open != nil {
		p.acts.Open(row.Item)
	}
}

// favSelectOne 开第 i 行的「放进哪些收藏夹」那个弹窗。
//
// ⚠️ **底下那一层可能是 nil**（`list.AppActions` 的 FavSelect 就是 nil，而那个
// 函数看不见界面、开不了弹窗——理由见 `list.actions.go`）。所以这里判空之后
// 什么也不做：`list.doAction` 那边的「说一句」在这条路上够不着，而菜单项是
// 这一层自己摆的，宁可按了没反应也不能调一个 nil 函数。
func (p *pagedRows) favSelectOne(i int) {
	row, ok := p.Row(i)
	if ok && p.acts.FavSelect != nil {
		p.acts.FavSelect(row.Item)
	}
}

// downloadOne 把第 i 行排进下载队列。
func (p *pagedRows) downloadOne(i int) {
	row, ok := p.Row(i)
	if ok && p.acts.Download != nil {
		p.acts.Download(row.Item)
	}
}

// Remove 在后台删掉第 i 行，删成功了把它从界面上拿走。
//
// 🔴 **删除是网络往返**，界面线程上直接发就是每点一次菜单冻一下窗口。所以走
// 后台 goroutine，结果投进信箱、下一帧在界面线程上落地（铁律一）。
//
// ⚠️ **不在界面线程上从列表里删**：删成功了要等这一行真的没了才刷新整页的
// 那一版，用户会看到自己删掉的那一条又出现在列表里——而 B 站那边它确实已经
// 没了。下次刷新又没了，那种错最难查。
func (p *pagedRows) Remove(i int, del func(context.Context, string) error) {
	row, ok := p.Row(i)
	if !ok || del == nil {
		return
	}
	id := row.ID
	// 代次在**界面线程上**取一份（p.gen 只由界面线程写，铁律一）：后台 goroutine
	// 再去读它就是数据竞争，`-race` 会当场报出来。
	gen := p.gen
	go func() {
		p.deliver(removeResult{gen: gen, id: id, err: del(context.Background(), id)})
	}()
}

// ─────────────────────────────────────────────────────────────────────────────
// 三重保障
// ─────────────────────────────────────────────────────────────────────────────

// trigger 是「这次翻页是被哪一条叫起来的」。三条保障走同一段代码，记下来只是
// 为了出错时说得清是哪一条。
type trigger uint8

const (
	trigFirstRender trigger = iota
	trigScrollEnd
	trigRefill
	trigRetry
)

func (t trigger) String() string {
	switch t {
	case trigFirstRender:
		return "首次渲染检查叫的"
	case trigScrollEnd:
		return "滚到底叫的"
	case trigRetry:
		return "用户点了重试"
	default:
		return "容器没填满叫的"
	}
}

// afterFrame 是三重保障的②③（①在 view 开头）。每一帧跑一次——三条保障不是
// 三个不同的钩子，是同一个判断（末尾那一行进没进画面）在不同时刻的三个取值。
func (p *pagedRows) afterFrame() {
	// 防重入闸门：正在拉、上一页失败了、或者已经到底，都不再拉。
	if p.loading || p.blocked || !p.hasMore {
		return
	}
	first, last := p.state.Visible()
	switch n := p.rowCount(); {
	case last < n-1:
		// 末尾还在画面外。什么都不做，等用户滚过去。
	case first > 0:
		// ② 滚到底：哨兵行进了画面，而且列表不在顶上（是用户滚过去的）。
		p.load(trigScrollEnd)
	default:
		// ③ 拉完之后再检一次：还在顶上、末尾已经画在屏幕上 = 全部内容一屏摆得下。
		// **这一支就是「筛选后只剩两页」的救命的那一支。**
		p.load(trigRefill)
	}
}

// rowCount 是这一帧**画出来的行数**（不是歌数）：网格那一档一行几首，所以行数
// 是拿列数除出来的。
//
// ⚠️ 用 `p.cols`（网格那一档**上一帧**的列数）而不是这一帧的：三重保障跑在画完
// 之后，而 `Visible()` 报的本就是**上一帧**画出来的行（`ui.ListState.Visible` 的
// 注释原话）。拿上一帧的列数除，行数和那些行才是同一帧的那一份。
func (p *pagedRows) rowCount() int {
	if !p.isGrid() {
		return p.listRows()
	}
	cols := p.cols
	if cols <= 0 {
		cols = 2 // 还没画过一帧，最窄那档（和 `widgets/list` 同一个兜底）
	}
	n := p.listRows()
	return (n + cols - 1) / cols
}

// listRows 是这一帧有几行，含末尾哨兵。行数只有一个算法（就是这里）。
func (p *pagedRows) listRows() int {
	if p.showFooter() {
		return len(p.rows) + 1
	}
	return len(p.rows)
}

// load 拉下一页。三重保障和用户点重试最后都走这里。
//
// 🔴 防重入就是第二行那把锁：三条保障会在同一帧里先后命中，没有它就是一次
// 滚动三个请求。
func (p *pagedRows) load(why trigger) {
	if p.loading || p.blocked || p.fetch == nil {
		return
	}
	if p.page > 0 && !p.hasMore {
		return
	}
	next := p.page + 1
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel, p.loading, p.err, p.why = cancel, true, nil, why
	// 🔴 **代次、已加载行数、筛选条件都在这里（界面线程上）取一份再交给后台**。
	// 它们全都只由界面线程写（Reload / setPreset / applyKeyword），后台 goroutine
	// 再去读就是数据竞争——而这种错在界面上几乎看不出来：多半是某一次筛选的结果配
	// 上了另一次的词，而那在 `-race` 之外永远验不出来。
	gen, loaded, q := p.gen, len(p.rows), p.snapshot()

	go func() {
		defer cancel()
		rows, more, err := p.fetchOne(ctx, next, loaded, q)
		p.deliver(libraryResult{gen: gen, page: next, rows: rows, hasMore: more, err: err})
	}()
}

// errFetcherPanic 是拉页器自己崩了之后变成的那个错。
var errFetcherPanic = errors.New("拉页的代码崩了")

// fetchOne 调 fetch，**崩了也变成一个错**。
//
// 🔴 不包这一层就是进程当场没了：fetch 跑在后台 goroutine 里，界面那一层的
// recover 一个都接不到它。而 fetch 正是「解析接口返回」的那一段，B 站一变就
// 在这里炸。
func (p *pagedRows) fetchOne(ctx context.Context, page, loaded int, q libraryQuery) (rows []libraryRow, more bool, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("biu: 第 %d 页的拉页器崩了：%v\n%s", page, rec, debug.Stack())
			rows, more, err = nil, false, fmt.Errorf("%w：%v", errFetcherPanic, rec)
		}
	}()
	return p.fetch(ctx, page, loaded, q)
}

// snapshot 取一份筛选条件的快照。**只能由界面线程调**（load 里）。
//
// 没装 query 回调时给零值（不筛、不带词）——那不是「忘了装」的常态，而是
// `NewPaged` 那一路（页面还没把筛选接上）本来就没有筛选。
func (p *pagedRows) snapshot() libraryQuery {
	if p.query == nil {
		return libraryQuery{}
	}
	return p.query()
}

// stop 停掉在途的那一页。它只是**不让结果落地**：真正的作废靠 gen。停请求是
// 为了别让服务端白做一遍。
func (p *pagedRows) stop() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.loading = false
}
