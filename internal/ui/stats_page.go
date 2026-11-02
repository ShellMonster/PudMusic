// 听歌统计页（`/stats`）。
//
// # 这一页摆什么
//
// 四档（日 / 周 / 月 / 全部），每档摆四枚统计磁贴、一张柱状图、Top 歌、
// Top 艺人。
//
// # 「全部」那一档为什么不会卡
//
// ⚠️ **数据全在内存里**：进页面时拿一次快照（见 `App.ListeningStats`），
// 那一页自己留着。**不是**每帧去扫一遍盘上的文件，也不是每帧重新聚合。
//
// ⚠️ **Top 列表用虚拟列表**（`widgets/list`）：几千行也只建看得见的那几行。
// 而「全部」那一档的 Top 歌可能很多——所以这一档**画在虚拟列表里**，不是
// 一堆自己排的文本。
//
// 🔴 铁律二：本文件里的 view **不许**调 `SetTheme`。

package ui

import (
	"fmt"
	"sort"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo/ui"
)

// statsPath 是听歌统计那条路由，和 routes 表里登记的是同一个字符串。
//
// 拿来和 `Route.Match` 比，而不是拿 `routes[N]`：那条表一改顺序，这里跟着
// 错，而且错得很安静——这一页退回占位页，没人看得出来（和 `localMusicPath`
// 同一个理由）。
const statsPath = "/stats"

// statsTab 是四档的那四档。
type statsTab int

const (
	// statsTabDay 是「日」：今天。
	statsTabDay statsTab = iota
	// statsTabWeek 是「周」：本周（从周一开始）。
	statsTabWeek
	// statsTabMonth 是「月」：本月。
	statsTabMonth
	// statsTabAll 是「全部」：所有时间。
	statsTabAll
)

// statsTabNames 是四档上面的字。
var statsTabNames = []string{"日", "周", "月", "全部"}

// statsPageState 是这一页自己的状态。
//
// ⚠️ **快照在这里，不在 App 上**：这一页在**进页面时**拿一次（见
// `refresh`），然后自己留着。每一帧去 `App.ListeningStats()` 拿一份会把
// 「进统计页」这一下的开销变成 O(n) × 每帧。
type statsPageState struct {
	// tab 是现在看哪一档。
	tab statsTab
	// tabIndex 是给 `mainTabs` 用的那个下标。
	//
	// ⚠️ **它是 `tab` 的投影，不是反过来**：`mainTabs` 要的是 `*int`，而
	// 「这一档是什么」在别处是 `statsTab`。两个都存着，改一处要改另一处
	// ——所以 `tab` 是真的那个，`tabIndex` 只在换档时同步。
	tabIndex int
	// loaded = 快照拿过了。
	//
	// ⚠️ **它不是「有没有数据」**：第一次运行（还没听过歌）时 `data` 是
	// 空的，而那是**空状态**，不是「加载中」。
	loaded bool
	// data 是最近一次拿到的快照。
	data app.ListeningStats
	// have = 有没有听歌记录。
	have bool
	// trackTable / artistTable 是那两张 Top 表的虚拟列表状态。
	trackTable  ui.ListState
	artistTable ui.ListState
	// trackSel / artistSel 是那两张表的选中行（-1 是没选）。
	trackSel  int
	artistSel int
	// scroll 是那一页的滚动位置。
	scroll ui.ScrollState
}

// newStatsPageState 开一份状态。
//
// ⚠️ **两个选中行都是 -1**（没选）：`ui.ListState.Selected` 那个 `*int` 指着
// 的值是「选中的行号」，而**零值 0 会被当成「选中了第一行」**——那会让第一行
// 铺一整块主色底，而那个主色上的字（`AccentText`）和那一行自己画的字
// （`TextMuted`）对不上，于是第一行的序号和副题**看不见**。
//
// ⇒ 所以这里显式给 -1（和 `localPage` 的 `sel: -1` 一个道理）。
func newStatsPageState() *statsPageState {
	st := &statsPageState{trackSel: -1, artistSel: -1}
	return st
}

