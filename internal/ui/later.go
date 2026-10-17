// 稍后再看：Biu 的 `/later`（`biu/src/pages/later/`）。
//
// 摆的是**标题 + 关键词框 + 日期范围筛选 + 一个会自己翻页的列表**。翻页、三重
// 保障、回顶、逐窗口唤醒、行身份全在 `library_rows.go` 里（两页共用），这一页只管
// 自己的表头、每一行和右键菜单。
//
// # 两处按自己的判断改了上游
//
//   - **表头多一列「加入时间」**（上游 `music-list-item/header.tsx` 是「投稿时间」，
//     传的是 `item.pubdate`）。**上游自己的日期筛选筛的是 `add_at`**——跟着筛的那
//     个时间画，用户才看得见「我现在筛的是哪一段」，摆一个筛选没在用的时间是让人
//     以为筛错了。
//   - **页头右上角那个「清除已看完」不摆**（上游 `later/index.tsx:221`）。它是
//     **批量删除**，而本项目还没有一份**共用**的确认弹窗：收藏夹批量删、本地音乐
//     删文件/删目录、下载记录删/清空各自有一份（PLAN 的 C-43 / L-05 那一格就写
//     「6 处危险操作」，现在是 3 处），这一页是剩下那几处之一。没有确认就删，点错
//     了没法找回；不摆用户不会以为它坏了（它压根没在那儿），摆一个一按就真删的会。
//
// 🔴 本文件里的 view **不许**调 `c.SetTheme`（铁律二，见 theme.go）。
package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// laterPath 是稍后再看那条路由，和 routes 表里登记的是同一个字符串。
//
// 拿来和 `Route.Match` 比，而不是拿 `routes[i]`：那条表一改顺序，这里跟着错，
// 而且错得很安静——这一页退回占位页，没人看得出来。
const laterPath = "/later"

// 列宽。Biu 那一行是 `grid-cols-[40px_1fr_150px_150px_150px_40px]`，取同样的
// 量级。定死是为了让这几列上下对齐（Biu 用 `tabular-nums`）。
const (
	colIndex   = 40
	colCount   = 90
	colDate    = 110
	colClock   = 60
	coverSide  = 48
	listPadX   = 8
	listPadY   = 8
	rowGap     = 8
	titleLines = 2
)

// buildLibraryPages 把「稍后再看」和「历史记录」两页接上。**在 New 里调一次**。
//
// 两页在这一行里一起建，是因为它们共用同一份操作接线（`list.AppActions`），
// 各自的部分只有拉页器（一行）和行画法（各一个方法）。分开两处建的话，
// 「忘了建某一页」这件事只有切过去看才知道。
func (s *Shell) buildLibraryPages() {
	acts := list.AppActions(s.app, s.addToast)
	s.laterPage.init(s.clock.Now(), s.drawLaterRow, s.laterMenu, s.fetchLater, acts, lookup(laterPath).Empty)
	s.historyPage.init(s.clock.Now(), s.drawHistoryRow, s.historyMenu, s.fetchHistory, acts, lookup(historyPath).Empty)
}

// fetchLater 是稍后再看那一页的拉页器。
//
// 🔴 **它跑在后台 goroutine 里，只认传进来的 `q`，不许回去读界面上任何东西**：
// `q` 是 `pagedRows.load` 在界面线程上取好的一份快照（见 libraryQuery）。以前这里
// 是直接读 `p.applied` 和 `p.dates` 的，而那两样用户随时在改——那就是一处数据竞争
// （`-race` 会报，界面上多半是「某一次筛选的结果配上了另一次的词」）。
//
// ⚠️ `q.keyword` 是**已经在生效的那个词**，不是 `p.draft`（框里正在敲的那个）——
// 用户敲到一半发出去的是半截词。
func (s *Shell) fetchLater(ctx context.Context, page, loaded int, q libraryQuery) ([]libraryRow, bool, error) {
	// 没在筛的时候 q.from / q.to 都是零值，服务层那一句 addRange 就不发这两个参数。
	items, count, err := s.app.LibraryOf().ToView(ctx, page, app.LibraryQuery{
		Keyword: q.keyword, From: q.from, To: q.to,
	})
	if err != nil {
		return nil, false, err
	}
	rows := make([]libraryRow, 0, len(items))
	for _, it := range items {
		rows = append(rows, laterRowOf(it))
	}
	// 「还有没有」照 Biu 的 `later/index.tsx:59`：**全部已加载**的行数和 B 站给的
	// 总条数比。用「这一页满不满」去推是错的——筛选之后最后几页常常正好是满的，
	// 推出来的「还有」会让翻页那一层一直拉空页转圈。
	//
	// ⚠️ `loaded` 是拉这一页之前传进来的（界面线程上取的），不是这里读的
	// `p.rows`：那一份是界面状态，后台 goroutine 读它就是数据竞争。
	return rows, loaded+len(rows) < count, nil
}

