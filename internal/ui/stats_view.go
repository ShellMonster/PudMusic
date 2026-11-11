// 听歌统计页的画法（原型 `PAGES.stats`，DESIGN.md §0′.T7）。
//
// 页面从上到下：页头（Listening / 听歌统计 / 最近 7 天）、`stat-strip` 四个大数字、两栏
// （每天收听的柱状图 | 最常听的歌手）、「本周单曲循环」曲目行。原型只有最近 7 天，所以日 / 周 /
// 月 / 全部那排 Tab 和 Top 表格随旧设计删了（底层 `ListeningStats` 数据和 `statsPeriod*`
// 聚合仍在）。
//
// ⚠️ **原型里的「一天里的收听时段」热力图这一版没做**：应用只按天落盘（`StatsDayStat`），没有
// 按小时的数据，凭空画一张是编数；第四个大数字「最常收听的时段」换成「连续听歌 N 天」。
//
// 🔴 铁律二：本文件里的 view **不许**调 `SetTheme`。
package ui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/leading"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// statsDays 是页面看的那一段：最近 7 天（含今天）。
const statsDays = 7

// statsTopLimit 是最常听的歌手 / 单曲循环各取多少行（原型是 5）。
const statsTopLimit = 5

// 图表的几样尺寸（原型 `.chart-svg`：viewBox 640×220、柱宽 48、圆角 4、高 240）。
const (
	statsChartW   = 640
	statsChartH   = 220
	statsBarW     = 48
	statsBarR     = 4
	statsChartBox = 240
	statsAxisW    = 40
)

// statsLast7 是「最近 7 天」那一段（含今天）。
func statsLast7(now time.Time, data app.ListeningStats) statsRange {
	start := now.AddDate(0, 0, -(statsDays - 1))
	return statsRange{
		From: start.Format("2006-01-02"), To: now.Format("2006-01-02"),
		Label: "最近 7 天", Days: statsDayPoints(start, now, data),
	}
}

// viewStats 是这一页的入口。
func (s *Shell) viewStats(c *ui.Context, _ *ui.Route) {
	// 🔴 铁律二：页面 view 的头一件正事就是装主题。
	s.applyFrameTheme(c)

	st := &s.statsPage
	// ⚠️ **快照在进页面时拿一次**：不是每帧拿（O(n) 的拷贝乘帧数）。
	if !st.loaded {
		st.refresh(s.app)
		st.loaded = true
	}
	now := s.now()
	rng := statsLast7(now, st.data)

	s.pudPage(c, "page:"+statsPath, func(winW, listW float32) {
		// ⚠️ **整页滚起来，滚动位置记在页面状态里**（`statsPageState.scroll`）。
		scroll := ui.Scroll(c).Key("stats-scroll").Grow(1).TrackScroll(&st.scroll)
		scroll.Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Stretch).Padding(space.None, space.None, space.PageBottom, space.None).
				Children(func() {
					pud.PageHead(c, "Listening", "听歌统计", "最近 7 天 · 只统计在本机的收听", winW, nil)
					if !st.have {
						pud.Empty(c, pud.ArtPlain, "还没有听歌记录",
							"听几首歌，这里就会有你的听歌时长、最常听的歌手和单曲循环。", nil)
						return
					}
					s.viewStatStrip(c, st, rng, now, winW)
					s.viewStatsTwoCol(c, st, rng, winW, listW)
					s.viewStatsLoop(c, st, rng, winW, listW)
				})
		})
	})
}

// syncStats 是这一页的每帧副作用：现在没有要同步的东西（日 / 周 / 月 / 全部那排 Tab 删了），
// 快照在进页面时拿一次、切走再切回来时由 `navigate` 重拿。留着这个入口是因为满屏播放器开着时
// 路由按它分派每页的副作用（见 underlay.go）。
func (s *Shell) syncStats(c *ui.Context) {}

// statStripCell 是 `stat-strip` 的一格：大数字（可带小单位）和下面一句说明。
type statStripCell struct {
	// parts 是大数字：数字和单位交替（"12" "小时" "40" "分"）；奇数位是单位。
	parts []string
	cap   string
}

// statUniqueSongs 是这一段里听过多少首不同的歌。
func statUniqueSongs(data app.ListeningStats, from, to string) int {
	seen := map[string]bool{}
	for day, d := range data.Days {
		if day < from || day > to {
			continue
		}
		for key := range d.Tracks {
			seen[key] = true
		}
	}
	return len(seen)
}