// refresh 重新拿一次快照。
//
// ⚠️ **进页面时调一次**，不是每帧调。
func (st *statsPageState) refresh(a *app.App) {
	data, ok := a.ListeningStats()
	st.data = data
	st.have = ok && len(data.Days) > 0
}

// setTab 换档。
func (st *statsPageState) setTab(t statsTab) {
	st.tab = t
	st.tabIndex = int(t)
}

// statsPeriodRows 是「这一档里」的那些歌（按时长倒序）。
//
// 🔴 **它跟那一档走**：磁贴看的是那一段，而下面那张 Top 表看的也得是那一段——
// 否则用户看到的���是「今天听了 30 分钟」和「最爱听的那首听了 2 小时」摆在
// 一起，而那两个数**根本不是一段时间**。
//
// ⚠️ **「日」那一档按「今天」算**，而今天可能只听了两三首——那一张表就
// 只有两三行。这是对的：用户问的是「今天听了什么」。
//
// ⚠️ **「全部」那一档按「所有时间」算**（那是全局那份聚合）。
func (st *statsPageState) statsPeriodRows(rng statsRange) []statsTrackRow {
	if rng.From == "" || rng.To == "" {
		return nil
	}
	// 区间内每一首的时长和次数（从按天存的那份里加起来）。
	type acc struct {
		ms    int64
		plays int64
	}
	byKey := map[string]*acc{}
	for day, d := range st.data.Days {
		if day < rng.From || day > rng.To {
			continue
		}
		for key, t := range d.Tracks {
			a, ok := byKey[key]
			if !ok {
				a = &acc{}
				byKey[key] = a
			}
			a.ms += t.Ms
			a.plays += t.Plays
		}
	}
	// ⚠️ **按时长倒序**，和全局那份一个口径（见 `TopTracks`）。
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		if byKey[keys[i]].ms != byKey[keys[j]].ms {
			return byKey[keys[i]].ms > byKey[keys[j]].ms
		}
		return keys[i] < keys[j]
	})
	out := make([]statsTrackRow, 0, len(keys))
	for _, key := range keys {
		if len(out) >= statsTopLimit {
			break
		}
		t := st.data.Tracks[key]
		title, subtitle := "这一首", ""
		if t != nil {
			title = t.Title
			subtitle = t.Artist
			if t.Local {
				subtitle = "本地文件"
			}
			if title == "" {
				title = "这一首"
			}
		}
		out = append(out, statsTrackRow{
			Key:      key,
			Title:    title,
			Subtitle: subtitle,
			Ms:       byKey[key].ms,
			Plays:    byKey[key].plays,
		})
	}
	return out
}

