// 听歌统计页的画法。
//
// # 三件事的分工
//
//	这一页**进页面时**拿一次快照（`statsPageState.refresh`），然后自己留着。
//	这一页**每一帧**只画它留着的那一份。
//	Top 歌 / Top 艺人**画在 `ui.Table` 里**——那是**虚拟列表**（框架的
//	`buildList` 只建看得见的那几行），所以「全部」那一档的几千行也不卡。
//
// # 「全部」那一档为什么不画柱状图
//
// ⚠️ **那一档的天数可能上千**——而柱状图要一根一根地画。上千根柱子既挤在
// 一起读不出来，又把每一帧的构建量拉上去。
//
// ⇒ 那一档画的是 Top 歌（虚拟表格，几千行也不卡）。
//
// 🔴 铁律二：本文件里的 view **不许**调 `SetTheme`。

package ui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"
)

// 柱状图那几样的尺寸。
const (
	// statsChartHeight 是柱状图画多高（像素）。
	statsChartHeight = 160
	// statsBandInner / statsBandOuter 是**一根柱子那一格**的内边距和外边距
	//（占那一格步长的比例，不是像素）。
	//
	// ⚠️ **它们不在 `space` 那张表里**：那张表管的是像素间距，而这两个是
	// **比例**——`chart.Band.Padding(inner, outer)` 要的就是比例。
	statsBandInner = 0.3
	statsBandOuter = 0.15
)

// statsTopLimit 是 Top 歌 / Top 艺人各取多少首。
//
// ⚠️ **「全部」那一档也只取这么多**：Top 榜本来就是「最前面的那些」，而把
// 几千行都摆出来没有意义。
const statsTopLimit = 100

// statsColumns 是 Top 歌 / Top 艺人那两张表的列。
//
// ⚠️ **宽度照本地音乐那一档的表格**（见 localmusic.go 的 `localColumns`）：
// 名次那一格窄、标题吃掉剩下的、时长那一格定死（不然上下两行的时长会左右
// 跳）。
var statsColumns = []ui.TableColumn{
	{Title: "#", ID: "rank", Width: 44, Align: ui.Center},
	{Title: "名称", ID: "name", MinWidth: 160},
	{Title: "时长", ID: "time", Width: 110, Align: ui.End},
	{Title: "次数", ID: "plays", Width: 84, Align: ui.End},
}

// viewStats 是这一页的入口。
func (s *Shell) viewStats(c *ui.Context, _ *ui.Route) {
	// 🔴 铁律二：页面 view 的头一件正事就是装主题。
	s.applyFrameTheme(c)

	st := &s.statsPage
	// ⚠️ **快照在进页面时拿一次**：不是每帧拿。每一帧拿一份要把 O(n) 的
	// 拷贝乘上帧数——「全部」那一档尤其明显。
	if !st.loaded {
		st.refresh(s.app)
		st.loaded = true
	}

	now := s.now()
	rng := statsRangeOf(st.tab, now, st.data)

	// ⚠️ **整页滚起来**：这一页的内容比一屏高（四枚磁贴 + 柱状图 + 两张表），
	// 而不滚的话底下那一张表**永远看不到**——用户以为这个应用没有 Top 艺人。
	//
	// ⚠️ **滚动位置记在页面状态里**（`statsPageState.scroll`）：新建一个等于
	// 每帧回到顶上，而那一帧是要重建整棵树的。
	scroll := ui.Scroll(c).Key("stats-scroll").Grow(1).TrackScroll(&st.scroll)
	scroll.Children(func() {
		ui.Column(c).FillWidth().Padding(space.None, pagePadX).Gap(space.Px16).
			Children(func() {
				ui.Text(c, "LISTENING").FontSize(scale.Font(c, scale.Size12)).
					TextColor(c.Theme().TextMuted)
				// 页头：标题。
				ui.Text(c, "听歌统计").FontSize(scale.Font(c, scale.PageTitle))

				// 四档。
				mainTabs(c, &st.tabIndex, tabMD, statsTabNames...).AlignSelf(ui.Start)

				if !st.have {
					// 空状态：这一页**自己**画（不是路由表里那一句）。
					//
					// ⚠️ **理由和 `follow.go` 那一处一样**：这一页是内容页，而空状态
					// 说的是「为什么空、怎么才能不空」——那一句得跟这一页的上下文
					// 在一起。
					s.viewStatsEmpty(c)
					return
				}

				// 四枚磁贴。
				s.viewStatsTiles(c, st, rng, now)

				// 柱状图。
				//
				// ⚠️ **它看的是「最近 14 天」，不跟着那一档走**（理由见
				// `statsChartDays`）：柱状图是看**趋势**的，而「日」那一档只有一天——
				// 画一根柱子不叫图。
				s.viewStatsChart(c, statsChartRange(now, st.data))

				// Top 歌。
				s.viewStatsTopTracks(c, st, rng)

				// Top 艺人。
				s.viewStatsTopArtists(c, st, rng)
			})
	})
	s.trackScrollbar(scroll)
}

