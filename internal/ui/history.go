// 历史记录：Biu 的 `/history`（`biu/src/pages/history/`）。
//
// 摆的和「稍后再看」是同一副骨架（关键词框 + 日期范围 + 会自己翻页的列表），
// 共享的部分在 `librarypage.go` 和 `library_rows.go`。这一页多出来的是**那一列
// 播放进度**——PLAN.md 的 P-04 里点名要的就是它。
//
// # 播放进度那一列抄上游 `pages/history/item.tsx`
//
//	formatProgress(progress, duration) = formatDuration(看过多少) + " / " + formatDuration(总时长)
//
// **为什么非有这一列不可**：没有它，用户看到的是一列标题，得自己回想看过多少。
// 「听了一半」这个状态在界面上完全看不出来，于是每一首都得从头听或者拖进度条
// 试一遍。
//
// # 表头抄上游 `pages/history/header.tsx`
//
//	#  标题  播放进度  观看时间
//
// # 「清空历史记录」先不做
//
// 上游页头右上角是一个批量删除（Biu `history/index.tsx:271`），确认弹窗是
// PLAN 的 C-43，还没做。没有确认就删没法找回，所以这一票不摆那个按钮。
//
// # 「记录播放历史」摆上了
//
// 上游页头上那个开关改的是设置里的 `reportPlayHistory`（Biu
// `history/index.tsx:262`），而那个键在设置里已经有了（`app.Settings`）。
// 开关摆上，用户就能在这个页上把它关掉——不然他要去设置页里找。
//
// 🔴 本文件里的 view **不许**调 `c.SetTheme`（铁律二，见 theme.go）。
package ui

import (
	"context"
	"strings"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// historyPath 是历史记录那条路由，和 routes 表里登记的是同一个字符串。
const historyPath = "/history"

// historyEmpty 是「一行都没有」时说的话。抄上游 `history/index.tsx:284`。
const historyEmpty = "暂无历史记录"

// 历史记录那一行的列宽。播放进度要放得下「12:34 / 45:00」，观看时间要放得下
// 「2026-10-16 21:04」——都是定死的，不然上下两行的这两列会左右跳。
const (
	colProgress = 132
	colViewedAt = 140
)

// progressText 是「看过多少 / 总共多长」。
//
// 没有总时长（直播、专栏那几类 B 站不给 duration）时只写看过多少：写
// 「0:00 / 0:00」在屏幕上读起来像坏了，而「12:34」是对的。
func progressText(progress, total time.Duration) string {
	p, d := clockText(progress), clockText(total)
	switch {
	case d == "":
		return p
	case p == "":
		return d
	default:
		return p + " / " + d
	}
}

// viewedAtText 是观看时间那一列的字。抄上游 `moment.unix(view_at).format(
// "YYYY-MM-DD HH:mm")`——**精确到分**，不是到秒：一屏几十行，秒在那儿没有
// 任何用处，只会让字变宽。
func viewedAtText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

// fetchHistory 是历史记录那一页的拉页器。跑在后台 goroutine 里（见
// pagedRows.fetchOne）。
func (s *Shell) fetchHistory(ctx context.Context, page, _ int) ([]libraryRow, bool, error) {
	p := &s.historyPage
	// 没在筛的时候两端都是零值，服务层那一句 addRange 就不发这两个参数。
	from, to, _ := p.dates.Range()
	items, more, err := s.app.LibraryOf().History(ctx, page, app.LibraryQuery{
		Keyword: p.applied, From: from, To: to,
	})
	if err != nil {
		return nil, false, err
	}
	rows := make([]libraryRow, 0, len(items))
	for _, it := range items {
		rows = append(rows, historyRowOf(it))
	}
	return rows, more, nil
}

// historyRowOf 把一行业务状态变成这一页的一行。
func historyRowOf(it app.LibraryItem) libraryRow {
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
		CanPlay:  it.CanPlay,
	}
}

// viewHistory 是历史记录这一页。
func (s *Shell) viewHistory(c *ui.Context, _ *ui.Route) {
	// 🔴 铁律二：页面 view 的头一件正事就是装主题。
	s.applyFrameTheme(c)

	// 没挂上客户端就说清，而不是发一个注定失败的请求（见 viewLibraryMissing）。
	if !s.app.LibraryOf().Mounted() {
		s.viewLibraryMissing(c, "page:"+historyPath, "历史记录")
		return
	}

	p := &s.historyPage
	now := s.clock.Now()
	st := s.app.SettingsSnapshot()
	// 设置来自更新的版本时一个字都不许写回去：把开关关掉，别摆一个按了没反应
	// 的（和设置页同一个理由，见 settings_page.go）。
	ro := s.app.SettingsReadOnly()

	ui.Column(c).Key("page:"+historyPath).Grow(1).Padding(16, 20).Gap(8).
		Children(func() {
			// 页头：标题在左，「记录播放历史」在右（上游是 `justify-between`）。
			ui.Row(c).FillWidth().Center().Children(func() {
				ui.Text(c, "历史记录").FontSize(22).Bold().Grow(1)
				s.viewReportHistory(c, st.ReportPlayHistory, ro)
			})

			ui.Row(c).FillWidth().Gap(16).Children(func() {
				datesChanged := p.dates.view(c, now)
				submitted := p.viewKeywordBox(c)
				// 换查询要在**画列表之前**（理由见 later.go 里同一句）。
				if datesChanged || p.keywordChanged(now, submitted) {
					p.apply()
				}
			})

			historyHeader(c)
			p.rows.View(c)
		})
}

