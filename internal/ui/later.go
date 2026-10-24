// 稍后再看：Biu 的 `/later`（`biu/src/pages/later/`）。
//
// 摆的是**标题 + 关键词框 + 日期范围筛选 + 一个会自己翻页的列表**。翻页、三重
// 保障、回顶、逐窗口唤醒、行身份全在 `library_rows.go` 里（两页共用），这一页只管
// 自己的表头、每一行和右键菜单。
//
// # 两处按自己的判断改了上游
//
//   - **表头多一列「加入时间」**（上游 `music-list-item/header.tsx` 只有「投稿时间」，
//     传的是 `item.pubdate`）。**上游自己的日期筛选筛的是 `add_at`**——跟着筛的那
//     个时间画，用户才看得见「我现在筛的是哪一段」，摆一个筛选没在用的时间是让人
//     以为筛错了。
//
//     ⚠️ **上游那一列「投稿时间」也在**（票 #142 补的，画的是 `pubdate`）：
//     两列并排，一列回答「我筛的是哪一段」，一列回答「哪首是哪天发的」。
//     拿一个顶另一个，总有一半问题没答案。**窄窗口下它们依次让位**给标题和
//     行尾的「⋯」（`laterTimeCols`）——窗口够宽时两列都在。
//
//   - **页头右上角那个「清除已看完」摆出来了**（上游 `later/index.tsx:218-224`）。
//     原来不摆的理由是「它是批量删除，而这一页没有确认弹窗」——现在**有**了：
//     `internal/ui/confirmmodal.go` 那一套是共用的（下载记录删/清空、本地音乐
//     删文件/删目录都走它），这一页也走同一套。⚠️ **确认之后请求跑在后台**，
//     清完了重新拉一遍列表（界面上的行还摆着，而 B 站那边已经没了）。
//
//     ⚠️ **它清的是 B 站那边那一份清单**：`POST /x/v2/history/toview/del` 带
//     `viewed=true`（上游 `service/history-toview-del.ts`），B 站那边**没有撤销**。
//
// 🔴 本文件里的 view **不许**调 `c.SetTheme`（铁律二，见 theme.go）。
package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/leading"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// laterPath 是稍后再看那条路由，和 routes 表里登记的是同一个字符串。
//
// 拿来和 `Route.Match` 比，而不是拿 `routes[i]`：那条表一改顺序，这里跟着错，
// 而且错得很安静——这一页退回占位页，没人看得出来。
const laterPath = "/later"

// 列宽。序号 / 封面 / 列间距那几个数取自 DESIGN.md 的「列表」那一行：序号 32
// （居中）、封面 48、列间距 16。右边那几列取上游同一个量级，定死是为了让它们上下
// 对齐（Biu 用 `tabular-nums`）。
const (
	colIndex  = 32
	colCount  = 90
	colDate   = 110
	colClock  = 60
	coverSide = 48
	listPadX  = space.Px8
	listPadY  = space.Px8
	rowGap    = space.ListGap
	// colPubDate 是「投稿时间」那一列的宽。这一页的画法有三档
	// （今天 / 昨天 / N天前 / MM-DD / YYYY-MM-DD），最长是 YYYY-MM-DD
	// （10 个字符，font 12 下约 66 宽），84 够。
	colPubDate = 84
	// titleLines 是**网格那一档**一格歌名最多几行。2 是 Biu 的 `line-clamp-2`
	// （上游的 music-card 就是两行）。
	titleLines = 2
	// listTitleLines 是**列表那一行**歌名最多几行。**1**——上游那一行是
	// `truncate`（`music-list-item/index.tsx:112`），多了就省略号；折成两行的话
	// 整张列表的行高就跟着歌名长短跳（理由见 `widgets/list/card.go` 的文件头）。
	listTitleLines = 1
)

