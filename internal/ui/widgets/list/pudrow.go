// 「深夜唱片行」的曲目行（DESIGN.md §0′.T7）：列表这一档的行直接用 `widgets/pud` 的
// `TrackRow`（原型 `.trow`），表头用 `pud.TrackHead`——几何（行高 60、列宽、悬停才出的
// 操作键）由那一份定，这里只负责把 `List` 的接线（右键菜单、选中、键盘模式、播放、
// 进 UP 空间）挂上去。
//
// ⚠️ **多选态（`SetSelection` 装了）仍走 `card.go` 里那份旧行**：勾选框那一列原型的
// 曲目行里没有对应位置的接线，收藏夹页（T3）改版时再换；这里不替它决定。
package list

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/egoist/mygo/ui"
)

// pudDefaultW 是还没量过列表宽（第一帧）时用的行宽。
const pudDefaultW = 960

// RowWidth 是这个列表此刻行的宽（上一帧量到的盒子宽；第一帧用默认值）。页面把它交给
// `pud.TrackHead`，表头和行读同一个数才对得齐。
func (l *List) RowWidth() float32 {
	if l.width > 0 {
		return l.width
	}
	return pudDefaultW
}

// usesPudRow 报这个列表此刻画不画原型那种行（多选态走旧行）。
func (l *List) usesPudRow() bool { return l.pud }

// SetPudRows 让这个列表的行和表头改用原型样式（`pud.TrackRow` / `pud.TrackHead`）。
// 默认关：没改版的页面保持旧行，改版的页面在建列表时打开它。
func (l *List) SetPudRows(on bool) { l.pud = on }

// PudHeader 画这个列表的原型表头；多选态退回旧表头。
func (l *List) PudHeader(c *ui.Context) {
	if l.view == ViewCard {
		return
	}
	pud.TrackHead(c, l.RowWidth())
}

// viewPudRow 画一行（`viewCard` / `viewCompactCard` 在非多选态都走这里）。
func (l *List) viewPudRow(r ui.ListRow) {
	c, i := r.Context, r.Index-leadRows(l.lead)
	t := c.Theme()
	m := l.songs[i]
	playing := l.isPlaying(m)
	user := isUserItem(m)
	sh := ShowOf(m)
	w := l.wiringAt(i)

	tr := pud.Track{
		Index:     indexText(i),
		Title:     sh.Title,
		Artist:    sh.Extra,
		Owner:     m.Owner,
		Plays:     l.countText.At(i, m.View, PlayCountText),
		Duration:  l.durText.At(i, m.Duration, formatDuration),
		Playing:   playing,
		Compact:   l.view == ViewCompact,
		Highlight: l.hl,
		Dim:       m.Item.Invalid,
		CanLike:   !user && l.acts.FavSelect != nil,
		Menu:      func(mn *ui.Menu) { l.buildMenu(mn, i) },
		Cover: func(c *ui.Context, cw, ch float32) {
			imgcache.ViewTitled(c, l.cache, m.Cover, m.Title, cw, ch)
		},
	}
	if m.Item.Invalid {
		// 失效的条目：歌名压成次要色（`Dim`），歌名下面一枚「已失效」角标（和旧行的 `InvalidTag` 同一句）。
		tr.Quality = "已失效"
	}
	cols := l.Columns()
	switch {
	case cols.Text != nil:
		tr.Album = cols.Text(m)
	case cols.PubTime:
		tr.Album = PubTimeText(m.PubTime, l.now())
	}
	if !cols.PlayCount {
		tr.Plays = ""
	}
	if user {
		tr.Duration = ""
	}
	if l.sel2.On != nil {
		// 批量选择态（收藏夹的多选）：序号格换成勾选框，勾上的整行 accent-soft 底。
		tr.Multi, tr.Checked = true, l.sel2.On(i)
		tr.Selected = tr.Checked
	}
	tr.RowWire = func(row ui.Element) ui.Element {
		row = row.ContextMenu(func(mn *ui.Menu) { l.buildMenu(mn, i) }).HandleInput(w.input)
		return SelectedRow(row, t, r.Selected(), row.Hovered(), l.keys.On() && Focused(c, &l.state))
	}

	ev := pud.TrackRow(c, "row:"+m.ID, tr, l.RowWidth())
	switch {
	case ev.Check && l.sel2.Toggle != nil:
		l.sel2.Toggle(i)
	case ev.Play:
		l.rowPlay(i)
	case ev.Like && l.acts.FavSelect != nil:
		l.acts.FavSelect(m.Item)
	case ev.Owner && l.acts.OpenOwner != nil && m.Item.ArtistMid > 0:
		l.acts.OpenOwner(m.Item.ArtistMid, m.Owner)
	}
}
