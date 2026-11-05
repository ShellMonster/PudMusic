// 稍后再看 / 历史记录两页的原型样式行和筛选零件（DESIGN.md §0′.T7）。
//
// 行是 `pud.TrackRow`（原型 `.trow`），右键菜单、选中、键盘模式的接线沿用
// `pagedRows` 那一套；历史记录按日分组，组头是「今天 / 昨天 / M 月 D 日」（宋体 20）
// 加「N 首」（原型 `.day-head`）。
package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// libraryRowSpec 是一页曲目行要定的几样。
type libraryRowSpec struct {
	// key 是这一页行的身份前缀（两页不能重名）。
	key string
	// colText 是「专辑」那一列的字（稍后再看：看到哪儿；历史：几点播放的）。
	colText func(s *Shell, m libraryRow) string
	// remove 非 nil 时悬停出现一枚「移除」。
	remove func(i int)
	// byDay 按日分组：行的序号按组重新数，组首行带组头。
	byDay bool
}

// libraryDone 报这一条稍后再看是不是已经看完（B 站给 -1 或看到了片尾）。
func libraryDone(m libraryRow) bool {
	return m.Progress < 0 || (m.Duration > 0 && m.Progress >= m.Duration)
}

// laterColText 是稍后再看的「专辑」列：已看完 / 看到 m:ss / 还没看（原型 `later` 的 colText）。
func laterColText(_ *Shell, m libraryRow) string {
	switch {
	case libraryDone(m):
		return "已看完"
	case m.Progress > 0:
		return "看到 " + clockText(m.Progress)
	}
	return "还没看"
}

// historyColText 是历史记录的「专辑」列：「HH:MM 播放」（原型 `history` 的 colText）。
func historyColText(_ *Shell, m libraryRow) string {
	if m.ViewedAt.IsZero() {
		return ""
	}
	return m.ViewedAt.Format("15:04") + " 播放"
}

// dayLabel 是组头上的字：今天 / 昨天 / M 月 D 日。
func dayLabel(t, now time.Time) string {
	today := startOfDay(now)
	diff := int(today.Sub(startOfDay(t)).Hours()/24 + 0.5)
	switch {
	case diff <= 0:
		return "今天"
	case diff == 1:
		return "昨天"
	}
	return strconv.Itoa(int(t.Month())) + " 月 " + strconv.Itoa(t.Day()) + " 日"
}

// drawLibraryPudRow 画第 r 行（返回的元素由 `pagedRows.viewRow` 挂右键菜单）。
func (s *Shell) drawLibraryPudRow(c *ui.Context, r ui.ListRow, rows *pagedRows, sp libraryRowSpec) ui.Element {
	t := c.Theme()
	all := rows.Rows()
	i := r.Index
	m := all[i]
	w := rows.width
	if w <= 0 {
		w = 960
	}
	winW, _ := c.Size()

	col := ui.Column(c).Key(sp.key + m.ID).AlignSelf(ui.Stretch)
	return col.Children(func() {
		index := i + 1
		if sp.byDay {
			first := i == 0 || !sameDay(all[i-1].ViewedAt, m.ViewedAt)
			if first {
				n := 0
				for j := i; j < len(all) && sameDay(all[j].ViewedAt, m.ViewedAt); j++ {
					n++
				}
				// ⚠️ **组头画在选中行外面**：它属于「这一天」这一组，不属于
				// 「这一首」——塞进选中的那一列的话，指针移到组里第一首上时
				// hover 底色会连「今天」两个字一起盖住（原型 `.day-head` 和
				// `.trow` 也是两个独立元素）。
				s.viewDayHead(c, dayLabel(m.ViewedAt, s.clock.Now()), n, i == 0)
			}
			index = 1
			for j := i - 1; j >= 0 && sameDay(all[j].ViewedAt, m.ViewedAt); j-- {
				index++
			}
		}
		tr := pud.Track{
			Index:     strconv.Itoa(index),
			Title:     m.Title,
			Owner:     m.Owner,
			Plays:     rows.countText.At(i, m.View, playCountText),
			Duration:  rows.durText.At(i, m.Duration, clockText),
			Album:     sp.colText(s, m),
			Playing:   rows.isPlaying(m),
			Compact:   rows.Layout() == list.ViewCompact,
			AlbumCol:  pud.AlbumModeFor(winW),
			CanLike:   rows.acts.FavSelect != nil,
			Removable: sp.remove != nil,
			Dim:       !m.CanPlay,
			Menu:      func(mn *ui.Menu) { rows.menu(mn, i) },
			Cover: func(c *ui.Context, cw, ch float32) {
				imgcache.ViewTitled(c, s.images, m.Cover, m.Title, cw, ch)
			},
		}
		// ⚠️ 选中态只包**曲目行这一格**（`SelectedRow` 的 hover / 选中底都
		// 落在这一格上），不包上面的组头——组头跟着第一首走，但不是第一首
		// 的一部分。
		track := ui.Column(c).AlignSelf(ui.Stretch)
		track = list.SelectedRow(track, t, r.Selected(), track.Hovered(), rows.keys.On() && list.Focused(c, &rows.state))
		var ev pud.TrackEvents
		track.Children(func() {
			ev = pud.TrackRow(c, sp.key+"t:"+m.ID, tr, w)
		})
		switch {
		case ev.Play:
			rows.playFrom(i)
		case ev.Like:
			rows.actions().FavSelect(i)
		case ev.Remove && sp.remove != nil:
			sp.remove(i)
		case ev.Owner && rows.acts.OpenOwner != nil && m.Item.ArtistMid > 0:
			rows.acts.OpenOwner(m.Item.ArtistMid, m.Owner)
		}
	})
}