// laterHeaderKey / historyHeaderKey 是这两行表头的身份。⚠️ **两个都要不一样**：同一个
// 父下面给两份一样的 Key 会被框架当错（无头 Tester 当场 panic 并报出代码行号）。
const (
	laterHeaderKey   = "later:header"
	historyHeaderKey = "history:header"
)

// buildLibraryPages 把「稍后再看」和「历史记录」两页接上。**在 New 里调一次**。
//
// 两页在这一行里一起建，是因为它们共用同一份操作接线（`s.listActions()`），
// 各自的部分只有拉页器（一行）和行画法（各一个方法）。分开两处建的话，
// 「忘了建某一页」这件事只有切过去看才知道。
//
// ⚠️ **用 `s.listActions()` 而不是 `list.AppActions(...)`**：那份里
// `FavSelect` 是 nil，而这两页的行右键「收藏」会落到那句「现在还不能用」
// 上去——收藏早就能用了（`app.FavoriteSong`），那句话是假的。
func (s *Shell) buildLibraryPages() {
	acts := s.listActions()
	s.laterPage.init(s.clock.Now(), s.drawLaterRow, s.laterMenu, s.fetchLater, acts, lookup(laterPath).Empty)
	// 🔴 网格那一档的画法（设置里的「数据显示样式」）也在这儿装上。**没装的话那一档
	// 退回列表**（见 `pagedRows.SetView` 的注释），而症状是「改了设置、这一页没变」。
	s.laterPage.rows.SetCell(s.drawLaterCell)
	s.historyPage.init(s.clock.Now(), s.drawHistoryRow, s.historyMenu, s.fetchHistory, acts, lookup(historyPath).Empty)
	// 网格那一档的画法（理由同上面那一处）。
	s.historyPage.rows.SetCell(s.drawHistoryCell)
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

// laterMinTitleWidth 是这一页的标题列至少留多宽。和 `widgets/list` 的
// `minTitleWidth` 是同一个数（80）：到这个宽度以下就先把「投稿时间」收掉。
const laterMinTitleWidth = 80

// laterFixedWidth 是这一行**除标题以外**的宽。withPubDate 报算不算新加的
// 「投稿时间」那一列。
func laterFixedWidth(mt listMetrics, withPubDate bool) float32 {
	// 孩子：序号 / 封面 / 标题 / 播放量 / 加入时间 / [投稿时间] / 时长 /「⋯」。
	items := 7
	w := mt.index + mt.cover + mt.count + mt.date + mt.clock +
		list.OperationColWidth + 2*mt.padX
	if withPubDate {
		items++
		w += mt.pubDate
	}
	return w + float32(items-1)*mt.gap
}

// laterTimeCols 报这一页此刻画哪几个时间列：新加的「投稿时间」和原有的
// 「加入时间」。
//
// 判据是**上一帧量到的列表宽**（`pagedRows.width`，由 `drawList` 末尾写）：
// 头一帧还没量，先照画，下一帧按真的宽度收——和 `widgets/list` 的
// `List.Columns` 同一个节奏。表头和行读的是同一个判据。
//
// 两级让位：
//
//  1. **先收「投稿时间」**（这一票新加的）：它放不下时收掉，把宽度还给标题
//     ——900 宽 + 侧栏 400 时加上它固定宽合计 536、内容只有 460，不收的话先
//     被压成 0 的是标题，接着行尾那枚「⋯」被挤出窗口（评审量到过）。
//  2. **基线那五列也留不出标题时再收「加入时间」**：900 + 400 时基线
//     固定宽 444、内容只有 460——标题只剩 16，画出来是「…」；900 + 480 时
//     基线固定宽 444、内容只有 380——标题 0、行尾的「⋯」被挤出画面。
//     收掉「加入时间」之后标题回到 134（400）/ 54（480），「⋯」也可见。
//
// 两列都收掉之后标题仍有 54（900 + 480），那是这一页在这个窗口下能给出的
// 最好结果；再往下就只剩窗口 900 加更宽侧栏，而侧栏上限就是 480。
func laterTimeCols(rows *pagedRows) (pubDate, addTime bool) {
	w := rows.width
	if w <= 0 {
		return true, true // 还没量过：先照全画，下一帧收
	}
	mt := metricsOf(rows.Layout())
	base := laterFixedWidth(mt, false)
	if w-(base+mt.pubDate+mt.gap) >= laterMinTitleWidth {
		return true, true
	}
	if w-base >= laterMinTitleWidth {
		return false, true
	}
	return false, false
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
			// 播放量：列表那一列和网格卡片封面底部那一条都是它（`Music.View`）。
			View: it.View,
			// 投稿时间：接口的 `pubdate`（`app.LibraryItem.PubDate`）。
			//
			// ⚠️ **它和这一页的「加入时间」是两个时间，两列都画**：
			// 「加入时间」是筛选器筛的那个 `add_at`（本文件头第一条），
			// 「投稿时间」是上游画的那个 `pubdate`。拿一个顶另一个的话，
			// 用户要么看不出筛的是哪一段、要么看不到「哪首是哪天发的」。
			PubTime: it.PubDate,
			Item:    it.Item,
		},
		Progress:   it.Progress,
		ViewedAt:   it.ViewedAt,
		View:       it.View,
		CanPlay:    it.CanPlay,
		CannotPlay: it.CannotPlay,
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

	// 🔴 **没登录就说清，而且一个请求都不发**（票 #138）。
	//
	// ⚠️ **这一支原来没有**：从地址栏或者前进后退进这一页，标题、日期筛选、
	// 关键词框、表头全都在，然后列表空着，或者末尾一行「加载失败，点此重试」——
	// 一句都没提「你要先登录」。用户会以为应用坏了或者网络有问题。
	//
	// ⚠️ **旗标在路由表里**（`Route.RequiresLogin`），而「要不要摆进侧栏」是
	// 另一个标志（`Route.HideInMenuWhenLoggedOut`）——两件事，两个标志。
	if lookup(laterPath).RequiresLogin && !s.app.LoggedIn() {
		s.viewNeedsLogin(c, "page:"+laterPath, laterNotLoggedIn)
		return
	}

	p := &s.laterPage
	now := s.clock.Now()

	ui.Column(c).Key("page:"+laterPath).Grow(1).Padding(space.None, pagePadX).Gap(space.Px8).
		Children(func() {
			s.viewLaterHeader(c)

			// 换查询的位置在**画列表之前**：Reload 会把已加载的行清掉，放在后面
			// 的话这一帧画的是清掉之前的样子，用户会先看到一屏旧的、再变成空、
			// 再变成新的。筛选和搜索框为什么分两行，见 `viewFilters`。
			if p.viewFilters(c, now) || p.keywordChanged(now, p.submitted) {
				p.apply()
			}

			// 表头**只在列表 / 紧凑两档**摆：网格里一格是一首歌，表头上那几列
			// 和格子对不上号（理由见 `rowheader.go` 文件头）。
			//
			// ⚠️ **摆法（列表 / 紧凑 / 网格）在 `syncLibraryPage` 那一层装**：它每一帧
			// 都要跟着设置里那个「数据显示样式」换，而满屏开着的时候这一页不画
			// （票 #161）——不挪的话，用户在满屏里改完设置回来，这一页还是旧的那一档。
			// 两个时间列放不放得下由 `laterTimeCols` 说，行那一处读的是同一个
			// 判据（见 drawLaterRow）。
			viewLibraryHeader(c, p.rows, func(mt listMetrics, key string) list.HeaderSpec {
				pubDate, addTime := laterTimeCols(p.rows)
				return laterHeaderSpec(mt, key, pubDate, addTime)
			}, laterHeaderKey)
			p.rows.View(c)
			s.noteScrollbar(p.rows.Hot())
		})

	// 弹窗画在这一列**外面**：它盖的是整个窗口，一个 `Grow(1)` 的页面列里放它
	// 的话面板会跟着页面走位（和下载记录那一页同一件事，见 `downloadlist.go`）。
	s.viewLaterClearConfirm(c)
}

