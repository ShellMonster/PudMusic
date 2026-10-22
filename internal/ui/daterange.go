// 日期范围筛选：Biu 那个 `src/components/date-range-picker/index.tsx` 的 4 个
// 预设 Tab 加上手动区间。
//
// # 手动区间是**双月日期选择器**（票 #176 / G34）
//
// 上游那一整个组件是 HeroUI 的 `DateRangePicker`（`visibleMonths={2}`）加四个
// `Tab`。日历那一半自己画了——理由在 `daterangepicker.go` 的文件头（框架的
// `ui.Calendar` 只能选一天，而且翻月会把选中的日子一起翻掉）。
//
// # 预设的值逐条抄上游
//
//	全部时间  不筛（上游传 null）
//	今天      今天 00:00:00 ~ 今天 23:59:59
//	昨天      昨天 00:00:00 ~ 昨天 23:59:59
//	近一周    7 天前 00:00:00 ~ 今天 23:59:59
//
// 「近一周」是**往前 7 天**（`moment().subtract(7, "days")`，含今天一共 8 天），
// 不是「这一周」也不是「最近 168 小时」——照抄，用户在 Biu 里习惯的那个含不含
// 今天就照着来。
//
// # 换筛选条件一律回到第 1 页
//
// 上游两个页面都是 `refreshList()`：清空、回顶、从第 1 页重来。这里返回
// 「变了」让页面去 `Reload`，那一步做的正是这件事。
package ui