// statsPeriodArtists 是「这一档里」的那些 UP 主。
func (st *statsPageState) statsPeriodArtists(rng statsRange) []statsArtistRow {
	if rng.From == "" || rng.To == "" {
		return nil
	}
	type acc struct {
		ms    int64
		plays int64
	}
	byMid := map[int64]*acc{}
	for day, d := range st.data.Days {
		if day < rng.From || day > rng.To {
			continue
		}
		for _, t := range d.Tracks {
			if t.ArtistMid == 0 {
				continue
			}
			a, ok := byMid[t.ArtistMid]
			if !ok {
				a = &acc{}
				byMid[t.ArtistMid] = a
			}
			a.ms += t.Ms
			a.plays += t.Plays
		}
	}
	mids := make([]int64, 0, len(byMid))
	for mid := range byMid {
		mids = append(mids, mid)
	}
	sort.SliceStable(mids, func(i, j int) bool {
		if byMid[mids[i]].ms != byMid[mids[j]].ms {
			return byMid[mids[i]].ms > byMid[mids[j]].ms
		}
		return mids[i] < mids[j]
	})
	out := make([]statsArtistRow, 0, len(mids))
	for _, mid := range mids {
		if len(out) >= statsTopLimit {
			break
		}
		name := ""
		if a, ok := st.data.Artists[mid]; ok {
			name = a.Name
		}
		out = append(out, statsArtistRow{
			Mid:   mid,
			Name:  name,
			Ms:    byMid[mid].ms,
			Plays: byMid[mid].plays,
		})
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 区间
// ─────────────────────────────────────────────────────────────────────────────

// statsRange 是「这一档看的是哪一段」。
type statsRange struct {
	// From / To 是两端那两天的键（"2026-11-01" 那种）。
	From string
	To   string
	// Label 是这一档的名字（「今天」「本周」「本月」「全部」）。
	Label string
	// Days 是这一段里的那些天（按时间倒序，最新的在前）。
	//
	// ⚠️ **它是算出来的，不是从 map 里乱序取的**——map 的遍历顺序是随机
	// 的，而柱状图要按时间排。
	Days []statsDayPoint
}

// statsDayPoint 是柱状图上的一个点。
type statsDayPoint struct {
	// Key 是那一天的键。
	Key string
	// Label 是那个点上要写的字（"11-01" 那种）。
	Label string
	// Ms 是那一天累计的净时长（毫秒）。
	Ms int64
}

// statsRangeOf 算出某一档对应的那一段。
//
// ⚠️ **周从周一开始**（不是从「七天前」开始）——那是日历上的一周，
// 而「最近七天」是另一个意思。
func statsRangeOf(tab statsTab, now time.Time, data app.ListeningStats) statsRange {
	switch tab {
	case statsTabDay:
		d := now.Format("2006-01-02")
		return statsRange{From: d, To: d, Label: "今天",
			Days: []statsDayPoint{{Key: d, Label: statsDayLabel(now), Ms: data.DayMs(d)}}}
	case statsTabWeek:
		start := statsWeekStart(now)
		end := start.AddDate(0, 0, 6)
		return statsRange{
			From: start.Format("2006-01-02"), To: end.Format("2006-01-02"),
			Label: "本周", Days: statsDayPoints(start, end, data),
		}
	case statsTabMonth:
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		end := start.AddDate(0, 1, -1)
		return statsRange{
			From: start.Format("2006-01-02"), To: end.Format("2006-01-02"),
			Label: "本月", Days: statsDayPoints(start, end, data),
		}
	default:
		// 「全部」：从最早的那一天到今天。
		first := data.FirstDay()
		today := now.Format("2006-01-02")
		if first == "" {
			first = today
		}
		return statsRange{From: first, To: today, Label: "全部"}
	}
}

// statsChartDays 是柱状图画的那一段天。
//
// 🔴 **它永远是「最近 14 天」**，不跟着那一档走。
//
// ⚠️ **为什么**：柱状图是「**每天的**听歌时长」——它的用处是看**趋势**。
// 而「日」那一档只有一天，画一根柱子不叫图（而那一页在「日」那一档是最常
// 打开的——用户第一眼看的偏偏是它）。而「全部」那一档可能有上千天，上千根
// 柱子既挤在一起读不出来，又把每一帧的构建量拉上去。
//
// ⇒ 所以它是**一个固定的尾随窗口**：到今天为止的 14 天。
// 而那四枚磁贴跟着一档走——**图和磁贴看的是两件事**（一个是趋势，一个是
// 那一段的总数），而这一页把它们摆在一起时，标题各说各的。
const statsChartDays = 14

// statsChartRange 是柱状图的那一段：从 13 天前到今天。
func statsChartRange(now time.Time, data app.ListeningStats) statsRange {
	end := now
	start := end.AddDate(0, 0, -(statsChartDays - 1))
	return statsRange{
		From: start.Format("2006-01-02"), To: end.Format("2006-01-02"),
		Label: "最近 14 天", Days: statsDayPoints(start, end, data),
	}
}

// statsWeekStart 是「本周的第一天」（周一的零点）。
func statsWeekStart(now time.Time) time.Time {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	// ⚠️ **Go 的星期天是 0**：周一到周六是 1 到 6，周日是 0。
	// 所以「往回数几天」是 `(weekday + 6) % 7`——周日那天算出 6，
	// 也就是往回数 6 天到周一。
	return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
}

// statsDayPoints 把「从 start 到 end」这段的天逐天列出来（倒序）。
//
// ⚠️ **每一天都列，哪怕那天没听过**：柱状图上那一天的柱子是 0，而少了
// 一天的话图会骗人（用户以为那几天没数据，而不是「听了 0 分钟」）。
func statsDayPoints(start, end time.Time, data app.ListeningStats) []statsDayPoint {
	var out []statsDayPoint
	// 🔴 **从早到晚**：左边是最早那一天，右边是今天。
	//
	// ⚠️ **时间轴从左往右走**，而「越往右越近」——这是时间轴的通例
	// （天气、日历、收支图都是这样）。反过来画（最新的在左边）的话，用户
	// 读出来的趋势是**反的**。
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		out = append(out, statsDayPoint{Key: key, Label: statsDayLabel(d), Ms: data.DayMs(key)})
		if len(out) > statsMaxChartDays {
			break
		}
	}
	return out
}

// statsMaxChartDays 是柱状图最多画多少天。
//
// ⚠️ **「全部」那一档不画柱状图**（那天数可能上千），画的是 Top 歌。
// 而「月」那一档最多 31 天。
const statsMaxChartDays = 31

// statsDayLabel 是柱状图上那个点的字。
func statsDayLabel(d time.Time) string {
	return fmt.Sprintf("%d-%02d", int(d.Month()), d.Day())
}

// ─────────────────────────────────────────────────────────────────────────────
// 数字怎么写
// ─────────────────────────────────────────────────────────────────────────────

// statsDurationText 是「一段时长」写成什么样。
//
// ⚠️ **不足一分钟写「不到 1 分钟」，不是「0 分钟」**——「0 分钟」在屏幕上
// 和「这一格坏了」长得一模一样。
//
// ⚠️ **超过一小时写「x 小时 y 分钟」**：只写分钟的话，一天听了 12 小时会
// 写成「720 分钟」，那个数字用户得自己换算。
func statsDurationText(ms int64) string {
	if ms <= 0 {
		return "还没有记录"
	}
	total := time.Duration(ms) * time.Millisecond
	h := int(total / time.Hour)
	m := int((total % time.Hour) / time.Minute)
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%d 小时 %d 分钟", h, m)
	case h > 0:
		return fmt.Sprintf("%d 小时", h)
	case m > 0:
		return fmt.Sprintf("%d 分钟", m)
	default:
		return "不到 1 分钟"
	}
}