// viewLaterHeader 是页头那一行：标题在左，「清除已看完」在右。
//
// 上游是 `flex items-center justify-between`（`later/index.tsx:218-224`）：左边一个
// `h1`，右边一枚 110×32 的按钮「🗑 清除已看完」。我们原来右边**整块空白**——
// 少了一个能清掉已看完视频的入口。
//
// ⚠️ **它一直摆着**（上游也是）：队列空的时候按它不报错，只是没什么可清。
func (s *Shell) viewLaterHeader(c *ui.Context) {
	ui.Row(c).Key("later:page-title").FillWidth().AlignItems(ui.Center).Children(func() {
		ui.Text(c, "稍后再看").FontSize(scale.Font(c, scale.PageTitle))

		// 上游是 `justify-between`：标题在左、按钮在右。中间的空白是 `Spacer`
		// 吃掉的。
		ui.Spacer(c)

		// ⚠️ **按下不直接清**：那是一次不可恢复的批量删除（B 站那边没有撤销），
		// 所以先弹一道确认（`viewLaterClearConfirm`）。
		if s.viewClearViewedButton(c).Clicked() {
			s.laterClearOpen = true
		}
	})
}

// 那一枚按钮的尺寸。抄上游 `<Button variant="flat" size="sm">`：HeroUI 的 sm 是
// 32 高，图标 18，文字 14（`text-sm`）。
//
// ⚠️ **上游那一枚是 110 宽**（实测 x=1074..1183），那是「8 内边距 + 18 图标 +
// 8 缝 + 5 个字 + 8 内边距」——我们照抄这几样，别自己另定一个宽度。
const (
	clearViewedH    = 32
	clearViewedIcon = 18
)

