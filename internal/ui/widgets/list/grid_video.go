// 网格的「视频卡」格子（UP 空间「投稿」那一档，原型 `videoCard` / `.vgrid`）。
package list

import (
	"strconv"
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/egoist/mygo/ui"
)

// videoAgo 是视频卡副题里的「几天前」（原型 `daysAgo`：今天 / 昨天 / N 天前 / N 个月前 / N 年前）。
// 零值时间是空串（这一页没有这个数）。
func videoAgo(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := calendarDaysBetween(t, now)
	switch {
	case d < 1:
		return "今天"
	case d < 2:
		return "昨天"
	case d < 30:
		return strconv.Itoa(d) + " 天前"
	case d < 365:
		return strconv.Itoa(d/30) + " 个月前"
	}
	return strconv.Itoa(d/365) + " 年前"
}

// viewVideoCell 画一格视频卡：16:9 缩略图 + 播放量 / 时长小牌 + 两行标题 + 「UP 名 · 几个月前」。
//
// 标题是**原标题**（原型 `${t.t} · ${t.a}`，不走 `ShowOf` 的清洗：视频标题不是歌名）。
// 单击封面 = 放这一首（原型 `data-act="play"`），右键出这一格的菜单。播放量是 `Music.View`，
// 时长是 `Music.Duration`；弹幕数 `bili.UpVideo` 不带，那一半不画。
func (g *Grid) viewVideoCell(c *ui.Context, i int, cell float32) {
	m := g.songs[i]
	owner := m.Owner
	if owner == "" {
		owner = m.Item.Artist
	}
	sub := owner
	if t := videoAgo(m.PubTime, time.Now()); t != "" {
		if sub != "" {
			sub += " · "
		}
		sub += t
	}
	v := pud.Video{
		Title:    m.Title,
		Sub:      sub,
		Plays:    g.countText.At(i, m.View, PlayCountText),
		Duration: g.durText.At(i, m.Duration, formatDuration),
		Playing:  isPlaying(g.acts, m.Item),
		// 圆角是 `pud.VideoCard` 传进来的 r-8，落在最内层的图上（GPU 只有最内层裁剪切圆角）。
		Cover: func(c *ui.Context, w, h, radius float32) {
			imgcache.ViewRadius(c, g.cache, m.Cover, w, h, radius)
		},
	}
	ui.Column(c).Key("vcell:" + m.ID).
		ContextMenu(func(menu *ui.Menu) { buildSongMenu(menu, g.songs, g.acts, i, g.playFrom) }).
		Children(func() {
			if pud.VideoCard(c, "vcard:"+m.ID, v, cell) {
				g.sel = i
				g.rowPlay(i)
			}
		})
}
