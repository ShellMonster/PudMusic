// 历史记录：Biu 的 `/history`（`biu/src/pages/history/`）。
//
// 和「稍后再看」是同一副骨架（关键词框 + 日期范围 + 会自己翻页的列表），共享的部
// 分在 `librarypage.go` 和 `library_rows.go`。这一页多出来的是**那一列播放进度**——
// PLAN.md 的 P-04 点名要的就是它。
//
// # 那一列抄上游 `pages/history/item.tsx`
//
//	formatProgress(看过多少, 总时长) = formatDuration(看过多少) + " / " + formatDuration(总时长)
//
// **非有这一列不可**：没有它用户看到的是一列标题，得自己回想看过多少。「听了一半」
// 这个状态在界面上完全看不出来，于是每一首都得从头听或者拖进度条试一遍。
//
// 另外两处：页头右上角的「清空历史记录」是批量删除、而本项目还没有一份**共用**
// 的确认弹窗（PLAN 的 C-43 / L-05：收藏夹批量删、本地音乐删文件/删目录、下载
// 记录删/清空各自有一份，这一页是剩下那几处之一），所以**不摆**；「记录播放
// 历史」那个开关改的是设置里已有的 `reportPlayHistory`（`app.Settings`），摆上
// 用户就不用去设置页里找。
//
// 🔴 本文件里的 view **不许**调 `c.SetTheme`（铁律二，见 theme.go）。
package ui