// viewClearViewedButton 是页头右上角那枚「清除已看完」。
//
// 上游：`variant="flat" size="sm" startContent={<RiDeleteBinLine size={18}/>}`。
//
// ⚠️ **上游那一枚有一层常驻的淡灰底**（HeroUI 的 flat + 默认色 =
// `bg-default-100`），我们**不留那一层底**：仓库里这一族动作键的规矩是「无描边、
// 无常驻底色，悬停才上一层淡色」（`flatbutton.go` 的文件头，BMG-70 那一票量的），
// 而这一页页头右边没有别的东西要跟它分主次。悬停 / 按下的反馈照旧在。
func (s *Shell) viewClearViewedButton(c *ui.Context) ui.Element {
	b := flatButton(c, "").Height(clearViewedH).Padding(space.None, space.Px8).Gap(space.Px8)
	b.Children(func() {
		ui.Icon(c, icon(iconDelete)).FontSize(clearViewedIcon)
		ui.Text(c, "清除已看完").FontSize(scale.Font(c, scale.Size14))
	})
	return b
}

// viewLaterClearConfirm 画「清除已看完」之前那道确认。
//
// ⚠️ **一定要问**：B 站那边没有「撤销」，清掉的就再也回不来（只有用户自己在
// B 站 APP 里加回来的那条路）。上游也问（`later/index.tsx:196-211`），标题是
// 「删除已观看完的视频？」、确认键上是「删除」——这里逐字照抄。
//
// ⚠️ **确认之后不直接清**：请求跑在后台（`pagedRows.ClearViewed`），清完了重新
// 拉一遍列表——界面上的行还摆着，而 B 站那边已经没了。
func (s *Shell) viewLaterClearConfirm(c *ui.Context) {
	if !s.laterClearOpen {
		return
	}
	switch confirmModal(c, &s.laterClearOpen, confirmSpec{
		// ⚠️ **danger（红）**：上游那一份没写 `type`，而它的默认值就是
		// `danger`（`components/confirm-modal/index.tsx:31`）。删掉的是一整批
		// 已看完的视频，红是对的。
		tone:    toneDanger,
		title:   "删除已观看完的视频？",
		confirm: "删除",
		cancel:  "取消",
	}) {
	case confirmAccepted:
		// 先关弹窗再发请求：请求回来之后要弹一句提示，而弹窗还开着的话提示就
		// 压在弹窗底下（同一件事见 `downloadlist_ops.go` 的 `viewConfirm`）。
		s.laterClearOpen = false
		s.laterPage.rows.ClearViewed(s.app.LibraryOf().ClearViewedLater)
	case confirmCancelled:
		s.laterClearOpen = false
	}
}

