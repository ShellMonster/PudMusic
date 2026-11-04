// 首页「榜单」和「新歌」两档（原型 `PAGES.home` 的 `chart` / `new` 两支，CSS 383–392 行）。
//
// # 榜单：本周最热
//
// 数据是 B 站音乐站热榜的最新一期前 20 首（`app.HomeTabHot`）。每行：大序号（宋体 32，
// 前三名换 accent-text）、56 的封面、歌名 + 「歌手 · 播放量」、升降。
//
// **升降**：B 站榜接口只给当期名次，所以升降来自 app 层记下的上一期名次
// （`app.Recommend.ChartTrend`，见 `app/chart_history.go`）：上升 `↑ n`（ok 色）、下降
// `↓ n`（text-3）、持平或没有上一期记录画「—」。第一次用时整张榜都是「—」。
//
// # 新歌：新歌速递
//
// 用曲目行（`pud.TrackRow`），原型里「专辑」那一列在这里写「x 天前上传」：数据是
// B 站新歌速递接口给的上架日期（月-日，年份由 `bili.parsePublishMD` 推出来）。
package ui

import (
	"fmt"
	"strconv"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/title"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// homeChartColGap 是榜单两列之间的缝（原型 `.chart { column-gap: 48px }`）。
const homeChartColGap = 48

// homeChartCols 是榜单排几列：内容区够宽（≥ 948，和卡片网格同一条线）排两列，否则一列。
func homeChartCols(contentW float32) int {
	if contentW >= pud.HideAlbumBelow {
		return 2
	}
	return 1
}

// viewChart 画「榜单」这一档。
func (h *homePage) viewChart(c *ui.Context, s *Shell, inner float32) {
	sec := &h.feed.sec[homeChart]
	songs := sec.songs
	if len(songs) > homeChartCount {
		songs = songs[:homeChartCount]
	}
	h.section(c, func() {
		if pud.SecHeadIcon(c, "home:chart:all", "本周最热", "按本周播放量排序 · 每周一更新", "播放前 20", "<play") {
			h.playFrom(s, songs, 0)
		}
		switch {
		case len(songs) > 0:
			cw := h.contentW
			if cw <= 0 {
				cw = inner
			}
			cols := homeChartCols(cw)
			colW := (inner - float32(cols-1)*homeChartColGap) / float32(cols)
			ui.Column(c).FillWidth().Children(func() {
				for start := 0; start < len(songs); start += cols {
					end := min(start+cols, len(songs))
					ui.Row(c).Key("home:chart:line:" + songs[start].ID).Gap(homeChartColGap).Children(func() {
						for i := start; i < end; i++ {
							h.viewChartRow(c, s, songs, i, colW)
						}
					})
				}
			})
		case sec.err != nil:
			h.sectionError(c, "home:chart:retry", sec.err, func() { h.retry(homeChart) })
		case sec.started && !sec.loading:
			pud.Empty(c, pud.ArtPlain, "榜单暂时是空的", "B 站这一期还没有给出榜单，过一会儿再来看看。", nil)
		default:
			for i := 0; i < 6; i++ {
				pud.SkeletonRow(c, "home:chart:sk:"+strconv.Itoa(i), inner)
			}
		}
	})
}

// viewChartRow 画榜单的一行（原型 `.chart-row`：序号 56 | 封面 56 | 文字 | 升降，缝 16，
// 内边距 12 8，底下一条 line，悬停 hover 底）。整行都能点（播放这一首往后）。
func (h *homePage) viewChartRow(c *ui.Context, s *Shell, songs []list.Music, i int, w float32) {
	p := tok.Of(c.Theme())
	m := songs[i]
	name := title.Clean(m.Title).Title
	row := ui.ButtonBase(c).Key("home:chart:row:"+m.ID).Label("播放 "+name).Width(w).
		Padding(space.Px12, space.Px8).Gap(space.Px16).AlignItems(ui.Center).
		Draw(func(pp *ui.Painter, r ui.Rect) {
			pp.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 1, W: r.W, H: 1}, p.Line, 0)
		})
	if row.Hovered() {
		row = row.Background(p.Hover)
	}
	row.Children(func() {
		numColor := p.Text3
		if i < 3 {
			numColor = p.AccentText
		}
		ui.Box(c).Width(56).Shrink(0).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			typo.Display(ui.Text(c, strconv.Itoa(i+1)), scale.Font(c, scale.Size32)).TextColor(numColor).SingleLine()
		})
		ui.Box(c).Size(56, 56).Radius(tok.R4).Background(p.Surface2).Clip().Shrink(0).Children(func() {
			h.coverOf(m.Cover)(c, 56, 56)
		})
		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			fg := p.Text
			if h.isCur(m) {
				fg = p.AccentText
			}
			ui.Text(c, name).FontSize(scale.Font(c, scale.Size14)).FontWeight(600).TextColor(fg).
				SingleLine().Ellipsis("…")
			if sub := homeCardSub(m); sub != "" {
				ui.Text(c, sub).FontSize(scale.Font(c, scale.Size13)).TextColor(p.Text2).SingleLine().Ellipsis("…")
			}
		})
		h.viewTrend(c, m.ID)
	})
	if row.Clicked() {
		h.playFrom(s, songs, i)
	}
}