import (
	"context"
	"strings"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// historyPath 是历史记录那条路由，和 routes 表里登记的是同一个字符串。
const historyPath = "/history"

// 历史记录那一行的列宽。播放进度要放得下「12:34 / 45:00」，观看时间要放得下
// 「2026-10-16 21:04」——都是定死的，不然上下两行的这两列会左右跳。
const (
	colProgress = 132
	colViewedAt = 140
)

// progressText 是「看过多少 / 总共多长」。
//
// ⚠️ **「看过多少」没有时长也要写出来，零秒写「0:00」**：上游的 formatProgress 是
// `formatDuration(prog > 0 ? prog : 0) + " / " + …`，也就是零秒写「0:00」而不是留
// 空。只写总时长的话，一行「刚点开就退出的」和一行「看完了」在屏幕上长得一模一样——
// 而这一列存在的全部理由就是让人一眼看出这两者的区别（计划书的 P-04）。
//
// 没有总时长（直播、专栏那几类 B 站不给 duration）时只写看过多少：「12:34 / 0:00」
// 在屏幕上读起来像坏了，「12:34」是对的。
func progressText(progress, total time.Duration) string {
	played := playedText(progress)
	if d := clockText(total); d != "" {
		return played + " / " + d
	}
	return played
}

// playedText 是「看过多少」那一半，**零秒写「0:00」**。
//
// 为什么不用 `clockText`：那个是给「总时长」用的，零值返回空串表示**不知道多长**
// （B 站不给 duration）；而「看过多少」永远知道——没有就是 0 秒，写空串会让那一行
// 少一半信息。
func playedText(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d == 0 {
		return "0:00"
	}
	return clockText(d)
}

// viewedAtText 是观看时间那一列的字（上游的 "YYYY-MM-DD HH:mm"）。**精确到分**：
// 一屏几十行，秒在那儿没有任何用处，只会让字变宽。
func viewedAtText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

// fetchHistory 是历史记录那一页的拉页器。跑在后台 goroutine 里（见
// pagedRows.fetchOne），所以它**只认传进来的 `q`**，不回去读界面状态——理由和
// `fetchLater` 里那段一模一样。
func (s *Shell) fetchHistory(ctx context.Context, page, _ int, q libraryQuery) ([]libraryRow, bool, error) {
	items, more, err := s.app.LibraryOf().History(ctx, page, app.LibraryQuery{
		Keyword: q.keyword, From: q.from, To: q.to,
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
			// ⚠️ **没有 View**：历史记录那个接口不给播放量（`app.LibraryItem.View`
			// 的注释写着「只有稍后再看有」）。所以网格卡片封面底部那一条渐变上
			// **左半边不画**——而这不是「忘了填」，是这个接口没有。
			Item: it.Item,
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

	// 🔴 **没登录就说清，而且一个请求都不发**（票 #138）。理由同 later.go 那一处。
	if lookup(historyPath).RequiresLogin && !s.app.LoggedIn() {
		s.viewNeedsLogin(c, "page:"+historyPath, historyNotLoggedIn)
		return
	}

	p := &s.historyPage
	now := s.clock.Now()
	st := s.app.SettingsSnapshot()
	// 设置来自更新的版本时一个字都不许写回去：把开关关掉，别摆一个按了没反应
	// 的（和设置页同一个理由，见 settings_page.go）。
	ro := s.app.SettingsReadOnly()

	ui.Column(c).Key("page:"+historyPath).Grow(1).Padding(0, pagePadX).Gap(8).
		Children(func() {
			// 页头：标题在左，「记录播放历史」在右（上游是 `justify-between`）。
			ui.Row(c).FillWidth().Center().Children(func() {
				ui.Text(c, "历史记录").FontSize(scale.Font(c, pageTitleSize)).Grow(1)
				s.viewReportHistory(c, st.ReportPlayHistory, ro)
			})

			// 换查询要在**画列表之前**（理由见 later.go 里同一句）。
			if p.viewFilters(c, now) || p.keywordChanged(now, p.submitted) {
				p.apply()
			}

			// 表头**只在列表 / 紧凑两档**摆，理由见 later.go 那一处。
			//
			// ⚠️ **摆法（列表 / 紧凑 / 网格）在 `syncLibraryPage` 那一层装**，理由同
			// later.go 那一处（票 #161）。
			viewLibraryHeader(c, p.rows, historyHeaderSpec, historyHeaderKey)
			p.rows.View(c)
			s.noteScrollbar(p.rows.Hot())
		})
}

// syncHistory 是「历史记录」这一页每帧的副作用。
//
// ⚠️ **换查询（关键词 / 日期范围）不在这里**，理由见 `syncLater` 那一段。
func (s *Shell) syncHistory(c *ui.Context) {
	s.syncLibraryPage(historyPath, &s.historyPage, c)
}

// viewReportHistory 是那个「记录播放历史」的开关，改的是**盘上已经有的键**
// （`app.Settings.ReportPlayHistory`）。至于「改了之后真的少记一条历史吗」——那是
// 播放器上报那一票的活儿；那一票没做之前它只是一个开关，摆在页面上比藏在设置里
// 好找。
func (s *Shell) viewReportHistory(c *ui.Context, on, ro bool) {
	// ⚠️ 这里**不能** .Key()：Switch 的旋钮有动画，动画状态记在控件节点上，
	// 每帧换一个新节点等于动画每帧从头开始（见 settings_page.go 的 pickAutoStart）。
	sw := ui.Switch(c, &on).Label("记录播放历史").Disabled(ro)
	// 🔴 **按下去要真的写设置**。`on` 是这一帧的局部变量：框架只把用户拨的那一下
	// 写进它，下一帧 view 又从设置里读一遍填回来。只绑定不写，用户按了开关弹回去，
	// 而界面上看不出任何异样——这是「按了没反应」里最难查的一种。
	//
	// 写不出去（没挂设置、只读）走 `applySetting` 那一句提示，和设置页同一套。
	if !sw.Changed() {
		return
	}
	s.applySetting("记录播放历史", func(cur *app.Settings) { cur.ReportPlayHistory = on })
}

// drawHistoryRow 画第 r 行。行首和稍后再看共用（`libraryRowHead`），右边那两列
// 不同：播放进度 / 观看时间。
func (s *Shell) drawHistoryRow(c *ui.Context, r ui.ListRow) ui.Element {
	t := c.Theme()
	rows := s.historyPage.rows
	m := rows.Rows()[r.Index]
	playing := rows.isPlaying(m)
	mt := metricsOf(rows.Layout())
	face := libraryRowFace(t, playing)

	// ⚠️ **hover 要在 Children 之前问**（同 `widgets/list/card.go` 里那一处）。
	//
	// ⚠️ **行高定死**（`mt.height`）：理由同 `drawLaterRow` 里那一处。
	row := ui.Row(c).Key("history-row:"+m.ID).AlignSelf(ui.Stretch).Gap(mt.gap).
		Padding(mt.padY, mt.padX).Height(mt.height)
	onRow := row.Hovered()

	// 选中那一行：盖掉框架那一块实心主色底，键盘走到它上面时给它一圈焦点环
	//（理由见 `widgets/list/selection.go`）。
	//
	// ⚠️ **环的条件是「列表有键盘焦点」且「上一次输入是键盘」**：鼠标点过之后
	// 方向键挪了游标，那一行也得有指示（框架那个 `focusVisible` 方向键不会设回来）。
	row = list.SelectedRow(row, t, r.Selected(), onRow,
		rows.keys.On() && list.Focused(c, &rows.state))

	return row.Children(func() {
		viewLibraryRowHead(c, r, m, s.images, mt, face, onRow,
			func() { rows.playFrom(r.Index) })

		ui.Text(c, progressText(m.Progress, m.Duration)).Width(mt.progress).
			TextAlign(ui.End).FontSize(scale.Font(c, 12)).TextColor(t.TextMuted).MaxLines(1)
		ui.Text(c, viewedAtText(m.ViewedAt)).Width(mt.viewedAt).
			TextAlign(ui.End).FontSize(scale.Font(c, 12)).TextColor(t.TextMuted).MaxLines(1)

		// 行尾那枚「⋯」。点开的是**这一行的菜单**，和右键同一份（理由同
		// `drawLaterRow` 里那一处）。
		ui.Box(c).Width(list.OperationColWidth).Children(func() {
			list.Operation(c, list.OperationLabel(m.Title), face.title,
				func(m *ui.Menu) { rows.menu(m, r.Index) })
		})
	})
}

// drawHistoryCell 画网格那一档的**一格**（设置里的「数据显示样式」选了「网格」时）。
//
// 结构和 `drawLaterCell` 同一个形状，差的是下面那两行：历史记录没有播放量，
// 有的是「看过多少 / 多久」和观看时间（理由见 `drawLaterCell` 的注释：右边那几列
// 挪到歌名下面，不是丢掉）。
func (s *Shell) drawHistoryCell(c *ui.Context, i int, cell float32, selected bool) ui.Element {
	t := c.Theme()
	rows := s.historyPage.rows
	m, ok := rows.Row(i)
	if !ok {
		return ui.Box(c).Grow(1)
	}
	playing := rows.isPlaying(m)
	// ⚠️ **选中不再改这一格的颜色**（票 #197）：原来选中的那一格铺一整块实心主色底
	// （和列表选中行是同一套，`ui/table.go` 的 node.Selected），而拍板的是「点一行
	// 不再整行刷绿」——上游压根没有这一层。所以选中的那一格和没选中的格子长得
	// **一个像素都不差**。
	//
	// 剩下的唯一一档主色是「正在放」那一格：它跟着声音走，不跟着光标走。
	titleColor, metaColor := t.Text, t.TextMuted
	if playing {
		// 主色当**字**：浅色下换成与底色 ≥4.5:1 的变体（理由见 `widgets/ink`）。
		titleColor = ink.AccentOn(t)
	}

	box := ui.Column(c).Key("history-cell:" + m.ID).Radius(t.Radius).
		AlignSelf(ui.Stretch).Grow(1)
	// ⚠️ **hover 要在 Children 之前问**（同 `widgets/list/grid.go` 里那一处）。
	onCell := box.Hovered()

	return box.Children(func() {
		// 格子右上角那枚「⋯」。点开的是**这一格的菜单**，和右键同一份
		//（理由见 `drawLaterCell` 里那一处）。
		list.OperationCorner(c, list.OperationLabel(m.Title), func(m *ui.Menu) {
			rows.menu(m, i)
		})

		// 封面 + 它上面那圈 hover 反馈：右下角一个 40 的主色圆播放钮，封面底部一条
		// black/90 的渐变。**左边那一半空着**：历史记录的接口不给播放量（见
		// `libraryRow.View` 的注释），画一列空白比不画更难看。
		hover.CardCover(c, s.images, m.Cover, cell, laterCellCoverH, onCell,
			"", clockText(m.Duration), m.Title, func() { rows.playFrom(i) })

		ui.Column(c).Grow(1).AlignItems(ui.Start).Gap(4).PaddingY(cardGapForCell).Children(func() {
			// 歌名 16 / UP 名 14：和 `widgets/list` 的网格卡同一档
			// （DESIGN.md「网格：歌名 16、UP 14」）。
			ui.Text(c, m.Title).FontSize(scale.Font(c, 16)).LineHeight(1.5).
				TextColor(titleColor).MaxLines(titleLines).Ellipsis("…")
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				if m.Owner != "" {
					ui.Text(c, m.Owner).Grow(1).FontSize(scale.Font(c, 14)).LineHeight(1).
						TextColor(metaColor).MaxLines(1).Ellipsis("…")
				}
				if playing {
					viewPlayingMark(c, t, m.Title)
					return
				}
				ui.Text(c, clockText(m.Duration)).FontSize(scale.Font(c, 12)).
					TextColor(metaColor).MaxLines(1)
			})
			ui.Row(c).Gap(6).FontSize(scale.Font(c, 11)).Children(func() {
				ui.Text(c, progressText(m.Progress, m.Duration)).Grow(1).
					TextColor(metaColor).MaxLines(1)
				ui.Text(c, viewedAtText(m.ViewedAt)).TextColor(metaColor).MaxLines(1)
			})
		})
	})
}

// historyMenu 是这一行的右键菜单，抄上游 `history/menu.tsx` 的顺序：能放的那几项在
// 前，在 B 站打开，然后是**这一页自己的**删除。
//
// ⚠️ 「收藏 / 下载音频」排在**删除之前**，而不是像上游那样排在最后——删掉一条
// 播放记录之后想把它收藏回来是办得到的（收藏夹里那份还在），而把收藏夹里的东西
// 删了就没了。顺序摆在这里是因为它们属于「这一行是什么」，不属于「这一页怎么改」。
func (s *Shell) historyMenu(m *ui.Menu, i int) {
	p := &s.historyPage
	row, ok := p.rows.Row(i)
	if !ok {
		return
	}
	acts := p.rows.actions()

	switch {
	// 直播、专栏那几类在 B 站那边也放不了（上游的 `canPlay = business ===
	// "archive"`，这里由 app 层判好传下来）。
	case m.Item("播放").Disabled(!row.CanPlay).Chosen():
		acts.Play(i)
	// 「下一首播放」判的是同一个 CanPlay，理由见 laterMenu 里那一项。
	case m.Item("下一首播放").Disabled(!row.CanPlay).Chosen():
		acts.PlayNext(i)
	case m.Item("添加到队列").Disabled(!row.CanPlay).Chosen():
		acts.Add(i)
	case m.Item("在 B 站打开").Chosen():
		acts.Open(i)
	// 收藏 / 下载：和稍后再看那一页摆的是同两项，理由见 laterMenu 的注释。
	case m.Item("收藏").Chosen():
		acts.FavSelect(i)
	case m.Item("下载音频").Chosen():
		acts.Download(i)
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
