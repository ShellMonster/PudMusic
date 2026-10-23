// 双月日期选择器：Biu 那个 `src/components/date-range-picker/index.tsx` 的
// `HeroDateRangePicker`（`visibleMonths={2}`）。
//
// # 上游长什么样
//
//	components/date-range-picker/index.tsx:74-82
//
//	<div className="w-64">
//	  <HeroDateRangePicker
//	    aria-label="选择日期范围"
//	    value={dateRange}
//	    onChange={handleDateRangeChange}
//	    visibleMonths={2}
//	  />
//	</div>
//
// 也就是：**一个 256 宽（`w-64`）的按钮**，点开是一块**两个月并排**的日历。
// 上游那一块是 HeroUI 的弹层，我们这一块是框架的 `ui.Popover`。
//
// # 为什么自己画日历而不是 `ui.Calendar`
//
// 两个理由，都是「范围」这件事本身带来的：
//
//   - **框架那一版只能选一天**（选中那天画主色底，`mygo/ui/date.go` 的
//     `calendarGrid`），而范围选择器要画的是「两头实心、中间一段淡色」——
//     三样都表达不了。
//   - **框架那一版的月份是跟着选中的那天走的**：翻月会把选中的日子一起翻掉
//     （`coreCalendar` 传的是 `calendarGrid(c, date, date, true, …)`，月份和
//     游标是同一个指针）。于是「翻到上个月看看」这种最普通的动作会把用户
//     已经选好的区间改掉——而界面上看不出坏了。
//
// # 两个月是**一起**翻的
//
// 上游的 `visibleMonths={2}` 是两块挨着的日历，翻月是两块一起动（不是各翻各
// 的）。这里也是：`f.month` 是**左边那个月**，右边那个月恒定是它的下一个月。
//
// # 一周从周日开始
//
// 上游那一块走 HeroUI，星期几和一周从哪天开始都跟着系统区域走；而**这个应用
// 的界面全是中文**（没有多语言），所以表头照中文习惯摆（苹果日历中文版就是
// 从周日开始）。
//
// # 一处**故意的偏离**：日历上高亮的是「**正在筛的那一段**」
//
// 上游那边手动挑的那一份在选预设时会被清空（`setDateRange(null)`），所以它的
// 日历只反映**手动挑的**那一份。我们这里日历上高亮的是**正在筛的那一段**
// （不管它来自预设还是手动挑的）——不多一个状态，而且对用户更实诚：
// 「今天」那一档亮着的时候，日历上今天也是亮的，两处说的是同一件事。
//
// 而**什么都没筛的时候日历上什么都不亮**：`from` / `to` 里永远摆着一个真的
// 区间（不筛时摆的是今天，见 `setPreset`），而用户看着今天被高亮，会以为今天
// 正在筛。
package ui

