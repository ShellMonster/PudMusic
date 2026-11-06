// 「深夜唱片行」的曲目行（DESIGN.md §0′.T7）：列表这一档的行直接用 `widgets/pud` 的
// `TrackRow`（原型 `.trow`），表头用 `pud.TrackHead`——几何（行高 60、列宽、悬停才出的
// 操作键）由那一份定，这里只负责把 `List` 的接线（右键菜单、选中、键盘模式、播放、
// 进 UP 空间）挂上去。
//
// 多选态（`SetSelection` 装了）也走这一份：序号那一格换成勾选框（`pud.Track.Multi`），
// 勾上的整行 accent-soft 底（原型 `trackRows` 的 `multi`）。
package list

import (
	"strconv"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
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

// SetAlbumMode 说「专辑」那一列画不画。页面每帧用 `pud.AlbumModeFor(窗口宽)` 传进来；
// 行和表头读同一个值才对得齐。零值是 `pud.AlbumAuto`（只看列表自己的宽）。
func (l *List) SetAlbumMode(m pud.AlbumMode) { l.albumMode = m }

// PudHeader 画这个列表的原型表头。
func (l *List) PudHeader(c *ui.Context) {
	if l.view == ViewCard {
		return
	}
	pud.TrackHeadMode(c, l.RowWidth(), l.albumMode)
}

// viewPudRow 画一行（`viewCard` / `viewCompactCard` 在非多选态都走这里）。
func (l *List) viewPudRow(r ui.ListRow) {
	c, i := r.Context, r.Index-leadRows(l.lead)
	t := c.Theme()
	m := l.songs[i]
	if isUserItem(m) {
		l.viewPudUserRow(r, i)
		return
	}
	playing := l.isPlaying(m)
	user := false
	sh := ShowOf(m)
	w := l.wiringAt(i)

	tr := pud.Track{
		Index:     indexText(i),
		Title:     sh.Title,
		Artist:    sh.Extra,
		Owner:     m.Owner,
		OwnerTip:  ownerTip(l.acts, m),
		Plays:     l.countText.At(i, m.View, PlayCountText),
		Duration:  l.durText.At(i, m.Duration, formatDuration),
		Playing:   playing,
		Compact:   l.view == ViewCompact,
		AlbumCol:  l.albumMode,
		Chosen:    r.Selected(),
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
	case ev.Like && (l.acts.Liked != nil || l.acts.FavSelect != nil):
		// ⚠️ **喜欢是 `Liked` 那一条**（绑定歌单），`FavSelect` 是兜底
		// （没接喜欢那一摊的地方照旧开多选框）。见 `actions.go` 里
		// `Liked` 的注释。
		if l.acts.Liked != nil {
			l.acts.Liked(m.Item)
		} else {
			l.acts.FavSelect(m.Item)
		}
	case ev.Owner && l.acts.OpenOwner != nil && m.Item.ArtistMid > 0:
		l.acts.OpenOwner(m.Item.ArtistMid, m.Owner)
	}
}

// ownerTip 是 UP 名那一枚的提示：**真接上了 `OpenOwner` 才有**——它同时是
// 「这个名字是链接」的开关（`pud.Track.OwnerTip` 非空才画链接）。没接的
// 列表（比如只装着「看看统计」那一份空操作的）返回空，名字画成普通字，
// 不装假链接。
//
// ⚠️ **还要那个人真的给得出**：B 站有的接口只给名字不给编号
// （`ArtistMid <= 0`），那种情况下点了也到不了他的空间——给提示就是撒谎。
func ownerTip(acts Actions, m Music) string {
	if acts.OpenOwner == nil || m.Item.ArtistMid <= 0 {
		return ""
	}
	return OwnerTipText(m.Owner)
}

// viewPudUserRow 画「用户」那一档的一行：一到两张 UP 主卡（原型 `.od-grid` 两列、缝 16），
// 点卡 = 进他的空间（和旧行的双击 / 菜单「播放」同一个出口）。没有关注键：搜索结果
// 拿不到关注状态，应用也没有关注的写接口。
func (l *List) viewPudUserRow(r ui.ListRow, i int) {
	c := r.Context
	m := l.songs[i]
	const gap = space.Px16
	cw := (l.RowWidth() - gap) / 2
	card := func(m Music) {
		u := pud.UpCard{
			Name:       m.Title,
			Sign:       m.Owner,
			HideFollow: true,
			Face: func(c *ui.Context, w, h float32) {
				imgcache.ViewTitled(c, l.cache, m.Cover, m.Title, w, h)
			},
		}
		if m.Fans > 0 {
			u.Stats = PlayCountText(m.Fans) + " 粉丝"
		}
		if m.Level > 0 {
			u.Level = itoa(m.Level)
		}
		if pud.UCard(c, "ucard:"+m.ID, u, cw).Open && l.acts.Play != nil {
			l.acts.Play([]app.Item{m.Item})
		}
	}
	// ⚠️ **这是布局行，不是一个可悬停的条目**（和 `follow.go` 的卡片行同一个
	// 形状）：一行一到两张 UCard，卡片自己的 hover 由 UCard 担。外层用 padding
	// 留行间距，不用 margin——List 的悬停底会铺到 margin 里，正好形成截图中那条
	// 灰色横带。内层拉伸两张卡，让同一行的卡片底边对齐，即使一张只有昵称、另一张
	// 有两行简介。
	row := ui.Column(c).AlignSelf(ui.Stretch).
		Padding(space.None, space.None, space.Px16, space.None)
	row = LayoutRow(row, c.Theme())
	row.Children(func() {
		cards := ui.Row(c).AlignSelf(ui.Stretch).AlignItems(ui.Stretch).Gap(gap)
		cards.Children(func() {
			card(m)
			if m.Pair != nil {
				card(*m.Pair)
			}
		})
	})
}

func itoa(n int) string { return strconv.Itoa(n) }