// viewDayHead 是历史记录的组头（原型 `.day-head`：上 32 下 8、左右 12，宋体 20 + 12 号等宽
// 「N 首」，基线对齐、缝 12）。第一组紧贴表头之上的工具条，上边距照原型也是 32。
func (s *Shell) viewDayHead(c *ui.Context, label string, n int, _ bool) {
	p := tok.Of(c.Theme())
	ui.Row(c).FillWidth().AlignItems(ui.Baseline).Gap(space.Px12).
		Margin(space.Px32, space.None, space.Px8, space.None).PaddingX(space.Px12).Children(func() {
		typo.Display(ui.Text(c, label), scale.Font(c, scale.Subhead)).SingleLine()
		typo.Mono(ui.Text(c, strconv.Itoa(n)+" 首")).FontSize(scale.Font(c, scale.Size12)).
			TextColor(p.Text3).SingleLine()
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 日期范围（原型 `range-pop`）
// ─────────────────────────────────────────────────────────────────────────────

// pudRanges 是预设，下标 4 是「不按日期筛」。
var pudRanges = []struct {
	label string
	days  int
}{{"今天", 0}, {"最近 7 天", 7}, {"最近 14 天", 14}, {"最近 30 天", 30}, {"不按日期筛", -1}}

// pudRangeState 是日期筛选弹层的状态（每页一份）。
type pudRangeState struct {
	open     bool
	sel      int // pudRanges 的下标；-1 = 自定义区间
	from, to string
	err      string
	label    string // 自定义区间的显示字
}

func newPudRangeState() pudRangeState { return pudRangeState{sel: len(pudRanges) - 1} }

// rangeLabel 是按钮上写的字。
func (st *pudRangeState) rangeLabel() string {
	if st.sel >= 0 && st.sel < len(pudRanges) {
		if st.sel == len(pudRanges)-1 {
			return "全部时间"
		}
		return pudRanges[st.sel].label
	}
	return st.label
}

// viewPudRange 画「日历 + 当前范围 + 下箭头」那枚小幽灵键和它的弹层；返回这一帧筛选条件变了没。
func (s *Shell) viewPudRange(c *ui.Context, key string, st *pudRangeState, f *dateFilter, now time.Time) (changed bool) {
	b := pud.DropButton(c, key, st.rangeLabel(), "calendar")
	if b.Clicked() {
		st.open = !st.open
	}
	ui.Popover(c, b, &st.open, func() {
		pud.PopPanel(c, key+":pop").MinWidth(300).Children(func() {
			pud.PopHeading(c, "按日期筛")
			for i, rg := range pudRanges {
				if pud.PopOpt(c, key+":o"+strconv.Itoa(i), rg.label, "", st.sel == i).Clicked() {
					st.sel, st.err, st.open = i, "", false
					if rg.days < 0 {
						f.setPreset(presetAll, now)
					} else {
						f.setRange(startOfDay(now.AddDate(0, 0, -rg.days)), endOfDay(now), true)
					}
					changed = true
				}
			}
			pud.PopSep(c)
			ui.Row(c).Gap(space.Px8).Padding(space.Px4, space.Px10, space.Px8, space.Px10).Children(func() {
				for _, fld := range []struct {
					name string
					v    *string
				}{{"开始", &st.from}, {"结束", &st.to}} {
					ui.Column(c).Grow(1).Gap(space.Px4).Children(func() {
						ui.Text(c, fld.name).FontSize(scale.Font(c, scale.Size12)).TextColor(tok.Of(c.Theme()).Text3)
						ui.TextInput(c, fld.v).Label(fld.name + "日期").Placeholder("YYYY-MM-DD")
					})
				}
			})
			if st.err != "" {
				ui.Text(c, st.err).FontSize(scale.Font(c, scale.Size12)).TextColor(tok.Of(c.Theme()).Err).
					Padding(space.None, space.Px10).Margin(space.None, space.None, space.Px8, space.None)
			}
			ui.Row(c).FillWidth().Justify(ui.End).Padding(space.None, space.Px10, space.Px8, space.Px10).Children(func() {
				if pud.Button(c, key+":apply", pud.Primary, pud.Sm, "应用", "").Clicked() {
					from, e1 := time.ParseInLocation("2006-01-02", strings.TrimSpace(st.from), now.Location())
					to, e2 := time.ParseInLocation("2006-01-02", strings.TrimSpace(st.to), now.Location())
					switch {
					case e1 != nil || e2 != nil:
						st.err = "请选好开始和结束日期"
					case from.After(to):
						st.err = "开始日期不能晚于结束日期"
					default:
						st.err, st.open, st.sel = "", false, -1
						st.label = from.Format("01/02") + " – " + to.Format("01/02")
						f.setRange(startOfDay(from), endOfDay(to), true)
						changed = true
					}
				}
			})
		})
	})
	return changed
}