// syncStats 是这一页的每帧副作用。
//
// ⚠️ **它只有一件事：需要的时候重新拿一次快照**。
//
// ⚠️ **为什么不是「每帧都拿」**：那一拿是 O(n) 的拷贝（地图 + 歌 + 艺人），
// 而这一页停留的时候可能有好几分钟——每帧拷一遍是白花的。
//
// 🔴 **它不在 `viewStats` 里**：满屏播放器开着的时候那一页压根不画，而它的
// 每帧副作用**照跑**（见 underlay.go 顶上那一段）——所以用户从满屏退回来，
// 看到的还是最新的数。
func (s *Shell) syncStats(c *ui.Context) {
	st := &s.statsPage
	// ⚠️ **换档就重新拿**：四档的数字必须对得上档。
	if st.tabIndex != int(st.tab) {
		st.setTab(statsVisibleTab(st.tabIndex))
		st.refresh(s.app)
		st.loaded = true
		return
	}
	// 🔴 **「切走再切回来」那一次的重新拿在 `navigate` 里**（见 router.go 的
	// `navigate` 那一段）——因为这一页的副作用是**按当前路由分派**的，它自己
	// 看不到「用户走了又回来」。
	//
	// ⚠️ **这里只管换档**：换档要重新算一遍（那一档是另一段区间）。
	if st.tabIndex != int(st.tab) {
		st.setTab(statsVisibleTab(st.tabIndex))
		st.refresh(s.app)
		st.loaded = true
	}
}

// viewStatsEmpty 是「还没有听歌记录」那一块。
func (s *Shell) viewStatsEmpty(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(space.Px48, space.None).AlignItems(ui.Center).Gap(space.Px8).
		Children(func() {
			ui.Text(c, "还没有听歌记录").FontSize(scale.Font(c, scale.Size18))
			ui.Text(c, "听几首歌，这里就会有你的听歌时长、最爱听的歌和最爱听的 UP 主").
				FontSize(scale.Font(c, scale.Size14)).TextColor(c.Theme().TextMuted)
		})
}

// statsTile 是那四枚磁贴的一枚。
type statsTile struct {
	// Label 是上面那一句小字（「今日收听」）。
	Label string
	// Value 是下面那个大数字。
	Value string
	// Unit 是大数字右边的那个单位（「首」「天」）。
	//
	// ⚠️ **它挂在数字右边，不另起一行**：另起一行的话，四枚磁贴的高度
	// 就差了（有的两行、有的三行），而那一排的下沿会参差不齐。
	Unit string
	// Hint 是大数字底下那一行小字（「1 小时 20 分钟」）。
	Hint string
}