// laterRowOf 把一行业务状态变成这一页的一行。
func laterRowOf(it app.LibraryItem) libraryRow {
	return libraryRow{
		Music: list.Music{
			ID:       it.ID,
			Title:    it.Title,
			Cover:    it.Cover,
			Owner:    it.Owner,
			Duration: it.Duration,
			Item:     it.Item,
		},
		Progress: it.Progress,
		ViewedAt: it.ViewedAt,
		View:     it.View,
		CanPlay:  it.CanPlay,
	}
}

// viewLater 是稍后再看这一页。
//
// ⚠️ 换查询（关键词 / 日期）之后**必须调 `Reload`**：翻到第 5 页再改条件却从
// 第 5 页继续，用户看到的是「新条件的第 5 页」——多半是空的，而他自己不知道为什么。
func (s *Shell) viewLater(c *ui.Context, _ *ui.Route) {
	// 🔴 铁律二：页面 view 的头一件正事就是装主题（SetTheme 只影响它之后
	// 创建的控件，漏了就会用系统色画出来，跟旁边的界面对不上）。
	s.applyFrameTheme(c)

	// 没挂上客户端就说清，而不是发一个注定失败的请求（见 viewLibraryMissing）。
	if !s.app.LibraryOf().Mounted() {
		s.viewLibraryMissing(c, "page:"+laterPath, "稍后再看")
		return
	}

	p := &s.laterPage
	now := s.clock.Now()

	ui.Column(c).Key("page:"+laterPath).Grow(1).Padding(16, 20).Gap(8).
		Children(func() {
			ui.Text(c, "稍后再看").FontSize(22).Bold()

			// 换查询的位置在**画列表之前**：Reload 会把已加载的行清掉，放在后面
			// 的话这一帧画的是清掉之前的样子，用户会先看到一屏旧的、再变成空、
			// 再变成新的。筛选和搜索框为什么分两行，见 `viewFilters`。
			if p.viewFilters(c, now) || p.keywordChanged(now, p.submitted) {
				p.apply()
			}

			laterHeader(c)
			p.rows.View(c)
		})
}

// laterHeader 是那一行表头。抄上游 `music-list-item/header.tsx`（见文件头）。
func laterHeader(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Key("later:header").FillWidth().Gap(rowGap).Padding(listPadY, listPadX).
		Children(func() {
			ui.Text(c, "#").Width(colIndex).TextAlign(ui.End).FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, "标题").Grow(1).FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, "播放量").Width(colCount).TextAlign(ui.End).FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, "加入时间").Width(colDate).TextAlign(ui.End).FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, "时长").Width(colClock).TextAlign(ui.End).FontSize(12).TextColor(t.TextMuted)
		})
}

// drawLaterRow 画第 r 行。
//
// ⚠️ 不给这一行设固定高度（见 `list/card.go` 顶上那段）：歌名折到第二行时整张
// 列表的行高就不一样了，那正是变高行存在的理由。
func (s *Shell) drawLaterRow(c *ui.Context, r ui.ListRow) ui.Element {
	t := c.Theme()
	m := s.laterPage.rows.Rows()[r.Index]

	// 选中那一行的底色由 `ui.List` 自己刷（主色底），字色得跟着换——不换就是
	// 主色底上的深色字，深色主题下直接看不见。
	titleColor, ownerColor := t.Text, t.TextMuted
	if r.Selected() {
		titleColor, ownerColor = t.AccentText, t.AccentText.Alpha(0.75)
	}

	return ui.Row(c).Key("later-row:"+m.ID).AlignSelf(ui.Stretch).Gap(rowGap).
		Padding(listPadY, listPadX).Children(func() {
		ui.Text(c, fmt.Sprint(r.Index+1)).Width(colIndex).TextAlign(ui.End).
			FontSize(12).TextColor(t.TextMuted).MaxLines(1)

		// 封面四态由 imgcache 画（空地址画空框，不画「失败」）。
		imgcache.View(c, s.images, m.Cover, coverSide, coverSide)

		ui.Column(c).Grow(1).AlignItems(ui.Start).Gap(4).Children(func() {
			ui.Text(c, m.Title).FontSize(14).LineHeight(1.5).
				TextColor(titleColor).MaxLines(titleLines).Ellipsis("…")
			if m.Owner != "" {
				ui.Text(c, m.Owner).FontSize(12).LineHeight(1).
					TextColor(ownerColor).MaxLines(1).Ellipsis("…")
			}
		})

		ui.Text(c, playCountText(m.View)).Width(colCount).TextAlign(ui.End).
			FontSize(12).TextColor(t.TextMuted).MaxLines(1)
		ui.Text(c, dayText(m.ViewedAt)).Width(colDate).TextAlign(ui.End).
			FontSize(12).TextColor(t.TextMuted).MaxLines(1)
		ui.Text(c, clockText(m.Duration)).Width(colClock).TextAlign(ui.End).
			FontSize(12).TextColor(t.TextMuted).MaxLines(1)
	})
}

