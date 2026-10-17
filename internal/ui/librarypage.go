// 「稍后再看」和「历史记录」这两页共用的那一摊界面状态（PLAN.md 的 P-03 /
// P-04）。
//
// 两页在界面上是同一件事：**一个关键词框 + 一排日期筛选 + 下面一个会自己翻页
// 的列表**。差的那部分只有三样——表头那一行、每一行长什么样、右键菜单里那几
// 项——它们在各自的页面文件里（`later.go` / `history.go`）。这里放共享的那些，
// 免得两页各写一份防抖、两份「换条件回第 1 页」，过两个月只改了一边。
//
// # 关键词的防抖是 500ms（抄上游）
//
// Biu 两页的搜索框都是 `useDebounceFn(fn, { wait: 500 })`
// （`later/search.tsx:17`、`history/search.tsx:17`）。不防抖的话用户每敲一个字
// 就发一次请求——B 站的风控不是摆设。
//
// 回车不等那 500ms（上游也是：`onKeyDown Enter` 直接调 `handleSearch`）。
// 用户按下回车就是想现在搜，让他等半秒是另一回事。
//
// # 换筛选条件一律回第 1 页
//
// 两页都是 `refreshList()`：清空、回顶、从第 1 页重来。翻到第 5 页再改关键词
// 却从第 5 页继续，用户看到的是「新条件的第 5 页」——多半是空的，而且他自己
// 不知道为什么。
package ui

