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
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
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

// backTopPx 是滚过多少像素才出现回顶按钮。**400 是死数，照抄**
// Biu `src/components/scroll-container/index.tsx`。
const backTopPx = 400

// 回顶按钮摆在哪儿。Biu 是 `fixed right-10 bottom-[120px]`（躲开播放条）。
const (
	backTopRight  = 40
	backTopBottom = 120
	// backTopRadius 是药丸形（Biu 的 `radius="full"`）。
	backTopRadius = 999
)

// pagedRows 是一个会自己往下翻页的行列表。
//
// 建一次，存进页面自己的状态里，每一帧调 View。
type pagedRows struct {
	// draw 是包了 recover 的画法，见 View。建的时候包一次就够。
	draw func(*ui.Context)

	// state 是这一份列表的滚动位置和选中项（铁律四的关键，见 view）。
	state ui.ListState
	sel   int

	// render 画这一行并**把它交回来**（菜单要挂在它身上，见 viewRow）。收整个
	// ListRow 而不是光一个下标，是为了让页面能看见 `r.Selected()`——选中行的字色
	// 和底色是一对。menu 是这一行的右键菜单。两者都由页面给。
	render func(c *ui.Context, r ui.ListRow) ui.Element
	menu   func(m *ui.Menu, i int)

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
func (p *pagedRows) View(c *ui.Context) { p.draw(c) }

// onPanic 记一笔并要一帧。
//
// 要下一帧是必须的：recover 接住的是 build pass 中间，那一帧剩下的部分没建，画出
// 来是半张。**这里不换错误页**：坏的是这一个控件，页面上别的东西照常用。
func (p *pagedRows) onPanic(rec any) {
	log.Printf("biu: 分页行列表画崩了：%v\n%s", rec, debug.Stack())
	p.invalidate()
}

// view 是真的画法，View 是它外面那道 recover。
func (p *pagedRows) view(c *ui.Context) {
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

	p.syncState()
	p.drawList(c)

	// ②③ 在这里跑（必须排在画完之后：`Visible` 报的是**上一帧**的行，
	// mygo 的 ListState.Visible 注释：「the row the list showed in the last frame」）。
	p.afterFrame()
	p.viewBackToTop(c)
}

// syncState 把这一帧的行身份、名字和选中项装进 ListState。
//
// 🔴 铁律四：**Key 每一帧都重装，装的是 Music.ID**。不装的话 MyGo 拿「父 ID + 兄弟
// 序号」当身份，前面插一行会让后面所有行的状态整体错配。⚠️ 别改成序号。
func (p *pagedRows) syncState() {
	p.state.Key = func(i int) any {
		if i >= len(p.rows) {
			return footerIdentity{}
		}
		return p.rows[i].ID
	}
	p.state.Label = func(i int) string {
		if i >= len(p.rows) {
			return "加载更多"
		}
		return p.rows[i].Title
	}
	p.state.Selected = &p.sel
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

// drawList 画那个虚拟列表。
func (p *pagedRows) drawList(c *ui.Context) {
	t := c.Theme()
	n := p.listRows()

	empty := ""
	switch {
	case len(p.rows) == 0 && p.loading:
		// 一行都还没有：第一页在途。这时候「正在加载…」就是列表全部的内容，
		// 哨兵先收起来——同一句摆两遍（空状态一句 + 页脚一句）是噪音。
		empty = "正在加载…"
	case len(p.rows) == 0 && p.blocked:
		// 一行都没有还失败了：页面上就留那一个重试按钮，一句话都不多说。
	case len(p.rows) == 0:
		empty = p.empty.Title
	}

	ui.List(c, &p.state, n).
		Grow(1).
		FillWidth().
		// 双击这一行 = 播放它（Biu 的 `onDoubleClick`，不是单击）。
		OnSubmit(func() { p.playFrom(p.sel) }).
		Children(func() {
			if empty == "" {
				return
			}
			ui.Column(c).Grow(1).Center().Gap(6).Padding(16).Children(func() {
				ui.Text(c, empty).FontSize(15).TextColor(t.Text)
				if p.empty.Hint != "" {
					ui.Text(c, p.empty.Hint).FontSize(12).TextColor(t.TextMuted)
				}
			})
		}).
		Rows(func(r ui.ListRow) { p.viewRow(r) }).
		TrackScroll(&p.scroll)
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
		ui.Text(c, "这一行画不出来").FontSize(13).TextColor(c.Theme().TextMuted).
			Padding(8).MaxLines(1)
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
	ui.Row(c).AlignSelf(ui.Stretch).Center().Gap(8).Padding(8).
		Children(func() {
			if p.blocked {
				if ui.Button(c, "加载失败，点此重试").FontSize(13).Clicked() {
					p.blocked = false
					p.load(trigRetry)
				}
				return
			}
			if p.loading {
				ui.Spinner(c).Label("加载更多")
				ui.Text(c, "正在加载…").FontSize(13).TextColor(t.TextMuted)
			}
		})
}

// viewBackToTop 画回顶按钮：滚过 400 像素才出现。
func (p *pagedRows) viewBackToTop(c *ui.Context) {
	// MaxY 是这一帧量出来的最大滚动量：0 就是内容还没列表高，压根滚不动。
	if p.scroll.Y <= backTopPx || p.scroll.MaxY <= 0 {
		return
	}
	t := c.Theme()
	// Overlay 把按钮浮在整窗之上（不占布局），Biu 的 `fixed` 就是这个意思。
	// Radius 给一个大数就是 Biu 的 `radius="full"`（药丸形）。
	ui.Overlay(c, func() {
		if ui.Button(c, "返回顶部").Absolute().
			Right(backTopRight).Bottom(backTopBottom).
			Radius(backTopRadius).Background(t.SurfaceHover).Clicked() {
			p.ScrollTop()
		}
	})
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
	Add  func(i int) // 把第 i 行追加到队列末尾
	Open func(i int) // 在系统里打开第 i 行的来处
}

// actions 返回这三个动作（两页的菜单拿它建菜单项）。
func (p *pagedRows) actions() RowActions {
	return RowActions{
		Play: p.playFrom,
		Add:  p.addOne,
		Open: p.openOne,
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

// openOne 在系统里打开第 i 行的来处（B 站上那一条）。
func (p *pagedRows) openOne(i int) {
	row, ok := p.Row(i)
	if ok && p.acts.Open != nil {
		p.acts.Open(row.Item)
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
	switch n := p.listRows(); {
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