// laterMenu 是这一行的右键菜单。上游 `later/menu.tsx` 是六项，这里留三项：
//
//   - **下一首播放 / 添加到播放列表**：我们的队列只有「追加到末尾」和「从这一首往后
//     接」（`app.Queue` 到现在只有 `Append`），没有「插到下一首」。上游那一支在我们
//     这儿就是「添加到队列」。
//   - **下载音频 / 下载视频**：要写盘、要选目录，是 PLAN 的 P-26。摆一个按了只说
//     「还没接」的项不如不摆——那两行和「在 B 站打开」长得一样，用户分不出哪个能用。
//
// ⚠️ **每一项都要 Item 一遍**：菜单是这趟全部建出来的，只有被 Chosen 的那一项为
// true（`ui/menu.go` 的 buildMenu）。少建一项，那一项就永远点不到。
func (s *Shell) laterMenu(m *ui.Menu, i int) {
	p := &s.laterPage
	row, ok := p.rows.Row(i)
	if !ok {
		return
	}
	acts := p.rows.actions()

	switch {
	case m.Item("播放").Disabled(!row.CanPlay).Chosen():
		// ⚠️ PGC（番剧 / 纪录片）放不了：取流那一套对 PGC 不通（见
		// `app.playableOnline`），上游也是不给这一类入口（`cannotPlay = is_pgc`）。
		// **画成灰的而不是不画**：不画的话同一份列表里有的行能播有的不能，用户会
		// 以为是坏的。
		acts.Play(i)
	case m.Item("添加到队列").Disabled(!row.CanPlay).Chosen():
		acts.Add(i)
	case m.Item("在 B 站打开").Chosen():
		acts.Open(i)
	case m.Item("移除").Chosen():
		p.rows.Remove(i, s.app.LibraryOf().RemoveLater)
	}
}

// playCountText 是播放量那一列的字，抄上游的 `Intl.NumberFormat` compact：往上折成
// 「1.2万」「1.2亿」。不折的话七位数播放量会把那一列撑到别列头上。
//
// 名字里带 `play` 是因为主干上 `settings_tabs_test.go` 有一个**同名的测试辅助函数**
// `countText(tt, want)`（数画面上有几个匹配的字）。同一个包里同名就是编译期的
// 「redeclared」，而那个错和这一票在做的事看起来毫不相干。
func playCountText(n int64) string {
	switch {
	case n <= 0:
		return ""
	case n >= 100_000_000:
		return fmt.Sprintf("%.1f亿", float64(n)/1e8)
	case n >= 10_000:
		return fmt.Sprintf("%.1f万", float64(n)/1e4)
	default:
		return fmt.Sprint(n)
	}
}

// dayText 是那一天的字（`2026-10-16`），没有就空串（上游 `formatSecondsToDate`）。
func dayText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

// clockText 是时长那一列的字：不足一小时 `mm:ss`，超过一小时 `hh:mm:ss`。
//
// ⚠️ 这一列要定宽（`colClock`）且用等宽数字（Biu 的 `tabular-nums`），不然 3:05 和
// 10:05 两列会左右跳。
//
// 一小时以上**带小时**：写 `mm:ss` 的话一首一小时的歌显示成「61:30」，用户得在心里
// 换算一次才知道那是多长。
func clockText(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	s := int(d.Round(time.Second).Seconds())
	if h := s / 3600; h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