// statNewArtists 是这一段里第一次听到的 UP 主有几位（歌的 `FirstHeard` 落在这一段里）。
func statNewArtists(data app.ListeningStats, from, to string) int {
	seen := map[int64]bool{}
	for _, t := range data.Tracks {
		if t.ArtistMid == 0 || t.FirstHeard < from || t.FirstHeard > to {
			continue
		}
		seen[t.ArtistMid] = true
	}
	return len(seen)
}

// statHM 把毫秒拆成「N 小时 M 分」的大数字（不足一小时只有分）。
func statHM(ms int64) []string {
	total := time.Duration(ms) * time.Millisecond
	h := int(total / time.Hour)
	m := int((total % time.Hour) / time.Minute)
	if h == 0 {
		return []string{strconv.Itoa(m), "分"}
	}
	return []string{strconv.Itoa(h), "小时", strconv.Itoa(m), "分"}
}

// statWeekDelta 是「比上周多 / 少 X」那半句（上周没有记录就没有这半句）。
func statWeekDelta(cur, prev int64) string {
	if prev <= 0 {
		return ""
	}
	d := cur - prev
	switch {
	case d > 0:
		return " · 比上周多 " + statsDurationText(d)
	case d < 0:
		return " · 比上周少 " + statsDurationText(-d)
	}
	return " · 和上周一样"
}

// viewStatStrip 是原型 `.stat-strip`：上下各一条 line，四格等宽，格内 24 / 24 / 24 / 0，第二格起
// 左边一条 line 加 24 的左内边距；窄内容区变 2×2。
func (s *Shell) viewStatStrip(c *ui.Context, st *statsPageState, rng statsRange, now time.Time, winW float32) { // winW 是窗口宽
	p := tok.Of(c.Theme())
	prevStart := now.AddDate(0, 0, -(2*statsDays - 1))
	prevEnd := now.AddDate(0, 0, -statsDays)
	cur := st.data.DayRangeMs(rng.From, rng.To)
	prev := st.data.DayRangeMs(prevStart.Format("2006-01-02"), prevEnd.Format("2006-01-02"))
	cells := []statStripCell{
		{statHM(cur), "收听总时长" + statWeekDelta(cur, prev)},
		{[]string{strconv.Itoa(statUniqueSongs(st.data, rng.From, rng.To)), "首"}, "播放过的歌曲"},
		{[]string{strconv.Itoa(statNewArtists(st.data, rng.From, rng.To)), "位"}, "新认识的歌手"},
		{[]string{strconv.Itoa(st.data.Streak(now)), "天"}, "连续听歌"},
	}
	// 大数字按窗口宽流动（`clamp(32, 3.6vw, 48)`）；格数按窗口宽：≤1024 两列、≤760 一列。
	size := scale.BigNumFluid(winW)
	perRow := 4
	switch {
	case winW <= 760:
		perRow = 1
	case winW <= 1024:
		perRow = 2
	}
	line := func(pp *ui.Painter, r ui.Rect, top, bottom bool) {
		if top {
			pp.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 1}, p.Line, 0)
		}
		if bottom {
			pp.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 1, W: r.W, H: 1}, p.Line, 0)
		}
	}
	ui.Column(c).Key("stats-strip").FillWidth().Margin(space.Px32, space.None, space.None, space.None).
		Children(func() {
			for start := 0; start < len(cells); start += perRow {
				end := min(start+perRow, len(cells))
				first := start == 0
				ui.Row(c).FillWidth().AlignItems(ui.Stretch).
					Draw(func(pp *ui.Painter, r ui.Rect) { line(pp, r, first, true) }).
					Children(func() {
						for i := start; i < end; i++ {
							cell := cells[i]
							leftEdge := i == start
							padL := float32(24)
							if leftEdge {
								padL = 0
							}
							ui.Column(c).Key("stats-cell:"+strconv.Itoa(i)).Grow(1).Basis(0).Gap(space.Px2).
								Padding(space.Px24, space.Px24, space.Px24, padL).
								Draw(func(pp *ui.Painter, r ui.Rect) {
									if !leftEdge {
										pp.Fill(ui.Rect{X: r.X, Y: r.Y, W: 1, H: r.H}, p.Line, 0)
									}
								}).Children(func() {
								ui.Row(c).AlignItems(ui.End).Children(func() {
									for k, part := range cell.parts {
										if k%2 == 0 {
											typo.Display(ui.Text(c, part), scale.Font(c, size)).SingleLine()
											continue
										}
										ui.Text(c, part).FontSize(scale.Font(c, size*0.45)).FontWeight(500).
											TextColor(p.Text2).Margin(space.None, space.None, space.None, space.Px4).SingleLine()
									}
								})
								ui.Text(c, cell.cap).FontSize(scale.Font(c, scale.Size13)).TextColor(p.Text2).
									LineHeight(leading.Body)
							})
						}
					})
			}
		})
}