import (
	"strings"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// searchDebounce 是关键词的防抖。500ms 是 Biu 上游那个 `wait`。
const searchDebounce = 500 * time.Millisecond

// libraryPage 是这两页共用的那一摊状态。
//
// **建一次，存进 Shell 的一份里**（`Shell.laterPage` / `Shell.historyPage`）。
// 不能每帧新建：搜索框里正在敲的字和「待发的搜索」都要跨帧活着。
type libraryPage struct {
	// rows 是下面那个会自己翻页的列表。建好之后不再换。
	rows *pagedRows
	// dates 是日期范围筛选。
	dates *dateFilter

	// draft 是搜索框里正在敲的字，applied 是**已经在筛**的那个词。
	//
	// 分成两个是因为即时模式每一帧都重画：直接拿框里的字去筛，用户每敲一个
	// 字就重拉一次（防抖解决的是频率，解决不了「框里的字和生效的词是同一个」
	// 这个混淆）。
	draft, applied string

	// pendingSince 是「敲下去、还没发出去」的起点。零值 = 没有待发的搜索。
	pendingSince time.Time
}

// init 把这一页接上。
//
// render / menu 是这一页的画法（页面上一个方法一个方法地传进来，它们要读
// `s.laterPage` / `s.historyPage` 自己那一份 rows），fetch 是它的拉页器，
// acts 是操作（`list.AppActions(app, notify)`，两页共用同一份接线），empty 是
// 一行都没有时说的话，now 是把两个日期输入框填成今天的那一下时钟。
func (p *libraryPage) init(now time.Time,
	render func(*ui.Context, ui.ListRow) ui.Element, menu func(*ui.Menu, int),
	fetch libraryFetcher, acts list.Actions, empty emptyState) {
	p.dates = newDateFilter(now)
	p.rows = newPagedRows(render, menu, fetch, acts)
	p.rows.SetEmpty(empty)
}

// apply 换查询：关键词或日期范围变了。清空、回顶、从第 1 页重来。
func (p *libraryPage) apply() { p.rows.Reload() }

// keywordChanged 报这一帧搜索条件变了没，变了就换查询。
//
// 它**顺带处理防抖**：框里的字和 applied 不一样时，先记下起点；到了 500ms
// 才真的换。回车是例外，立刻换（见文件头）。
func (p *libraryPage) keywordChanged(now time.Time, submitted bool) bool {
	draft := strings.TrimSpace(p.draft)
	if draft != p.applied && p.pendingSince.IsZero() {
		p.pendingSince = now
	}
	switch {
	case submitted:
		return p.applyKeyword(draft)
	case draft == p.applied:
		p.pendingSince = time.Time{}
		return false
	case now.Sub(p.pendingSince) >= searchDebounce:
		return p.applyKeyword(draft)
	}
	return false
}

// applyKeyword 把生效的词换掉，顺带把「待发」清干净。
func (p *libraryPage) applyKeyword(kw string) bool {
	p.pendingSince = time.Time{}
	if kw == p.applied {
		return false
	}
	p.applied = kw
	return true
}

// ─────────────────────────────────────────────────────────────────────────────
// 搜索框
// ─────────────────────────────────────────────────────────────────────────────

// viewKeywordBox 画那个搜索框，报「回车了没」。
//
// 摆在筛选器左边（Biu 的 `later/search.tsx` 是 `justify-between`：日期在左、
// 搜索框在右；两页摆的是同一排，我们把搜索框放右边，和上游一致）。
func (p *libraryPage) viewKeywordBox(c *ui.Context) bool {
	box := ui.TextInput(c, &p.draft).
		Width(keywordBoxWidth).
		// ⚠️ Label 不能省：它不只是给测试用的——没有它读屏软件读不出这是一个搜
		// 歌名的输入框（占位文字不是标签），无头 Tester 也只认 label / text。
		Label(keywordBoxLabel).
		Placeholder(keywordBoxLabel)
	return box.Submitted()
}

// viewLibraryMissing 是「B 站客户端没挂上」时这两页画的东西。
//
// **为什么不照样去请求、失败了再说**：那句话就晚了半秒，而且它长得跟「B 站
// 连不上」一模一样——用户会去检查网络、换网、清缓存，而真正的原因是这个应用
// 少装了一摊零件。直接说清是哪一件事，比让它猜要快得多。
//
// ⚠️ **照旧把 `routes` 表里那一句 Empty 也画出来**，而且它在前面。这一页平时
// 空着的时候画的就是那一句（见 `pagedRows.SetEmpty`），用户先看到熟悉的
// 「暂无稍后再看内容」，再看到「没接上接口」这个**这一次**特别的原因——而不是
// 两句话换来换去。少画那一句的话，从别的页面切过来的那条路（`homepage_test.go`
// 和 `settings_test.go` 都按 `lookup(path).Empty.Title` 认「切过去了」）就断了。
func (s *Shell) viewLibraryMissing(c *ui.Context, key, title string) {
	t := c.Theme()
	e := lookup(s.router.Path()).Empty
	ui.Column(c).Key(key).Grow(1).Center().Gap(8).Padding(24).
		Children(func() {
			ui.Text(c, title).FontSize(22).Bold()
			if e.Title != "" {
				ui.Text(c, e.Title).FontSize(15)
			}
			if e.Hint != "" {
				ui.Text(c, e.Hint).FontSize(12).TextColor(t.TextMuted)
			}
			ui.Text(c, "还没接上 B 站接口，这一页暂时拉不了内容。").FontSize(12).
				TextColor(t.Danger)
		})
}

// keywordBoxWidth 是搜索框的宽度。Biu 是 `w-64`（later）/`max-w-50`（history），
// 取同一个整数。
const keywordBoxWidth = 256

// keywordBoxLabel 是搜索框那一行的字。抄上游两页的 placeholder：
// 「搜索标题/UP主名称」（later/search.tsx:31、history/search.tsx:30 两处一字
// 不差）。
const keywordBoxLabel = "搜索标题/UP主名称"

// ─────────────────────────────────────────────────────────────────────────────
// 防抖那个定时器
// ─────────────────────────────────────────────────────────────────────────────

// syncSearchWatch 收「那个 500ms 的钟要不要醒着」。
//
// **每帧调一次**（在 `viewMain` 里），所以不管用户什么时候切走切回，醒着的
// 定时器一定会跟着停。漏了这一处就是一个每 125ms 醒一次、醒到进程退出的
// goroutine——用户看不出任何异样，只是这个应用再没省过电。
func (s *Shell) syncSearchWatch() {
	want := false
	if p := s.libraryPageAt(s.router.Path()); p != nil {
		want = !p.pendingSince.IsZero()
	}
	switch {
	case want && s.searchWatchStop == nil:
		// 醒得比防抖短一半：宁可多醒一次，不要到点了才醒（那会让搜索慢半拍）。
		s.searchWatchStop = s.clock.EveryFunc(searchDebounce/4, s.wake.invalidate)
	case !want && s.searchWatchStop != nil:
		s.searchWatchStop()
		s.searchWatchStop = nil
	}
}

// libraryPageAt 是路径对应的那一页（不在路径上是 nil）。
func (s *Shell) libraryPageAt(path string) *libraryPage {
	switch lookup(path).Match {
	case laterPath:
		return &s.laterPage
	case historyPath:
		return &s.historyPage
	}
	return nil
}