// syncLater 是「稍后再看」这一页每帧的副作用。
//
// ⚠️ **换查询（关键词 / 日期范围）不在这里**：那一条要读这一帧刚建出来的输入框
// （`viewFilters` 和 `box.Submitted()`），而满屏开着的时候这一页不画、用户也
// 碰不到那个框。收结果和起第一拉在 `syncLibraryPage` 那一层。
func (s *Shell) syncLater(c *ui.Context) {
	s.syncLibraryPage(laterPath, &s.laterPage, c)
}

// drawLaterRow 画第 r 行。
//
// ⚠️ **这一行的行高是定死的**（`mt.height`，理由在下面那条注释里）：歌名只占一行
// （上游那一行是 `truncate`），这一行由 48 的封面撑着。折行的话整张列表的行高就
// 跟着歌名长短跳，一屏能看几首歌会变来变去。
func (s *Shell) drawLaterRow(c *ui.Context, r ui.ListRow) ui.Element {
	t := c.Theme()
	rows := s.laterPage.rows
	m := rows.Rows()[r.Index]
	playing := rows.isPlaying(m)
	// 紧凑档只是这一行的几处尺寸变矮（设置里的「数据显示样式」）。
	mt := metricsOf(rows.Layout())
	face := libraryRowFace(t, playing)

	// ⚠️ **hover 要在 Children 之前问**（同 `widgets/list/card.go` 里那一处）：它报的
	// 是这一帧指针在不在这一行上，而封面上那圈反馈要等 Children 才建得出来。
	//
	// ⚠️ **行高定死**（`mt.height`）：歌名只占一行（上游那一行是 `truncate`），
	// 这一行由 48 的封面撑着——写死是为了让这个数**保证**是它，而不是「碰巧量出来
	// 是它」（理由同 `widgets/list/card.go` 的文件头）。
	row := ui.Row(c).Key("later-row:"+m.ID).AlignSelf(ui.Stretch).Gap(mt.gap).
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

		// ⚠️ **播放量是记好的那一份**（`rows.countText`）：现算的话每帧每行一次
		// 字符串格式化（票 #313）。
		ui.Text(c, rows.countText.At(r.Index, m.View, playCountText)).Width(mt.count).
			TextAlign(ui.End).
			FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).MaxLines(1)
		// 两个时间列放不放得下由 `laterTimeCols` 说（表头那一处读的是同一个
		// 判据）：窄窗口先收「投稿时间」，连基线都放不下时再收「加入时间」。
		pubDate, addTime := laterTimeCols(rows)
		if addTime {
			ui.Text(c, dayText(m.ViewedAt)).Width(mt.date).TextAlign(ui.End).
				FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).MaxLines(1)
		}
		// 投稿时间：上游这一页画的就是 `pubdate`（本文件头第一条）。
		//
		// ⚠️ **它和「加入时间」不是同一个时间**：筛选器筛的是 `add_at`，
		// 而「哪首是哪天发的」问的是 `pubdate`。
		if pubDate {
			ui.Text(c, list.PubTimeText(m.PubTime, s.now())).Width(mt.pubDate).
				TextAlign(ui.End).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).MaxLines(1)
		}
		// ⚠️ **时长是记好的那一份**（`rows.durText`）：理由同上面那一处。
		ui.Text(c, rows.durText.At(r.Index, m.Duration, clockText)).Width(mt.clock).
			TextAlign(ui.End).
			FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).MaxLines(1)

		// 行尾那枚「⋯」。点开的是**这一行的菜单**，和右键同一份（`rows.menu`
		// 就是右键那一条挂上去的那个函数）。
		//
		// ⚠️ **表头照它留了同样宽的空位**（`widgets/list` 的 `Header` 末尾那一个），
		// 两边一样宽表头和行才对得上。
		ui.Box(c).Width(list.OperationColWidth).Children(func() {
			// ⚠️ **无障碍名字是记好的那一份**（`rows.opText`）：理由同上面那两处。
			list.Operation(c, rows.opText.At(r.Index, m.Title, list.OperationLabel), face.title,
				func(m *ui.Menu) { rows.menu(m, r.Index) })
		})
	})
}

