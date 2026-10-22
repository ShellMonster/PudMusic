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

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// searchDebounce 是关键词的防抖。500ms 是 Biu 上游那个 `wait`。
//
// ⚠️ **歌词搜索弹窗的防抖也用这一个，不要另起一个常量**（BMG-66）。界面上凡是「等
// 用户把词敲完再动」的地方都用它——两处各自写一个 500 的话，改一个忘了另一个，
// 而它们的行为不一致这件事界面上一点也看不出来。
//
// ⚠️ 别拿 `app.lyricsSaveDelay`（也在 500ms）当同一件事：那个是**调完偏移/字号到
// 写盘之间**的防抖，和「敲字」是两回事，只是碰巧同一个数。
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

	// seenDraft 是上一帧框里的字。**防抖的钟靠它重打**：用户每敲一个字都要重新
	// 开始等那 500ms（`useDebounceFn` 也就是这么做的）。从第一个字算起的话，
	// 用户慢慢打的时候会在中途就发出去一个还没输完的词。
	seenDraft string

	// pendingSince 是「敲下去、还没发出去」的起点。零值 = 没有待发的搜索。
	pendingSince time.Time

	// submitted 是这一帧搜索框有没有被回车提交。**它必须记在状态上**而不是当成
	// 一个返回值传下去：`viewFilters` 画完之后还要拿它判要不要换查询，而那一段
	// 和画筛选器不在同一个闭包里。
	submitted bool
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
	p.rows = newPagedRows(render, menu, fetch, acts, p.query)
	p.rows.SetEmpty(empty)
}

// query 报「这一页现在要用什么条件去拉」。
//
// 🔴 **它只由界面线程调**（`pagedRows.load` 在起 goroutine 之前调它），而它读的
// 那几样东西——生效的词、日期区间的两端——用户随时在界面上改着。后台 goroutine 要是
// 直接去读它们就是数据竞争（`go test -race` 会报，界面上则多半表现为「某一次筛选的
// 结果配上了另一次的词」）。这里返回的是一份**值拷贝**，后台拿着它就再也不用回头看
// 界面状态了。
func (p *libraryPage) query() libraryQuery {
	from, to, on := p.dates.Range()
	return libraryQuery{keyword: p.applied, from: from, to: to, on: on}
}

// apply 换查询：关键词或日期范围变了。清空、回顶、从第 1 页重来。
func (p *libraryPage) apply() { p.rows.Reload() }

// keywordChanged 报这一帧搜索条件变了没，变了就换查询。
//
// **防抖的钟在用户每敲一个字的时候重新打**：上一次敲的已经过去 400ms、这一下又
// 敲了一个字，那一下就得从头再等 500ms，而不是把上一个字的 400ms 接着算——否则慢
// 慢打的时候会在中途发出去一个还没输完的词（上游 `useDebounceFn(wait: 500)` 就是
// 每个输入都重置那个钟）。回车是例外，立刻换（见文件头）。
func (p *libraryPage) keywordChanged(now time.Time, submitted bool) bool {
	draft := strings.TrimSpace(p.draft)
	switch {
	case submitted:
		p.seenDraft = draft
		return p.applyKeyword(draft)
	case draft != p.seenDraft:
		// 用户又敲了一个字：把钟重打，这一帧不搜。
		p.seenDraft, p.pendingSince = draft, now
		return false
	case draft == p.applied:
		p.pendingSince = time.Time{}
		return false
	case now.Sub(p.pendingSince) >= searchDebounce:
		return p.applyKeyword(draft)
	}
	return false
}