// viewReportHistory 是那个「记录播放历史」的开关。
//
// 改的是**盘上已经有的键**（`app.Settings.ReportPlayHistory`），不是新造一个。
// 至于「这个开关改了之后真的少记一条历史吗」——那是播放器上报那一票的活儿，
// 那一票没做之前它只是一个开关；摆在页面上比藏在设置里好找。
func (s *Shell) viewReportHistory(c *ui.Context, on, ro bool) {
	// ⚠️ 这里**不能** .Key()：Switch 的旋钮有动画，动画状态记在控件节点上，
	// 每帧换一个新节点等于动画每帧从头开始（见 settings_page.go 的 pickAutoStart）。
	ui.Switch(c, &on).Label("记录播放历史").Disabled(ro)
}

// historyHeader 是那一行表头。抄上游 `pages/history/header.tsx`。
func historyHeader(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Key("history:header").FillWidth().Gap(rowGap).Padding(listPadY, listPadX).
		Children(func() {
			ui.Text(c, "#").Width(colIndex).TextAlign(ui.End).FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, "标题").Grow(1).FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, "播放进度").Width(colProgress).TextAlign(ui.End).
				FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, "观看时间").Width(colViewedAt).TextAlign(ui.End).
				FontSize(12).TextColor(t.TextMuted)
		})
}

// drawHistoryRow 画第 r 行。结构和稍后再看那一行一样，多出来的两列在右边。
func (s *Shell) drawHistoryRow(c *ui.Context, r ui.ListRow) ui.Element {
	t := c.Theme()
	m := s.historyPage.rows.Rows()[r.Index]

	titleColor, ownerColor := t.Text, t.TextMuted
	if r.Selected() {
		titleColor, ownerColor = t.AccentText, t.AccentText.Alpha(0.75)
	}

	return ui.Row(c).Key("history-row:"+m.ID).AlignSelf(ui.Stretch).Gap(rowGap).
		Padding(listPadY, listPadX).Children(func() {
		ui.Text(c, rowNumber(r.Index)).Width(colIndex).TextAlign(ui.End).
			FontSize(12).TextColor(t.TextMuted).MaxLines(1)

		imgcache.View(c, s.images, m.Cover, coverSide, coverSide)

		ui.Column(c).Grow(1).AlignItems(ui.Start).Gap(4).Children(func() {
			ui.Text(c, m.Title).FontSize(14).LineHeight(1.5).
				TextColor(titleColor).MaxLines(titleLines).Ellipsis("…")
			if m.Owner != "" {
				ui.Text(c, m.Owner).FontSize(12).LineHeight(1).
					TextColor(ownerColor).MaxLines(1).Ellipsis("…")
			}
		})

		ui.Text(c, progressText(m.Progress, m.Duration)).Width(colProgress).
			TextAlign(ui.End).FontSize(12).TextColor(t.TextMuted).MaxLines(1)
		ui.Text(c, viewedAtText(m.ViewedAt)).Width(colViewedAt).
			TextAlign(ui.End).FontSize(12).TextColor(t.TextMuted).MaxLines(1)
	})
}

// historyMenu 是这一行的右键菜单。
//
// 抄上游 `history/menu.tsx` 的顺序：能放的那几项在前，在 B 站打开，然后是
// 删除（上游把它标成 danger）。理由和 `laterMenu` 那段一样：只摆我们真能做的。
func (s *Shell) historyMenu(m *ui.Menu, i int) {
	p := &s.historyPage
	row, ok := p.rows.Row(i)
	if !ok {
		return
	}
	acts := p.rows.actions()

	switch {
	case m.Item("播放").Disabled(!row.CanPlay).Chosen():
		// 直播、专栏那几类在 B 站那边也放不了（上游的 `canPlay =
		// business === "archive"`，这里由 app 层判好传下来）。
		acts.Play(i)
	case m.Item("添加到队列").Disabled(!row.CanPlay).Chosen():
		acts.Add(i)
	case m.Item("在 B 站打开").Chosen():
		acts.Open(i)
	case m.Item("删除").Chosen():
		p.rows.Remove(i, s.app.LibraryOf().RemoveHistory)
	}
}

// rowNumber 是那一行前面的序号。Biu 是 1 起的（`#` 列）。
func rowNumber(i int) string { return strings.TrimSpace(itoa(i + 1)) }

// itoa 是不引 strconv 的那一小段。序号每一帧每一行都要算一次，一个字符串
// 拼接比一次类型转换便宜——这一列在几千行的列表上每帧都跑。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