// homeTrendText 把升降写成字：上升「↑ n」、下降「↓ n」，持平或没有记录「—」。
func homeTrendText(delta int, ok bool) (text string, up bool) {
	switch {
	case !ok || delta == 0:
		return "—", false
	case delta > 0:
		return fmt.Sprintf("↑ %d", delta), true
	}
	return fmt.Sprintf("↓ %d", -delta), false
}

// viewTrend 画升降那一格（原型 `.trend`：等宽 12，上升 ok 色，其余 text-3）。
func (h *homePage) viewTrend(c *ui.Context, id string) {
	p := tok.Of(c.Theme())
	var delta int
	var ok bool
	if h.loads.trend != nil {
		delta, ok = h.loads.trend(id)
	}
	text, up := homeTrendText(delta, ok)
	col := p.Text3
	if up {
		col = p.Ok
	}
	typo.Mono(ui.Text(c, text)).FontSize(scale.Font(c, scale.Size12)).TextColor(col).SingleLine().Shrink(0)
}

// viewNew 画「新歌」这一档：新歌速递的曲目行。
func (h *homePage) viewNew(c *ui.Context, s *Shell, inner float32) {
	sec := &h.feed.sec[homeNew]
	h.section(c, func() {
		pud.SecHead(c, "home:new:head", "新歌速递", "B 站音乐站最近上架", "")
		switch {
		case len(sec.songs) > 0:
			pud.TrackHead(c, inner)
			now := c.Now()
			for i, m := range sec.songs {
				ev := pud.TrackRow(c, "home:new:"+m.ID, pud.Track{
					Index:    strconv.Itoa(i + 1),
					Title:    title.Clean(m.Title).Title,
					Artist:   m.Owner,
					Album:    homeDaysAgo(m.PubTime, now),
					Plays:    homePlays(m.View),
					Duration: clockText(m.Duration),
					Cover:    h.coverOf(m.Cover),
					Playing:  h.isCur(m),
					Paused:   !h.playing,
				}, inner)
				if ev.Play {
					h.playFrom(s, sec.songs, i)
				}
			}
		case sec.err != nil:
			h.sectionError(c, "home:new:retry", sec.err, func() { h.retry(homeNew) })
		case sec.started && !sec.loading:
			pud.Empty(c, pud.ArtPlain, "还没有新歌", "B 站最近没有新上架的歌，过一会儿再来看看。", nil)
		default:
			for i := 0; i < 8; i++ {
				pud.SkeletonRow(c, "home:new:sk:"+strconv.Itoa(i), inner)
			}
		}
	})
}