// viewStatsTiles 是那四枚统计磁贴。
func (s *Shell) viewStatsTiles(c *ui.Context, st *statsPageState, rng statsRange, now time.Time) {
	t := c.Theme()
	ms := st.data.DayRangeMs(rng.From, rng.To)
	plays := st.data.DayRangePlays(rng.From, rng.To)
	completed := st.data.DayRangeCompleted(rng.From, rng.To)
	streak := st.data.Streak(now)

	tiles := []statsTile{
		// ⚠️ **单位是「小时」**：那个大数字是小时（"0.5"），而底下那一行是
		// 它的另一种说法（"30 分钟"）——**单位不挂在数字旁边的话，用户得
		// 自己猜那个 0.5 是多少**。
		{Label: rng.Label + "收听", Value: statsHoursText(ms), Unit: "小时", Hint: statsDurationText(ms)},
		{Label: rng.Label + "起播", Value: fmt.Sprintf("%d", plays), Unit: "首", Hint: "从第一首到最后一首"},
		{Label: rng.Label + "听完", Value: fmt.Sprintf("%d", completed), Unit: "首", Hint: "听到一半以上"},
		{Label: "连续", Value: fmt.Sprintf("%d", streak), Unit: "天", Hint: "一天没断"},
	}

	// ⚠️ **四枚磁贴一样高**：每一枚都有三行（标签 / 大数字 / 说明），而
	// 少了说明的那几枚会矮一截——那一排的下沿就参差不齐了。
	ui.Row(c).Key("stats-tiles").FillWidth().Gap(space.Px12).AlignItems(ui.Stretch).Children(func() {
		for _, tile := range tiles {
			ui.Column(c).Key("stats-tile:"+tile.Label).Grow(1).Gap(space.Px2).
				Padding(space.Px12, space.Px16).Radius(s.cardRadius(t)).
				Background(s.cardBackground(t)).Border(1, t.Border).Children(func() {
				ui.Text(c, tile.Label).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
				// ⚠️ **数字和单位在一行**：单位挂在数字右边（不是另起一行），
				// 而下面那一行留给说明。
				// ⚠️ **`ui.End`（不是基线对齐）**：框架没有基线对齐这一档
				// （`ui.Align` 只有 Start / Center / End / Stretch），
				// 而这一行里那个大数字（24 号）和单位（14 号）——
				// 底边齐比基线齐差得少，而差的那一点用户看不出来。
				ui.Row(c).Gap(space.Px4).AlignItems(ui.End).Children(func() {
					ui.Text(c, tile.Value).FontSize(scale.Font(c, scale.Size24)).FontWeight(weightMedium)
					ui.Text(c, tile.Unit).FontSize(scale.Font(c, scale.Size14)).TextColor(t.TextMuted)
				})
				ui.Text(c, tile.Hint).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
			})
		}
	})
}

// viewStatsChart 是那张柱状图。
//
// ⚠️ **它用 `mygo/ui/chart`**——那是框架自带的原生图表（**全仓第一次用**）。
// 零新增依赖。
func (s *Shell) viewStatsChart(c *ui.Context, rng statsRange) {
	t := c.Theme()
	labels := make([]string, len(rng.Days))
	values := make([]float64, len(rng.Days))
	maxMs := int64(0)
	for i, d := range rng.Days {
		labels[i] = d.Label
		values[i] = float64(d.Ms)
		if d.Ms > maxMs {
			maxMs = d.Ms
		}
	}
	if maxMs <= 0 {
		return
	}

	ui.Column(c).Key("stats-chart").FillWidth().Gap(space.Px8).Children(func() {
		// ⚠️ **标题里带单位**（"分钟"）：左边那一栏写的是分钟，而那一栏
		// 只有数字没有单位——用户得从标题才知道那是多少分钟。
		ui.Text(c, "每天的听歌时长（最近 14 天，单位：分钟）").FontSize(scale.Font(c, scale.Size16))
		ui.Column(c).Key("stats-chart-card").FillWidth().
			Padding(space.Px12, space.Px16).Radius(s.cardRadius(t)).
			Background(s.cardBackground(t)).Border(1, t.Border).Children(func() {
			// ⚠️ **这两个数不是布局间距**：它们是 `chart.Band.Padding(inner,
			// outer)`——**一根柱子那一格的内边距和外边距**（占那一格步长的比例）。
			//
			// ⇒ 所以它们**不在 `space` 那张表里**（那张表管的是像素，而这两个
			// 是比例）。而 `widgets/space` 那道护栏扫的是「`.Padding(` 的实参
			// 是裸数字」——所以这里用具名常量，让护栏看得见这两个数的名字
			// （而不是两个光秃秃的数字）。
			x := chart.NewBand(labels...).Padding(statsBandInner, statsBandOuter)
			y := chart.NewLinear(0, float64(maxMs)).Nice(4)
			chart.Plot(c, chart.Axes{
				// ⚠️ **左边那一栏写的是「分钟」，不是毫秒**：毫秒是六位数
				// （"5000000"），那一栏窄，而那串数字用户读不出来它是多长。
				// 分钟是「30」「60」——一眼看得懂。
				Left:   y.Ticks(4),
				Bottom: x.Ticks(),
				Label: func(side chart.Side, tick chart.Tick) {
					if side == chart.Left {
						ui.Text(c, statsMinutesText(tick.Value)).NoWrap()
						return
					}
					ui.Text(c, tick.Label).NoWrap()
				},
			}, func(area ui.Element) {
				for i, v := range values {
					if v <= 0 {
						continue
					}
					chart.Rect(c, x.Band(i), y.Span(0, v)).Background(t.Accent).Radius(3)
				}
			}).Height(statsChartHeight).Gap(space.Px8)
		})
	})
}