import (
	"strconv"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// 日期选择器的尺寸。
//
// ⚠️ **256 是上游那个 `w-64`**（`date-range-picker/index.tsx:74`）。它管的是
// 触发键的宽度，而**弹出来的那一块比它宽得多**（两个月并排 ≈ 500）——上游
// 那边也是这样：触发键 256，弹层自己更宽。
const (
	// dateRangeW 是触发键的宽度。
	dateRangeW = 256
	// dateCellW / dateCellH 是日历里一天那一格的大小。
	//
	// ⚠️ **32 是框架自己那个日历用的数**（`mygo/ui/date.go` 的
	// `coreBox(c).Size(t.Space(8), t.Space(7))` = 32×28）。跟着它走的好处是
	// **点得着**：28 高的格子在 100% 缩放下已经偏小了，而这是同一个应用里
	// 第二个日历。
	dateCellW = 32
	dateCellH = 28
	// dateMonthGap 是两个月之间的距离。
	dateMonthGap = 8
	// dateGridGap 是日历里各行之间的距离（标题 / 星期 / 星期几那几行）。
	dateGridGap = 2
	// dateRangeLabel 是触发键的**无障碍名字**。
	//
	// ⚠️ 它和键上**看得见**的字不是一回事：看得见的那一句是当前区间
	// （`2026-10-01 ~ 2026-10-22`），而这一句是恒定的「这是个日期范围选择
	// 器」。测试靠它点开这一块（区间会变，靠字点就点不到了）。
	dateRangeLabel = "选择日期范围"
)

// dateWeekdays 是一行星期几。
var dateWeekdays = []string{"日", "一", "二", "三", "四", "五", "六"}

// dateRangePlaceholder 是触发键上「还没挑区间」时写的那一句。
const dateRangePlaceholder = "选择日期范围"

// viewRangePicker 画日期范围那个触发键，和它弹出来的那一块双月日历。
//
// ⚠️ **它自己管 `f.open`**：弹层开着的时候 `ui.PopoverBase` 才建面板，关着的
// 时候一个节点都不建（框架那一支）。而 `f.open` 是**跨帧**的状态——即时模式
// 下一帧一新建，局部变量下一帧就没了（和 `Shell.searchDraft` 同一个理由）。
func (f *dateFilter) viewRangePicker(c *ui.Context) {
	t := c.Theme()

	// 触发键。⚠️ **Label 是恒定的那一句**：键上看得见的字是当前区间，而区间
	// 会变——靠它点开这一块就点不到（而且无头 Tester 只认 label / text）。
	//
	// ⚠️ **日历图标在右边**（HeroUI 那个 DateRangePicker 就是这样：字在左、
	// 图标在右），而字那一段 `Grow(1)` 撑开——不撑的话它和图标会一起贴在
	// 左边，而 256 宽的键上左边那一小段字和右边一个图标，中间是一大片空。
	trig := ui.Button(c, "").Label(dateRangeLabel).
		Width(dateRangeW).Justify(ui.SpaceBetween)
	trig.Children(func() {
		ui.Text(c, f.rangeText()).FontSize(scale.Font(c, 12)).SingleLine().Grow(1)
		ui.Icon(c, icon(iconCalendar)).FontSize(iconMd).TextColor(t.TextMuted)
	})
	if trig.Clicked() {
		f.open = !f.open
		// 每次打开都回到当前区间的那个月（上游的 DateRangePicker 也是这样：
		// 它跟着 `value` 走）。而**正在挑的半截区间作废**——重新打开就是
		// 从头挑。
		f.pickingEnd = false
		f.month = startOfMonth(f.from)
	}

	// ⚠️ **弹层往左对齐**：框架那一支默认把面板贴在触发键的左边（往右长），
	// 而这一块有 500 宽——贴在左边的话在 900 宽的窗口上它会**长出窗口**，
	// 右半边直接被裁掉（而裁掉的那半边正是右边那个月）。
	//
	// 所以改成「面板的右边和触发键的右边对齐」（`AnchorBottomEnd` /
	// `AnchorTopEnd`）：面板往左长，最小窗口下也还在窗口里。
	ui.PopoverBase(c, trig, &f.open, func(panel ui.Element) {
		// 外观走共享出口（`popupPanel`）：圆角 / 底 / 描边 / 阴影四件一起，
		// 跟旁边那几个浮层（联想面板、歌词旁的旋钮、设置里的下拉）一模一样。
		// 这一处原来自己抄了框架 `stylePanel` 的一份：圆角抄的是 `t.Radius+2`
		//（跟着全局圆角走，默认 8 时是 10，不是 DESIGN 的 8），阴影抄的是固定
		// 18% 的黑（深色下太淡，面板和底下的内容糊成一片）。
		popupPanel(t, panel.
			AttachTo(trig, ui.AnchorBottomEnd, ui.AnchorTopEnd).
			MarginStart(8).
			Padding(datePanelPad))
		f.viewRangePanel(c)
	})
}

// datePanelPad 是弹层那一块的内边距。
const datePanelPad = 8

// viewRangePanel 画弹层里的那一块：两个月并排，两头各一枚翻月键。
func (f *dateFilter) viewRangePanel(c *ui.Context) {
	ui.Row(c).Gap(dateMonthGap).AlignItems(ui.Start).Children(func() {
		// 左边那枚「‹」：往前一个月。⚠️ **两个月一起动**——`f.month` 是左边
		// 那个月，右边那个月恒定是它的下一个月。
		//
		// ⚠️ **内边距清零**：扁平按钮左右各留 14，而这一枚只有 24 宽——
		// 不清零的话内容区是负的，字被裁掉（而那一枚窄到只剩一个箭头）。
		//
		// ⚠️ **Label 是「上一个月」而不是那个符号**：可见的字是「‹」，
		// 读屏软件念出来是「左尖括号」——那一句对用户什么也没说（这一票给
		// 确认键补了「确认 + 那个词」的名字，这里也得补）。
		if flatButton(c, "‹").Label("上一个月").Width(dateArrowW).Padding(0, 0).Clicked() {
			f.month = f.month.AddDate(0, -1, 0)
		}
		f.viewMonth(c, f.month)
		f.viewMonth(c, f.month.AddDate(0, 1, 0))
		if flatButton(c, "›").Label("下一个月").Width(dateArrowW).Padding(0, 0).Clicked() {
			f.month = f.month.AddDate(0, 1, 0)
		}
	})
}

// dateArrowW 是翻月键的宽度。
const dateArrowW = 24

// viewMonth 画一个月的日历：标题 + 一行星期几 + 6 行 7 天。
func (f *dateFilter) viewMonth(c *ui.Context, month time.Time) {
	t := c.Theme()
	first := startOfMonth(month)

	ui.Column(c).Gap(dateGridGap).Children(func() {
		ui.Text(c, first.Format("2006年1月")).FontSize(scale.Font(c, 13)).
			TextColor(t.Text).TextAlign(ui.Center).Grow(1)
		ui.Row(c).Children(func() {
			for _, wd := range dateWeekdays {
				ui.Text(c, wd).Width(dateCellW).TextAlign(ui.Center).
					FontSize(scale.Font(c, 11)).TextColor(t.TextMuted)
			}
		})
		// 从「1 号所在那一周的周日开始」排 6 行 42 格——多出来的那几格是下
		// 一个月的头几天（画成灰的），和框架自己那个日历一样。
		start := first.AddDate(0, 0, -int(first.Weekday()))
		for w := range 6 {
			ui.Row(c).Children(func() {
				for d := range 7 {
					f.viewDay(c, start.AddDate(0, 0, w*7+d), month)
				}
			})
		}
	})
}

// viewDay 画日历里的那一天。
//
// 颜色分三档（上游 HeroUI 那个范围选择器就是这么画的）：
//
//	区间两头  实心主色 + 主色上的字色
//	区间中间  主色 20% 的一层淡底
//	别的日子  没有底
//
// ⚠️ **不在这个月的日子是灰的**（和框架那个日历一样），但**它是区间的一头
// 时不灰**——用户挑的就是那一天，它灰着会让用户以为点不到。
func (f *dateFilter) viewDay(c *ui.Context, day, month time.Time) {
	t := c.Theme()
	// ⚠️ **Label 是完整日期，不是那个数字**：一格写着「3」，读屏软件念出来
	// 就是「3」——用户不知道是哪个月的三号（这一块一次摆着 42 格）。框架
	// 自己那个日历也是这么标的（`mygo/ui/date.go` 的
	// `i18n.FormatDate(locale, day, {Skeleton: "yMMMMd"})`）。
	cell := flatButton(c, strconv.Itoa(day.Day())).
		Label(day.Format("2006年1月2日")).
		Width(dateCellW).Height(dateCellH).Padding(0, 0).Radius(4)

	fg := t.Text
	if day.Month() != month.Month() {
		fg = t.TextMuted
	}
	switch {
	case f.isEdge(day):
		cell = cell.Background(t.Accent)
		// ⚠️ **区间那一头的字是主色上的那个字色**（白）：主色当底的时候字
		// 还是正文色就看不见了（浅色下亮绿压深字是 3.3:1 那一档）。
		fg = t.AccentText
	case f.inRange(day):
		cell = cell.Background(t.Accent.Alpha(0.2))
	}
	cell = cell.TextColor(fg)

	if cell.Clicked() {
		f.pickDay(day)
	}
}

// isEdge 报这一天是不是区间的**一头**（起始或结束）。
//
// ⚠️ **没在筛的时候什么都不亮**：`from` / `to` 里永远摆着一个真的区间（不筛的
// 时候摆的是今天，见 `setPreset`），而用户看着日历上今天被高亮，会以为今天
// 正在筛——而实际上什么都没筛。上游那边是同一个道理：手动挑的那一份在选
// 预设时就被清空了（`setDateRange(null)`）。
func (f *dateFilter) isEdge(day time.Time) bool {
	if f.pickingEnd {
		return sameDay(f.pendingFrom, day)
	}
	if !f.on {
		return false
	}
	return sameDay(f.from, day) || sameDay(f.to, day)
}

// inRange 报这一天是不是**落在区间中间**（**不含**两头——两头归 `isEdge`）。
func (f *dateFilter) inRange(day time.Time) bool {
	if f.pickingEnd || !f.on {
		return false
	}
	d := startOfDay(day)
	return d.After(startOfDay(f.from)) && d.Before(startOfDay(f.to))
}

// pickDay 是用户点了一个日子。
//
// 两段：先点的是**起始**，再点的是**结束**。
//
// ⚠️ **起始那一下不改 `from` / `to`**：它只把那一天记在 `pendingFrom` 上。
// 那样做的理由是**别让用户挑到一半就重拉一次列表**——而更要紧的是：在起始
// 挑好之前，`from` / `to` 还是上一次的值，那一段区间在画面上就是**上一次
// 挑的那一段**，不是「从这一天到那一天」。
//
// ⚠️ **点到起始之前**（也就是第二次点却点到了更早的一天）：从那一天重新开始
// 挑（上游的 DateRangePicker 也是这样——它把新点的当成新的起始）。
func (f *dateFilter) pickDay(day time.Time) {
	if !f.pickingEnd {
		f.pendingFrom = day
		f.pickingEnd = true
		return
	}
	if day.Before(startOfDay(f.pendingFrom)) {
		f.pendingFrom = day
		return
	}
	f.from = startOfDay(f.pendingFrom)
	f.to = endOfDay(day)
	f.pickingEnd = false
	// ⚠️ **「挑完了一段」这件事本身要单独记一笔**（`f.picked`）：挑出来的这一段
	// **可能和现在摆着的一样**——不筛的时候摆着的就是今天，而用户手动挑的恰好
	// 也是今天。只比 from / to 的话，这一次手动挑会被**静默丢掉**（页面不重拉、
	// 还写着「不按日期筛」）。
	f.picked = true
	// 挑完了就收起：上游那个 DateRangePicker 选满两端之后弹层就关了。
	f.open = false
}

// rangeText 是触发键上写的那一句。
//
// ⚠️ **没在筛的时候写「选择日期范围」而不是那两天**：不筛的时候 `from` / `to`
// 里摆着的是今天（`setPreset` 摆的），而用户看着「今天 ~ 今天」会以为今天正在
// 筛——这一句「不按日期筛」才是真的（它就写在旁边）。
func (f *dateFilter) rangeText() string {
	if !f.on {
		return dateRangePlaceholder
	}
	return f.from.Format("2006-01-02") + " ~ " + f.to.Format("2006-01-02")
}

// startOfMonth 是那个月的第一天。
func startOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

// sameDay 报 a 和 b 是不是同一天。
func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
