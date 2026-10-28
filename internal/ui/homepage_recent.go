// 首页「最近播放」那一档的页内头部：上次听到哪儿的「继续播放」大卡片 + 今天 / 昨天 /
// 更早各有几首的分组行，再接列表表头。
//
// ⚠️ **列表是定高的虚拟行，插不进分组标题行**，所以分组用头部里这一行汇总来说，
// 每首歌自己的那一列「观看时间」仍写着今天 / 昨天 / N天前。
package ui

import (
	"strconv"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	homeRecentCardH = 128
	homeRecentCover = 96
	homeRecentBarW  = 240
	homeRecentBarH  = 4
	homeRecentLeadH = homeRecentCardH + 12 + 20 + 12 + 40
)

// recentSummary 数「今天 / 昨天 / 更早」各有几首，空组不出现。
func recentSummary(items []list.Music, text func(list.Music) string) string {
	var today, yest, earlier int
	for _, m := range items {
		switch text(m) {
		case "今天":
			today++
		case "昨天":
			yest++
		default:
			earlier++
		}
	}
	var parts []string
	for _, g := range []struct {
		name string
		n    int
	}{{"今天", today}, {"昨天", yest}, {"更早", earlier}} {
		if g.n > 0 {
			parts = append(parts, g.name+" "+strconv.Itoa(g.n)+" 首")
		}
	}
	return strings.Join(parts, "  ·  ")
}

func (h *homePage) viewRecentLead(c *ui.Context) {
	t := c.Theme()
	items := h.lists[homeRecentTab].Items()
	ui.Column(c).FillWidth().Gap(space.Px12).Padding(space.None, space.None, space.Px12).Children(func() {
		if len(items) > 0 {
			h.viewRecentCard(c, items[0])
			now := c.Now()
			sum := recentSummary(items, func(m list.Music) string { return list.PubTimeText(m.PubTime, now) })
			if sum != "" {
				ui.Text(c, sum).Key("home:recent:groups").FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted)
			}
		}
		h.lists[homeRecentTab].Header(c, "home:header:"+strconv.Itoa(homeRecentTab))
	})
}

func (h *homePage) viewRecentCard(c *ui.Context, m list.Music) {
	t := c.Theme()
	sh := list.ShowOf(m)
	card := ui.Row(c).Key("home:recent:card").FillWidth().Height(homeRecentCardH).AlignItems(ui.Center).
		Gap(space.Px16).Padding(space.Px16).Radius(t.Radius + 4).Background(t.Text.Alpha(0.05)).
		Focusable().Role(ui.RoleButton).Label("继续播放 " + sh.Title).OnClick(func() { h.playNow(m) })
	card.Children(func() {
		imgcache.ViewTitled(c, h.newGrid.cache, m.Cover, sh.Title, homeRecentCover, homeRecentCover)
		ui.Column(c).Grow(1).Gap(space.Px4).Children(func() {
			ui.Text(c, "上次听到这里").FontSize(scale.Font(c, scale.Size13)).TextColor(t.Accent)
			ui.Text(c, sh.Title).FontSize(scale.Font(c, scale.Size18)).Bold().TextColor(t.Text).MaxLines(1).Ellipsis("…")
			if sh.Owner != "" {
				ui.Text(c, sh.Owner).FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted).MaxLines(1).Ellipsis("…")
			}
			if m.Progress > 0 && m.Duration > 0 {
				f := min(float32(m.Progress)/float32(m.Duration), 1)
				ui.Box(c).Key("home:recent:bar").Size(homeRecentBarW, homeRecentBarH).Radius(homeRecentBarH / 2).
					Background(t.Text.Alpha(0.12)).Clip().Children(func() {
					ui.Box(c).Size(homeRecentBarW*f, homeRecentBarH).Background(t.Accent)
				})
			}
		})
		ui.Box(c).Key("home:recent:play").Size(48, 48).Radius(24).Background(t.Accent).Center().Children(func() {
			ui.Icon(c, icon(iconPlayFill)).FontSize(24).TextColor(t.AccentText)
		})
	})
}