// statsMinutesText 是柱状图左边那一栏的刻度字（分钟）。
//
// ⚠️ **不足一分钟写「0」**：那一栏是刻度，而 0 就是「没有」——而「0 分钟」
// 三个字在那一栏里太宽了。
func statsMinutesText(ms float64) string {
	minutes := int64(ms / float64(time.Minute/time.Millisecond))
	return strconv.FormatInt(minutes, 10)
}

// statsTrackRow 是 Top 歌那一行要画的东西。
type statsTrackRow struct {
	// Key 是这一首的聚合键（行身份用它，见下面那条注释）。
	Key string
	// Title 是歌名。
	Title string
	// Subtitle 是歌名底下那一行（UP 名 / 「本地文件」）。
	Subtitle string
	// Ms 是听了多久。
	Ms int64
	// Plays 是起播了几次。
	Plays int64
}

// statsArtistRow 是 Top 艺人那一行要画的东西。
type statsArtistRow struct {
	// Mid 是 UP 主的编号（行身份用它）。
	Mid int64
	// Name 是 UP 名。
	Name string
	// Ms 是听了多久。
	Ms int64
	// Plays 是起播了几次。
	Plays int64
}

// viewStatsTopTracks 是 Top 歌那一块。
func (s *Shell) viewStatsTopTracks(c *ui.Context, st *statsPageState, rng statsRange) {
	t := c.Theme()
	rows := st.statsPeriodRows(rng)

	ui.Column(c).Key("stats-top-tracks").FillWidth().Gap(space.Px8).Children(func() {
		ui.Text(c, "最爱听的歌").FontSize(scale.Font(c, scale.Size16))
		if len(rows) == 0 {
			ui.Text(c, "这一段时间还没有听过歌").FontSize(scale.Font(c, scale.Size14)).
				TextColor(t.TextMuted)
			return
		}
		s.viewStatsTrackTable(c, st, rows)
	})
}