// viewStatsTwoCol 是两栏：左 1.4fr 每天收听，右 1fr 最常听的歌手，缝 48，上 48；窄内容区叠成一栏。
func (s *Shell) viewStatsTwoCol(c *ui.Context, st *statsPageState, rng statsRange, winW, listW float32) {
	stack := winW <= 1180 // 原型 `@media (max-width: 1180px)`，按窗口宽
	artists := st.statsPeriodArtists(rng)
	if len(artists) > statsTopLimit {
		artists = artists[:statsTopLimit]
	}
	leftW, rightW := listW, listW
	if !stack {
		leftW = (listW - space.Px48) * 1.4 / 2.4
		rightW = listW - space.Px48 - leftW
	}
	body := func() {
		ui.Column(c).Key("stats-chart").Width(leftW).Shrink(0).Children(func() {
			pud.SecHead(c, "stats:chart-head", "每天收听", "单位：分钟", "")
			s.viewStatsBars(c, rng, leftW)
		})
		ui.Column(c).Key("stats-artists").Width(rightW).Shrink(0).Children(func() {
			pud.SecHead(c, "stats:artists-head", "最常听的歌手", "", "")
			s.viewStatsArtistBars(c, artists)
		})
	}
	if stack {
		ui.Column(c).FillWidth().Gap(space.Px48).Margin(space.Px48, space.None, space.None, space.None).Children(body)
		return
	}
	ui.Row(c).FillWidth().Gap(space.Px48).AlignItems(ui.Start).
		Margin(space.Px48, space.None, space.None, space.None).Children(body)
}