// drawLaterCell 画网格那一档的**一格**（设置里的「数据显示样式」选了「网格」时）。
//
// 结构照 `widgets/list` 的音乐网格（`grid.go` 的 viewCell）：封面 → 歌名 → 副标题
// 那一行。**右边那几列没有丢**：播放量 / 加入时间 / 时长挤到歌名下面那一行的两端
// ——丢掉的不是「排版」，是用户要看的数，而网格档照样得看得见。
func (s *Shell) drawLaterCell(c *ui.Context, i int, cell float32, selected bool) ui.Element {
	t := c.Theme()
	rows := s.laterPage.rows
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

	box := ui.Column(c).Key("later-cell:" + m.ID).Radius(t.Radius).
		AlignSelf(ui.Stretch).Grow(1)
	// ⚠️ **hover 要在 Children 之前问**（同 `widgets/list/grid.go` 里那一处）。
	onCell := box.Hovered()

	return box.Children(func() {
		// 格子右上角那枚「⋯」。点开的是**这一格的菜单**，和右键同一份
		//（理由见 `widgets/list` 的 `operation.go`）。
		// ⚠️ **无障碍名字是记好的那一份**（`rows.opText`）：理由同 `drawLaterRow`
		// 里那一处。
		list.OperationCorner(c, rows.opText.At(i, m.Title, list.OperationLabel), func(m *ui.Menu) {
			rows.menu(m, i)
		})

		// 封面按**格子宽**画，不写死：`list.CellWidth` 给的就是这一格实际拿到的
		// 宽（`widgets/list` 的网格卡片同一个做法）。写死 160 的话，窗口一窄到
		// 两列，封面就顶出格子外面去了。
		//
		// 上面那圈 hover 反馈：右下角一个 40 的主色圆播放钮，封面底部一条 black/90
		// 的渐变（**左边播放量右边时长**——这一页的播放量是有的，见 `libraryRow.View`，
		// 而 `widgets/list` 那一派本地音乐没有，所以那边那一半空着）。
		hover.CardCover(c, s.images, m.Cover, cell, laterCellCoverH, onCell,
			rows.countText.At(i, m.View, playCountText),
			rows.durText.At(i, m.Duration, clockText), m.Title,
			func() { rows.playFrom(i) })

		ui.Column(c).Grow(1).AlignItems(ui.Start).Gap(space.Px4).PaddingY(cardGapForCell).Children(func() {
			// 歌名 16 / UP 名 14：和 `widgets/list` 的网格卡同一档
			// （DESIGN.md「网格：歌名 16、UP 14」）。
			ui.Text(c, m.Title).FontSize(scale.Font(c, scale.Size16)).LineHeight(leading.Title).
				TextColor(titleColor).MaxLines(titleLines).Ellipsis("…")
			ui.Row(c).Gap(space.Px6).AlignItems(ui.Center).Children(func() {
				if m.Owner != "" {
					ui.Text(c, m.Owner).Grow(1).FontSize(scale.Font(c, scale.Size14)).LineHeight(leading.Owner).
						TextColor(metaColor).MaxLines(1).Ellipsis("…")
				}
				// 「正在放」占时长那一格：两样都画必然把时长挤成「0:」，而用户读的
				// 是「正在放」（理由见 `widgets/list/grid.go` 那一处）。
				if playing {
					viewPlayingMark(c, t, m.Title)
					return
				}
				ui.Text(c, clockText(m.Duration)).FontSize(scale.Font(c, scale.Size12)).
					TextColor(metaColor).MaxLines(1)
			})
			// 播放量 / 加入时间：列表里是右边两列，格子里放到最下面那一行。
			ui.Row(c).Gap(space.Px6).FontSize(scale.Font(c, scale.Size11)).Children(func() {
				// ⚠️ 理由同上面那几处（`rows.countText`）。
				ui.Text(c, rows.countText.At(i, m.View, playCountText)).Grow(1).
					TextColor(metaColor).MaxLines(1)
				ui.Text(c, dayText(m.ViewedAt)).TextColor(metaColor).MaxLines(1)
			})
		})
	})
}