// statsTableDurationText 是表格里那一栏「时长」的字。
//
// ⚠️ **它和 `statsDurationText` 不是同一套**：那一套是给磁贴的说明行用的
// （那里「还没有记录」是说得通的——那一格真的可能是空的）。
//
// 而表格里每一行**都是听过的**——那一行写「还没有记录」是错的，而且读起来
// 像「这一首没数据」。
func statsTableDurationText(ms int64) string {
	if ms <= 0 {
		return "不到 1 分钟"
	}
	return statsDurationText(ms)
}

// statsHoursText 是磁贴上那个大数字（以小时为单位）。
//
// ⚠️ **它和 `statsDurationText` 是两套写法**：磁贴上要的是一个能一眼看的
// 大数字，而说明那一行要的是人话。
func statsHoursText(ms int64) string {
	if ms <= 0 {
		return "0"
	}
	// ⚠️ **毫秒换算成小时**：`time.Hour` 是**纳秒**，而这里拿到的是毫秒——
	// 直接除会差一千倍（90 分钟会写成 0.0）。
	hours := float64(ms) / float64(time.Hour/time.Millisecond)
	if hours < 10 {
		return fmt.Sprintf("%.1f", hours)
	}
	return fmt.Sprintf("%d", int(hours+0.5))
}

// statsCountText 是一个计数的字（"23 首" 那种）。
func statsCountText(n int64, unit string) string {
	if n <= 0 {
		return "0 " + unit
	}
	return fmt.Sprintf("%d %s", n, unit)
}