// viewStatsTrackTable 是 Top 歌那张表。
//
// ⚠️ **`ui.Table` 是虚拟列表**：框架的 `buildList` 只建看得见的那几行，所以
// 「全部」那一档的几千行也不卡。
func (s *Shell) viewStatsTrackTable(c *ui.Context, st *statsPageState, rows []statsTrackRow) {
	t := c.Theme()
	// 🔴 **行身份是那一首的聚合键**，不是序号（铁律四）。
	st.trackTable.Key = func(i int) any {
		if i >= 0 && i < len(rows) {
			return "stats-track:" + rows[i].Key
		}
		return "stats-track-out"
	}
	st.trackTable.Label = func(i int) string {
		if i >= 0 && i < len(rows) {
			return rows[i].Title
		}
		return ""
	}
	st.trackTable.Selected = &st.trackSel

	// 🔴 **登记焦点**（`focus.go` 的 `registerContentFocus`）：这一页**自己
	// 画了列表**（`ui.Table`），而侧栏按 ↓ 要能进内容区。
	//
	// ⚠️ **Top 歌先登记、Top 艺人后登记**——后登记的那个赢（`focus.go` 顶上
	// 那一段），而 Top 艺人是这一页最后画的那一个。
	s.registerContentFocus(list.HandleFocus(&st.trackTable.Handle))

	// ⚠️ **表在卡片里**：这一页上面是四张卡片（磁贴）和一张卡片（柱状图），
	// 而表格如果光着摆，这一页就有**两种语言**（圆角的卡片 + 无框的表格）。
	ui.Column(c).FillWidth().Padding(space.None, space.Px8, space.Px8, space.None).
		Radius(s.cardRadius(t)).Background(s.cardBackground(t)).Border(1, t.Border).
		Children(func() {
			ui.Table(c, &st.trackTable, statsColumns, len(rows), func(row, col int) {
				if row < 0 || row >= len(rows) {
					return
				}
				r := rows[row]
				switch statsColumns[col].ID {
				case "rank":
					ui.Text(c, itoa(row+1)).TextColor(t.TextMuted)
				case "name":
					ui.Column(c).FillWidth().Children(func() {
						ui.Text(c, r.Title).SingleLine()
						if r.Subtitle != "" {
							ui.Text(c, r.Subtitle).FontSize(scale.Font(c, scale.Size12)).
								TextColor(t.TextMuted).SingleLine()
						}
					})
				case "time":
					ui.Text(c, statsTableDurationText(r.Ms))
				case "plays":
					ui.Text(c, itoa(int(r.Plays)))
				}
			})
		})
}

// viewStatsTopArtists 是 Top 艺人那一块。
func (s *Shell) viewStatsTopArtists(c *ui.Context, st *statsPageState, rng statsRange) {
	t := c.Theme()
	rows := st.statsPeriodArtists(rng)

	ui.Column(c).Key("stats-top-artists").FillWidth().Gap(space.Px8).Children(func() {
		ui.Text(c, "最爱听的 UP 主").FontSize(scale.Font(c, scale.Size16))
		if len(rows) == 0 {
			ui.Text(c, "这一段时间还没有听过 B 站的歌（本地音乐不计入这里）").
				FontSize(scale.Font(c, scale.Size14)).TextColor(t.TextMuted)
			return
		}
		s.viewStatsArtistTable(c, st, rows)
	})
}

// viewStatsArtistTable 是 Top 艺人那张表。
func (s *Shell) viewStatsArtistTable(c *ui.Context, st *statsPageState, rows []statsArtistRow) {
	t := c.Theme()
	st.artistTable.Key = func(i int) any {
		if i >= 0 && i < len(rows) {
			return "stats-artist:" + strconv.FormatInt(rows[i].Mid, 10)
		}
		return "stats-artist-out"
	}
	st.artistTable.Label = func(i int) string {
		if i >= 0 && i < len(rows) {
			return rows[i].Name
		}
		return ""
	}
	st.artistTable.Selected = &st.artistSel

	// 🔴 **后登记的赢**：所以这一枚是这一页的焦点目标（理由见上面那一处）。
	s.registerContentFocus(list.HandleFocus(&st.artistTable.Handle))

	// ⚠️ **表在卡片里**：理由同上面那一张。
	ui.Column(c).FillWidth().Padding(space.None, space.Px8, space.Px8, space.None).
		Radius(s.cardRadius(t)).Background(s.cardBackground(t)).Border(1, t.Border).
		Children(func() {
			ui.Table(c, &st.artistTable, statsColumns, len(rows), func(row, col int) {
				if row < 0 || row >= len(rows) {
					return
				}
				r := rows[row]
				switch statsColumns[col].ID {
				case "rank":
					ui.Text(c, itoa(row+1)).TextColor(t.TextMuted)
				case "name":
					ui.Text(c, r.Name).SingleLine()
				case "time":
					ui.Text(c, statsTableDurationText(r.Ms))
				case "plays":
					ui.Text(c, itoa(int(r.Plays)))
				}
			})
		})
}