// statsWeekdayLabel 是柱子下面的字（原型：周四…周一、最后一根「今天」）。
func statsWeekdayLabel(d time.Time, today bool) string {
	if today {
		return "今天"
	}
	return [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[d.Weekday()]
}

// viewStatsBars 是「每天收听」柱状图（原型 `.chart-svg`）：640×220 的 viewBox 等比缩到容器宽、
// 高 240，水平居中；四条横向参考线（line）、7 根柱（今天主色、其余 line-strong，圆角 4）、柱上写
// 分钟数（text-2）、柱下写星期（text-3），都是 11 号等宽。
func (s *Shell) viewStatsBars(c *ui.Context, rng statsRange, w float32) {
	p := tok.Of(c.Theme())
	n := len(rng.Days)
	if n == 0 {
		return
	}
	mins := make([]float64, n)
	maxMin := 0.0
	for i, d := range rng.Days {
		mins[i] = float64(d.Ms) / float64(time.Minute/time.Millisecond)
		if mins[i] > maxMin {
			maxMin = mins[i]
		}
	}
	// 纵轴上限：原型是固定的 200（参考线 0 / 60 / 120 / 180）；数据超过 200 时按 20 取整往上放。
	top := float64(int(maxMin/20+0.999)) * 20
	if top < 200 {
		top = 200
	}
	scaleX := w / statsChartW
	if sy := float32(statsChartBox) / statsChartH; sy < scaleX {
		scaleX = sy
	}
	offX := (w - statsChartW*scaleX) / 2
	offY := (statsChartBox - statsChartH*scaleX) / 2 // 原型 SVG 默认 xMidYMid meet：垂直居中
	gap := (float32(statsChartW) - statsAxisW - statsBarW*float32(n)) / float32(n-1)
	fs := scale.Font(c, 11*scaleX)
	box := ui.Box(c).Key("stats-bars").Width(w).Height(statsChartBox).Label("最近 7 天每天收听分钟数")
	box = box.Draw(func(pp *ui.Painter, r ui.Rect) {
		for v := 0.0; v <= top; v += 60 {
			y := r.Y + offY + (statsChartH-24-float32(v/top)*(statsChartH-40))*scaleX
			pp.Fill(ui.Rect{X: r.X + offX + statsAxisW*scaleX, Y: y, W: (statsChartW - statsAxisW) * scaleX, H: 1}, p.Line, 0)
		}
		for i := range mins {
			h := float32(mins[i]/top) * (statsChartH - 40)
			x := statsAxisW + float32(i)*(statsBarW+gap)
			col := p.LineStrong
			if i == n-1 {
				col = p.Accent
			}
			pp.Fill(ui.Rect{X: r.X + offX + x*scaleX, Y: r.Y + offY + (statsChartH-24-h)*scaleX, W: statsBarW * scaleX, H: h * scaleX}, col, statsBarR*scaleX)
		}
	})
	box.Children(func() {
		label := func(txt string, cx, cy float32, col ui.Color, alignRight bool) {
			t := typo.Mono(ui.Text(c, txt)).FontSize(fs).TextColor(col).SingleLine().Absolute()
			if alignRight {
				// 纵轴刻度和参考线同一条基线：和下面的柱上数字一样要加垂直居中的 offY（原来漏了，
				// 图比 640×240 窄时 offY 可达 40 多，「0」就比基线高出一截）。
				t.Right(w - (offX + cx*scaleX)).Top(offY + (cy-6)*scaleX)
				return
			}
			t.Left(offX + cx*scaleX - 20).Width(40).TextAlign(ui.Center).Top(offY + (cy-6)*scaleX)
		}
		for v := 0.0; v <= top; v += 60 {
			y := statsChartH - 24 - float32(v/top)*(statsChartH-40)
			label(strconv.Itoa(int(v)), statsAxisW-8, y, p.Text3, true)
		}
		for i, d := range rng.Days {
			x := statsAxisW + float32(i)*(statsBarW+gap) + statsBarW/2
			t, _ := time.ParseInLocation("2006-01-02", d.Key, time.Local)
			label(statsWeekdayLabel(t, i == n-1), x, statsChartH-6, p.Text3, false)
			if mins[i] > 0 {
				h := float32(mins[i]/top) * (statsChartH - 40)
				label(strconv.Itoa(int(mins[i]+0.5)), x, statsChartH-32-h, p.Text2, false)
			}
		}
	})
}

// viewStatsArtistBars 是「最常听的歌手」（原型 `.bar-row`：24 | 120 | 1fr | 72，缝 12，上下 8；
// 名次 12 号等宽 text-3，名字单行省略，轨道高 8 圆角 4 surface-2，条是 text-2、第一名主色，右边
// 12 号等宽 text-3 的分钟数）。
func (s *Shell) viewStatsArtistBars(c *ui.Context, rows []statsArtistRow) {
	p := tok.Of(c.Theme())
	if len(rows) == 0 {
		ui.Text(c, "这一段时间还没有听过歌").FontSize(scale.Font(c, scale.Size14)).TextColor(p.Text2)
		return
	}
	maxMs := rows[0].Ms
	for i, r := range rows {
		frac := float32(0)
		if maxMs > 0 {
			frac = float32(r.Ms) / float32(maxMs)
		}
		fill := p.Text2
		if i == 0 {
			fill = p.Accent
		}
		ui.Row(c).Key("stats-artist:"+strconv.FormatInt(r.Mid, 10)).FillWidth().Gap(space.Px12).
			// ⚠️ **右边留一条滚动条的热区**：行尾那列「N 分钟」右对齐到内容右缘，
			// 而 overlay 滚动条就画在那一条竖带上（`scrollbarGutter`：thumb 10 +
			// 两侧各 3）——零内边距的话，指针一压上去分钟数就被盖住。
			Padding(space.Px8, scrollbarGutter, space.Px8, space.None).AlignItems(ui.Center).Children(func() {
			typo.Mono(ui.Text(c, strconv.Itoa(i+1))).FontSize(scale.Font(c, scale.Size12)).TextColor(p.Text3).
				Width(24).Shrink(0)
			ui.Text(c, r.Name).FontSize(scale.Font(c, scale.Size14)).TextColor(p.Text).Width(120).Shrink(0).
				SingleLine().Ellipsis("…")
			ui.Box(c).Grow(1).Basis(0).Height(8).Draw(func(pp *ui.Painter, rc ui.Rect) {
				pp.Fill(rc, p.Surface2, 4)
				pp.Fill(ui.Rect{X: rc.X, Y: rc.Y, W: rc.W * frac, H: rc.H}, fill, 4)
			})
			typo.Mono(ui.Text(c, statsMinutesText(float64(r.Ms))+" 分钟")).FontSize(scale.Font(c, scale.Size12)).
				TextColor(p.Text3).Width(72).Shrink(0).TextAlign(ui.End).SingleLine()
		})
	}
}

// statsMinutesText 是分钟数的字（毫秒 → 整分钟）。
func statsMinutesText(ms float64) string {
	return strconv.FormatInt(int64(ms/float64(time.Minute/time.Millisecond)+0.5), 10)
}

// statsTrackRow 是单曲循环那一行要画的东西。
type statsTrackRow struct {
	// Key 是这一首的聚合键（行身份用它）。
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

// statsArtistRow 是「最常听的歌手」那一行要画的东西。
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

// viewStatsLoop 是「本周单曲循环」：曲目表头 + 最多 5 行，「专辑」那一列写「本机播放 N 次」，
// 时长一列写这一首听了多久（B 站没给每首的总时长，这里用实际听的时长）。
func (s *Shell) viewStatsLoop(c *ui.Context, st *statsPageState, rng statsRange, winW, listW float32) {
	rows := st.statsPeriodRows(rng)
	if len(rows) > statsTopLimit {
		rows = rows[:statsTopLimit]
	}
	ui.Column(c).Key("stats-loop").FillWidth().Margin(space.Px48, space.None, space.None, space.None).Children(func() {
		pud.SecHead(c, "stats:loop-head", "本周单曲循环", "", "")
		if len(rows) == 0 {
			ui.Text(c, "这一段时间还没有听过歌").FontSize(scale.Font(c, scale.Size14)).TextColor(tok.Of(c.Theme()).Text2)
			return
		}
		pud.TrackHeadMode(c, listW, pud.AlbumModeFor(winW))
		for i, r := range rows {
			r := r
			acts := s.listActions()
			ev := pud.TrackRow(c, "stats-track:"+r.Key, pud.Track{
				Index: strconv.Itoa(i + 1), Title: r.Title, Owner: r.Subtitle, AlbumCol: pud.AlbumModeFor(winW),
				Album:    fmt.Sprintf("本机播放 %d 次", r.Plays),
				Duration: clockText(time.Duration(r.Ms) * time.Millisecond),
				// ⚠️ **正在放的那一行要像别的列表一样亮起来**（eq-bars + 歌名
				// 主色）：行底下没有 `app.Item`，先按 `ItemOfStatsKey` 把聚合键
				// 拆回一项再交给 `IsPlaying` 判（判据是 `Item.Identity`，和
				// 拆键同一份口径）。拆不出来的行不算在放——它连是哪一首都认
				// 不出。不标的话按了播放那一行什么变化都没有，像没按上。
				Playing: statsRowPlaying(acts, r.Key, st.data.Tracks[r.Key]),
				Cover: func(c *ui.Context, w, h, radius float32) {
					imgcache.ViewTitledRadius(c, s.images, "", r.Title, w, h, radius)
				},
			}, listW)
			if ev.Play {
				// ⚠️ **走和其他列表同一条路**（`AppActions.PlayNow` →
				// `EnqueueAndPlayNow`）：统计行底下没有 `app.Item`，按下去要
				// 先把聚合键拆回一项（`ItemOfStatsKey`）；拆不出来 /
				// 放不了都说一句，不按了没反应。
				item, err := app.ItemOfStatsKey(r.Key, st.data.Tracks[r.Key])
				switch {
				case err != nil:
					acts.Notify("放不了这一首："+err.Error(), notice.Danger)
				case acts.PlayNow != nil:
					acts.PlayNow(item)
				}
			}
		}
	})
}

// statsRowPlaying 报「本周单曲循环」这一行是不是**正在放的那首**。
//
// 行底下没有 `app.Item`（它画的是统计快照），所以按 `ItemOfStatsKey` 把聚合
// 键拆回一项再交给 `acts.IsPlaying` 判——和按下播放键时拆键是同一条路，
// 判据自然是同一份口径（`Item.Identity`）。
//
// ⚠️ **拆不出来的行当「没在放」**：那种行连身份都认不出，亮起它等于说
// 「正在放的是它」——那是另一种撒谎。
func statsRowPlaying(acts list.Actions, key string, snap *app.StatsTrackStat) bool {
	if acts.IsPlaying == nil {
		return false
	}
	it, err := app.ItemOfStatsKey(key, snap)
	if err != nil {
		return false
	}
	return acts.IsPlaying(it)
}