// applyKeyword 把生效的词换掉，顺带把「待发」和「上一次敲的字」都清干净
// （不清的话下一帧又会认成「用户刚敲的」，于是每帧都判一遍防抖）。
func (p *libraryPage) applyKeyword(kw string) bool {
	p.pendingSince, p.seenDraft = time.Time{}, kw
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
func (p *libraryPage) viewKeywordBox(c *ui.Context) bool {
	box := ui.TextInput(c, &p.draft).
		Width(keywordBoxWidth).
		// ⚠️ Label 不能省：它不只是给测试用的——没有它读屏软件读不出这是一个搜
		// 歌名的输入框（占位文字不是标签），无头 Tester 也只认 label / text。
		Label(keywordBoxLabel).
		Placeholder(keywordBoxLabel)
	return box.Submitted()
}

// viewFilters 画「日期范围筛选 + 关键词框」那一块，返回「换条件了没」。
//
// ## 为什么搜索框**另起一行**（上游 `later/search.tsx` 是同一排）
//
// 上游那一排是 `flex justify-between`：日期选择器在左、输入框在右（`w-64`）。那个
// 排法在它那边的窗口宽度下摆得下 ours 摆不下：我们的页面区最窄是
// **900（`WindowOptions.MinWidth`）减掉侧栏和页内边距 ≈ 660**，而日期那一块光两个
// 输入框加四个预设就已经要 ~577，再加一个 256 的输入框要 849。
//
// **后果不是「挤一挤」，是叠在一起**：实测 900 宽时结束日期的框
// （X=667,W=130）和搜索框（X=700,W=200）重叠，点结束日期点的是搜索框，它的日历
// 打不开——而用户缩小窗口是为了看东西，不是为了点什么打不开。
//
// 也不能靠「窄了就换排法」绕开：宽度要等布局完才知道（`Bounds` 量的是上一帧的
// 盒子，`c.Size()` 是窗口不是页面区），改窗口大小之后的第一帧照样是错的，而那一帧
// 就是用户点下去的那一帧。
//
// 拆两行是唯一在**每一个**宽度上都对的排法，代价是竖着多一行的位置。
func (p *libraryPage) viewFilters(c *ui.Context, now time.Time) bool {
	changed := false
	ui.Column(c).Key("filter:bar").FillWidth().Gap(8).Children(func() {
		changed = p.dates.view(c, now)
		p.submitted = p.viewKeywordBox(c)
	})
	return changed
}

// listMetrics 是这两页一行里的内边距、间距和那几列的宽度。
//
// 两种：普通和紧凑（设置里的「数据显示样式」）。**做成一份而不是在画行的地方散落
// 几个 if**：一行的左右内边距和列宽是对齐的——序号那一列宽一点、封面大一点，
// 右边三列就会错位，而错位在界面上只是「有点挤」，没人往设置那档上想。
type listMetrics struct {
	padY, padX, gap float32
	cover           float32
	index           float32
	count, date     float32
	// pubDate 是「投稿时间」那一列的宽（稍后再看那一页多出来的一列）。
	pubDate    float32
	clock      float32
	progress   float32
	viewedAt   float32
	titleLines int
	titleSize  float32
	// height 是这一行的高。**它是定死的**（封面 + 上下内边距），和
	// `widgets/list` 的 `listRowHeight` 同一个道理：歌名只占一行，这一行由封面
	// 撑着，行高写死是为了让这个数**保证**是它，而不是「碰巧量出来是它」。
	height float32
}

// metricsOf 是「这一档该用哪一份尺寸」。
//
// ⚠️ 网格那一档**不查这张表**：格子是另画的（`drawLaterCell`），而那份的列宽是按
// 格子宽算的。这里只管列表和紧凑两档。
func metricsOf(v list.View) listMetrics {
	if v != list.ViewCompact {
		return listMetrics{
			padY: listPadY, padX: listPadX, gap: rowGap, cover: coverSide,
			index: colIndex, count: colCount, date: colDate, clock: colClock,
			pubDate: colPubDate, progress: colProgress, viewedAt: colViewedAt,
			// ⚠️ **歌名一行**（上游那一行是 `truncate`）：折行的话整张列表的行高
			// 就跟着歌名长短跳（理由见 `widgets/list/card.go` 的文件头）。
			// 字号 16 是 DESIGN.md 的「列表歌名 16」。
			titleLines: listTitleLines, titleSize: 16,
			// 行高：封面 48 + 上下内边距 8×2。
			height: coverSide + 2*listPadY,
		}
	}
	// 紧凑：**封面整块不画**、上下内边距 8 → 4、行高 64 → 40（和 `widgets/list`
	// 紧凑那一档同一个道理，见它 `card.go` 顶上那一段）。
	// 几个数字比 `widgets/list` 的紧凑那一份略宽一点点——这两页右边多两列，收得太
	// 狠的话歌名只剩几十像素（实测 900 宽的页面区下）。
	return listMetrics{
		padY: compactListPadY, padX: compactListPadX, gap: compactRowGap,
		cover: 0, index: compactColIndex,
		count: compactColCount, date: compactColDate, clock: compactColClock,
		pubDate:  compactColPubDate,
		progress: compactColProgress, viewedAt: compactColViewedAt,
		titleLines: 1, titleSize: 13,
		// 行高定死：这一档没有封面撑着，40 是它自己那一个值（改之前也是 40）。
		height: compactListRowHeight,
	}
}

// 紧凑那一档的行内尺寸。数在 `widgets/list/card.go` 那一组旁边，两个地方摆着同一个
// 概念的两套数——那是因为这两页的行**多两列**（播放量 / 观看时间），列宽算得不一样。
const (
	compactListPadY = 4
	compactListPadX = 6
	compactRowGap   = 16
	compactColIndex = 32
	// compactListRowHeight 是紧凑档那一行的高。定死的（同普通那一档），而这一档
	// 没有封面撑着——40 是行里歌名 + UP 名那两块留得下的那个高度。
	compactListRowHeight = 40
	compactColCount      = 74
	compactColDate       = 92
	compactColClock      = 50
	compactColProgress   = 110
	compactColViewedAt   = 118
	// compactColPubDate 是紧凑档「投稿时间」那一列的宽。和 `colPubDate`
	// 同一个算法（最长 YYYY-MM-DD，font 12 约 66 宽），这一档再收窄一点。
	compactColPubDate = 76
)

// rowFace 是这一行字该用的两个颜色 + 「正在放」那一档。两页共用（理由见
// `widgets/list/card.go` 的 `rowColors`）：选中跟着光标走、正在放跟着声音走，是两件
// 不同的事，分成两档而不是一种底色。
type rowFace struct {
	playing    bool
	title, own ui.Color
}

// libraryRowFace 算这一档。两页和它们的网格格子都走这一个。
func libraryRowFace(t *ui.Theme, selected, playing bool) rowFace {
	switch {
	case selected:
		// 主色底上的字。UP 名那一块压到 0.75：主色底太亮时它比正文色更站得住，也还是
		// 「次要信息」的样子。
		return rowFace{title: t.AccentText, own: t.AccentText.Alpha(0.75)}
	case playing:
		// 主色当**字**：浅色下换成与底色 ≥4.5:1 的变体（默认那个绿压白底
		// 1.9:1），深色下原样。理由同 `widgets/list/card.go` 的 `rowColors`。
		return rowFace{playing: true, title: ink.AccentOn(t), own: t.TextMuted}
	default:
		return rowFace{title: t.Text, own: t.TextMuted}
	}
}

// viewLibraryRowHead 画这两页**行首**那几块：序号（正在放的那一行是跳动的波形）、
// 封面（上面压着 hover 反馈）、标题 + UP 名。剩下右边那几列由各自的页面接上去。
//
// ⚠️ **它只画那几块、不建盒子**：调用方那一行已经有 `Gap` / `Padding` 了，这里再套一
// 层会把内边距算两遍（实测序号那一列整整右移 8，而两边都「看起来对」）。
//
// ⚠️ 只抽这一段，不抽整行：两页右边那几列**布局不一样**（稍后再看是播放量 /
// 加入时间 / 时长，历史记录是播放进度 / 观看时间），整行合起来会变成一串 if-else。
// 行首是完全一样的，抽出来两页就不可能画得不一样。
//
// ⚠️ **mt / face / onRow / play 都是外面给进来的**，这一段自己不问：
//
//   - mt 是那一档的行内尺寸（`metricsOf`）——和行右边那几列必须是同一份；
//   - face 是按「选中 / 正在放」判好的颜色（`libraryRowFace`）；
//   - onRow 是**指针在不在这行上**，而它问的是**外层那个 `ui.Row`**（这一段画在它
//     里面），在这里问自己只会得到 false（同 `widgets/list/card.go` 里「`Hovered` 要在
//     `Children` 之前问」那一段）；
//   - play 是按封面上那个三角做的事。
//
// ⚠️ **歌名只占一行**（`mt.titleLines` = 1，上游那一行是 `truncate`）：这一段只
// 画行首那几块，行高由**外面那一行**定死（`mt.height`）——所以这里不用管折行，
// 而折行本来也不该发生（折了的话整张列表的行高就跟着歌名长短跳，理由见
// `widgets/list/card.go` 的文件头）。
func viewLibraryRowHead(c *ui.Context, r ui.ListRow, m libraryRow, cover *imgcache.Cache,
	mt listMetrics, face rowFace, onRow bool, play func()) {
	// 序号；正在放的那一行换成一段跳动的波形（上游 `music-list-item`）。
	viewRowIndex(c, r.Index, face.playing, face.title, m.ID, m.Title, mt.index)

	// 封面，四态由 imgcache 画（空地址画空框，不画「失败」），上面压着 hover 反馈：
	// 压暗 + 一个 20px 的白色播放三角。
	//
	// ⚠️ **紧凑档不画封面**（`mt.cover` 是 0，上游紧凑那一支也没有封面），所以
	// 那一档连这块盒子都不建：建一个 0 宽的呢，外圈那个 hover 反馈就没有地方贴。
	if mt.cover > 0 {
		hover.RowCover(c, cover, m.Cover, mt.cover, onRow, m.Title, play)
	}

	ui.Column(c).Grow(1).AlignItems(ui.Start).Gap(4).Children(func() {
		ui.Text(c, m.Title).FontSize(scale.Font(c, mt.titleSize)).LineHeight(1.5).
			TextColor(face.title).MaxLines(mt.titleLines).Ellipsis("…")
		if m.Owner != "" {
			// UP 名 14（DESIGN.md「列表 UP 名 14」；上游是 `text-sm`）。
			ui.Text(c, m.Owner).FontSize(scale.Font(c, 14)).LineHeight(1).
				TextColor(face.own).MaxLines(1).Ellipsis("…")
		}
	})
}

// libraryMenuItems 是两页共用的那三项菜单。
//
// ⚠️ **每一项都要 Item 一遍**：菜单是这趟全部建出来的，只有被 Chosen 的那一项为
// true（`ui/menu.go` 的 buildMenu）。少建一项，那一项就永远点不到。
//
// 「下一首播放」和「添加到播放列表」**没有**：我们的队列只有「追加到末尾」和「从这
// 一首往后接」（`app.Queue` 到现在只有 `Append`），摆一个没有的项等于骗用户。
// 「下载音频 / 下载视频」也没有：要写盘、要选目录，是 PLAN 的 P-26，而摆一个按了
// 只说「还没接」的项不如不摆——那两行和「在 B 站打开」长得一样。
func libraryMenuItems(m *ui.Menu, row libraryRow, a RowActions, i int) bool {
	switch {
	case m.Item("播放").Disabled(!row.CanPlay).Chosen():
		// ⚠️ 放不了的那一类**画成灰的而不是不画**：不画的话同一份列表里有的行能播
		// 有的不能，用户会以为是坏的。PGC / 直播放不了（见 `app.playableOnline`）。
		a.Play(i)
	case m.Item("添加到队列").Disabled(!row.CanPlay).Chosen():
		a.Add(i)
	case m.Item("在 B 站打开").Chosen():
		a.Open(i)
	default:
		return false
	}
	return true
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
	// ⚠️ 画法整个在 `viewPageEmptyWithReason`（empty.go）：我的关注那一页也是这
	// 四样，原来两页各写一遍——而那正是这一票要消掉的「各画各的」。
	s.viewPageEmptyWithReason(c, key, title, lookup(s.router.Path()).Empty)
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
//
// 🔴 **歌词搜索弹窗共用这一个钟**（`lyricsSearching`，见 `lyricssearch.go`）：
// 用户一次只能在一处敲字，两个钟各自醒着就是两个定时器，而哪一个先停都不对。
// 另起一个的代价还不止是那一个 goroutine——它会让「防抖 500ms」这件事在代码里
// 出现两个出处，改一个忘了另一个时界面上一点也看不出来。
//
// 🔴 **搜索框的联想词那一档也共用它**（`suggestSearching`，见
// `navbar_suggest.go`）：理由一模一样——同一个搜索框上的同一个「等用户把词敲完」，
// 而且它那个钟不该在浮层收掉之后还醒着。
//
// 🔴 **收藏夹页那个按歌名找的输入框也共用它**（`collectionSearching`，见
// `collectionpage.go`）：同一个道理——「等用户把词敲完」在这一个应用里只该
// 有**一个**出处。
func (s *Shell) syncSearchWatch() {
	want := false
	if p := s.libraryPageAt(s.router.Path()); p != nil {
		want = !p.pendingSince.IsZero()
	}
	if s.lyricsSearching() || s.suggestSearching() || s.collectionSearching() {
		want = true
	}
	switch {
	case s.closed:
		// 关掉之后不许再装（理由见 navbar_feed.go 里 `s.feed.arm` 那一句）。
		if s.searchWatchStop != nil {
			s.searchWatchStop()
			s.searchWatchStop = nil
		}
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