import (
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// datePresets 是那四个预设，顺序就是 Tab 从左到右。
//
// ⚠️ 顺序不能改：用户在 Biu 里习惯「第 3 个是昨天」。
var datePresets = []struct {
	label string
	// span 算这个预设的区间。**nil 是不筛**（全部时间）。
	span func(now time.Time) (from, to time.Time)
}{
	{label: "全部时间", span: nil},
	{label: "今天", span: func(now time.Time) (time.Time, time.Time) {
		d := startOfDay(now)
		return d, endOfDay(d)
	}},
	{label: "昨天", span: func(now time.Time) (time.Time, time.Time) {
		d := startOfDay(now.AddDate(0, 0, -1))
		return d, endOfDay(d)
	}},
	{label: "近一周", span: func(now time.Time) (time.Time, time.Time) {
		return startOfDay(now.AddDate(0, 0, -7)), endOfDay(now)
	}},
}

// presetAll 是「全部时间」那一档的下标（上游的 `selectedKey === "all"`）。
const presetAll = 0

// dateFilter 是一个日期范围筛选条件。
//
// 零值不可用，请用 newDateFilter：两个端点里得先有**真的日期**——零值时刻
// 画出来是「1/1/1」（公元元年），那不是「没选」，那是坏了。
type dateFilter struct {
	// from / to 是区间的两端。
	//
	// ⚠️ **它们现在是纯状态，不是控件**：手动挑日期的那一块是自己画的
	// （`daterangepicker.go` 的双月选择器），而框架的 `ui.DateInput` 只能选
	// **一天**、画不出「两头实心、中间一段淡色」那个区间（见那个文件的
	// 文件头）。所以现在只有这一层改它们。
	from, to time.Time

	// tab 是当前选中的预设（0..3）。-1 = 用户手动挑了，预设不高亮。
	tab int
	// seenTab / seenFrom / seenTo 是上一帧画完之后的值。
	// 拿它们比对才知道「用户改了」——直接比当前值和上次算出来的值会把用户
	// 的每一次输入都当成没发生。
	seenTab          int
	seenFrom, seenTo time.Time

	// on 报现在有没有在按日期筛。选「全部时间」是 false。
	on bool

	// ── 双月选择器自己的那一摊（见 `daterangepicker.go`）──────────────────

	// open 是那块双月日历开着没有（`ui.PopoverBase` 的那个开关）。
	open bool
	// month 是**左边**那个月（哪一天都行，月份有效）。右边那个月恒定是它的
	// 下一个月。
	month time.Time
	// pickingEnd 是「起始已经挑好了，正等着挑结束」吗。
	pickingEnd bool
	// pendingFrom 是刚挑好的那个**起始**（还在等结束的那一段）。
	pendingFrom time.Time
}

// newDateFilter 建一个「不按日期筛」的筛选器。
//
// now 是用来把区间填成今天的那一下时钟——测试传假时钟，这一页上的
// 「今天」就永远是那一天，不会随测试跑的时间变。
func newDateFilter(now time.Time) *dateFilter {
	d := startOfDay(now)
	f := &dateFilter{from: d, to: endOfDay(d), tab: presetAll, seenTab: presetAll}
	f.seenFrom, f.seenTo = f.from, f.to
	f.month = startOfMonth(d)
	return f
}

// Range 是这一帧生效的区间。on 为 false 时两端都返回零值（不筛）。
func (f *dateFilter) Range() (from, to time.Time, on bool) {
	if !f.on {
		return time.Time{}, time.Time{}, false
	}
	return f.from, f.to, true
}

// setPreset 换到第 i 个预设。
func (f *dateFilter) setPreset(i int, now time.Time) {
	if i < 0 || i >= len(datePresets) {
		return
	}
	f.tab = i
	if datePresets[i].span == nil {
		f.on = false
		// 把区间收成今天。用户下一步多半是要手动挑一天——摆着今天，
		// 他知道自己是从哪儿开始改的；摆着公元元年，他以为界面坏了。
		d := startOfDay(now)
		f.from, f.to = d, endOfDay(d)
	} else {
		f.on = true
		f.from, f.to = datePresets[i].span(now)
	}
	// ⚠️ **半截挑着的区间作废**：换了预设就等于换了条件，而那个「起始已经
	// 挑好、还等着挑结束」的半截状态是上一件事的。
	f.pickingEnd = false
	// 记下改完的值：**必须记**，否则 view 末尾那句「区间变了没」会把这个
	// preset 自己的写入当成用户输入，预设的高亮当帧就被抹掉了。
	f.seenTab, f.seenFrom, f.seenTo = f.tab, f.from, f.to
}

// view 画这一行筛选器，报「这一帧筛选条件变了没」。
//
// 变了就返回 true，页面据此 `Reload`（清空、回顶、从第 1 页重来）。
//
// ⚠️ 只能由界面线程调——它改的就是这一帧画的那个状态。
func (f *dateFilter) view(c *ui.Context, now time.Time) bool {
	t := c.Theme()
	// picked 是这一帧用户点了哪一档（-1 = 没点）。**只记不改**：真正改条件的是
	// 画完之后那一句统一做——直接在按钮回调里改的话，下面那句「和上一帧比一比」
	// 就认不出这是用户换的（它自己刚把 seen* 更新过了），于是换条件永远不触发
	// Reload，而界面看上去只是那一句变了、列表没重拉。
	picked := -1

	ui.Row(c).Key("filter:dateRange").FillWidth().Center().Gap(6).Children(func() {
		// 四个预设。为什么是四个按钮而不是 `ui.Tabs`：上游用的是 Tab，而**用户
		// 手动在日历上挑过之后，上游把四个 Tab 全取消选中**
		//（`setSelectedKey("")`），表示「这不是任何一个预设」。`ui.Tabs` 表达不了
		// 「一个都不选中」——它把越界的下标夹回 0（`coreTabsBase`），于是
		// 「全部时间」永远亮着，旁边那一句却写着「2026-10-10 ~ 2026-10-16」，
		// 两句话互相打架。自己画这四个按钮就一个 if 的事（和设置页那三档
		// 主题同一个写法）。
		for i, p := range datePresets {
			b := ui.Button(c, p.label).FontSize(scale.Font(c, 12)).Radius(6)
			if i == f.tab {
				b = b.Background(t.SurfaceHover)
			}
			if b.Clicked() {
				picked = i
			}
		}

		// 手动挑区间：**双月日期选择器**（上游 `visibleMonths={2}`）。
		f.viewRangePicker(c)

		ui.Text(c, f.summary()).FontSize(scale.Font(c, 12)).LineHeight(smallCopyLeading).
			TextColor(t.TextMuted)
	})

	switch {
	case picked >= 0:
		// 点了某一档预设：把这一档算出来的区间写进去（setPreset 里也会把
		// seen* 更新掉，不然下一帧又会认成「用户又动了一次」）。
		f.setPreset(picked, now)
		return true
	case !f.from.Equal(f.seenFrom) || !f.to.Equal(f.seenTo):
		// 用户在日历上手动挑了日子：`ui.DateInput` 直接写的是上面那两个字段，
		// 框架不报「变了」（它只改那个指针）。所以比对上一帧画完之后的样子——
		// 变了就是用户动的。
		//
		// 这一句**必须在画完之后**：画的时候框架才会写这两个字段。
		f.on = true
		f.tab = -1 // 手动挑的日期不点亮任何预设（上游那一支 setSelectedKey("")）
		f.seenTab, f.seenFrom, f.seenTo = f.tab, f.from, f.to
		return true
	}
	return false
}

// summary 是这一帧该显示的那一句筛选状态。
//
// 为什么要有这一句：选了「全部时间」之后，两个输入框里仍然摆着日期（见
// setPreset），只看框里的字会以为「今天」正在筛。写出来的那一句才是**真的
// 正在筛什么**。
func (f *dateFilter) summary() string {
	if !f.on {
		return "不按日期筛"
	}
	return f.from.Format("2006-01-02") + " ~ " + f.to.Format("2006-01-02")
}

// startOfDay 是那一天 00:00:00（**本地时区**：用户挑的是他看日历上写的那些
// 日期，用 UTC 换算会让东八区的用户在晚上八点之后看到的是「昨天」）。
func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// endOfDay 是那一天 23:59:59。用 23:59:59 而不是「第二天 00:00:00」：接口收
// 的是闭区间，第二天零点会把「今天 23:59:59.5 听的那一条」漏掉——而那正是
// 用户刚听完、正要加进稍后再看的那一条。
func endOfDay(t time.Time) time.Time {
	return startOfDay(t).Add(24*time.Hour - time.Second)
}