// 网格那一档里一格的样子。**只定高**：封面高 140 和 `widgets/list` 的网格一致
// （Biu 的 music-card 就是这个高），而宽是按列数算出来的（`list.CellWidth`）——
// 写死一个宽的话窗口一窄封面就顶出格子。
const (
	laterCellCoverH = 140
	cardGapForCell  = space.Px8
)

// laterMenu 是这一行的右键菜单，八项：立即播放 / 从这一首往后听 / 下一首播放 /
// 添加到队列 / 在 B 站打开 / 收藏 / 下载音频 / 移除。
//
// ⚠️ **每一项都要 Item 一遍**：菜单是这趟全部建出来的，只有被 Chosen 的那一项为
// true（`ui/menu.go` 的 buildMenu）。少建一项，那一项就永远点不到。
//
// ⚠️ **菜单本体在 `Shell.libraryMenuItems`**（两页共用一份，见那里的注释）：这里
// 只说这一页是「稍后再看」——最后那一项因此是「移除」。
func (s *Shell) laterMenu(m *ui.Menu, i int) {
	s.libraryMenuItems(m, s.laterPage.rows, i, libraryLater)
}

// playCountText 是播放量那一列的字。**折法的出处只有一处**：`list.PlayCountText`
// （网格卡片封面底部那一条渐变上画的是同一个数，而本包依赖那一层，反过来会循环依赖）。
//
// 名字里带 `play` 是因为主干上 `settings_tabs_test.go` 有一个**同名的测试辅助函数**
// `countText(tt, want)`（数画面上有几个匹配的字）。同一个包里同名就是编译期的
// 「redeclared」，而那个错和这一票在做的事看起来毫不相干。
func playCountText(n int64) string { return list.PlayCountText(n) }

// dayText 是那一天的字（`2026-10-16`），没有就空串（上游 `formatSecondsToDate`）。
func dayText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

// clockText 是时长那一列的字：不足一小时 `m:ss`，超过一小时 `hh:mm:ss`。
//
// ⚠️ 这一列要定宽（`colClock`）且用等宽数字（Biu 的 `tabular-nums`），不然 3:05 和
// 10:05 两列会左右跳。
//
// 一小时以上**带小时**：写 `m:ss` 的话一首一小时的歌显示成「61:30」，用户得在心里
// 换算一次才知道那是多长。
//
// 🔴 **小时是两位**（票 #222 的评审）：播放条上那一份（`playbar.go` 的 `clock`）
// 照上游的 `trim: false` 写成 `01:15:00`，而这里原来写 `1:15:00`——**同一个
// 时长在两处有两种写法**，用户对着播放条和历史列表看同一首歌，会以为其中一处
// 坏了。
//
// ⚠️ **分钟那一位还是一位**（上游那边是两位，`00:05`）：票面只钉了小时那一份，
// 而这一列是定宽的（`colClock` 60、`colProgress` 132），两位分钟会让它宽一格。
func clockText(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	s := int(d.Round(time.Second).Seconds())
	if h := s / 3600; h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
