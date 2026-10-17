// 侧栏里那两组收藏夹：Biu 的 SD-05~SD-09。
//
// 两组（我创建的 / 我收藏的）各有折叠、右键菜单、拖拽排序，顺序由
// `internal/app/favorites.go` 落盘。这一阶段还没有 B 站接口，条目是谁给的
// 见那个文件的包注释；空的时候两组照画标题和折叠开关，里面一行
// 「还没有收藏夹」，不假装有内容。
package ui

import (
	"slices"
	"strconv"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// favGroups 是侧栏上有几组收藏夹。app 那边也是两组（见 app.FavoriteGroup）。
const favGroups = 2

// favMenuAction 是右键菜单里的一项。
type favMenuAction struct {
	key   string
	label string
}

// favMenuItems 是两组的右键菜单，项数和顺序逐条照 Biu
// （`biu/src/layout/side/collection/index.tsx:91-116`）。
//
// ⚠️ 验收写的是「我创建的 5 项、我收藏的 4 项」，所以**不要**顺手给两组统一
// 菜单：Biu 的差在「修改」「删除」只属于我创建的，「取消收藏」只属于我
// 收藏的。
func favMenuItems(g app.FavoriteGroup) []favMenuAction {
	if g == app.FavoritesCollected {
		return []favMenuAction{
			{"play", "播放"},
			{"add-to-playlist", "添加到播放列表"},
			{"hide", "隐藏"},
			{"unfavorite", "取消收藏"},
		}
	}
	return []favMenuAction{
		{"play", "播放"},
		{"add-to-playlist", "添加到播放列表"},
		{"edit", "修改"},
		{"hide", "隐藏"},
		{"delete", "删除"},
	}
}

// viewFavorites 是两组收藏夹，按「我创建的」在上、「我收藏的」在下排
// （Biu 的 `collection/index.tsx:545-549`）。
func (s *Shell) viewFavorites(c *ui.Context, st app.Settings) {
	hidden := hiddenMenuKeys(st)
	for g := range favGroups {
		s.viewFavoriteGroup(c, st, app.FavoriteGroup(g), hidden)
	}
}

// viewFavoriteGroup 是一组：折叠标题 + （展开时的）那一列。
//
// 折叠态只画标题，和 Biu 一样（`collection/index.tsx:432-434`）。顺带把
// 折叠态下的拖拽也一起禁掉了——列表根本没建，「禁用」是自然发生的，
// 不需要另写一条分支（SD-09 要的就是这个结果）。
func (s *Shell) viewFavoriteGroup(c *ui.Context, st app.Settings, g app.FavoriteGroup, hidden map[string]bool) {
	if st.SideMenuCollapsed {
		// 折叠态是 72 宽，只摆得下图标。Biu 摆的是图标，本项目还没有图标
		// 资源，摆一列字会被裁成半个字。这里干脆不画：拖拽在折叠态本来
		// 就是禁用的（SD-09），摆出来也点不出一件有用的事。
		return
	}

	folded := favoriteFolded(st, g)
	items := visibleFavorites(s.app.SideFavorites(g), hidden)

	// Key 是这一组的序号：两组各自独立，折叠/展开不会让另一组的状态串过来。
	ui.Column(c).Key("fav-group:" + strconv.Itoa(int(g))).Gap(2).Children(func() {
		s.viewFavoriteHeader(c, g, folded)
		if folded {
			return
		}
		if len(items) == 0 {
			ui.Text(c, "还没有收藏夹").FontSize(12).
				TextColor(c.Theme().TextMuted).Padding(6, 8)
			return
		}
		s.viewFavoriteList(c, g, items)
	})
}

// viewFavoriteHeader 是那一组的折叠开关，右边跟着一个「新建收藏夹」。
//
// 「新建」只给「我创建的」那组（Biu 的 `collection/index.tsx:486-498`：那个
// + 号是 `renderFavoriteGroup` 的 `titleExtra`，只有 created 那一组传了它）。
// 「我收藏的」里新建出来的东西属于**别人**，自己建的会出现在上面那一组。
func (s *Shell) viewFavoriteHeader(c *ui.Context, g app.FavoriteGroup, folded bool) {
	arrow := "▾"
	if folded {
		arrow = "▸"
	}

	// 不补 `Label(g.String())`：可见的字已经说了它是什么，再补一份只会让
	// 画面上出现两份同样的文字（无障碍节点也算一份）。
	//
	// 也不写 `Grow(1)`：Column 默认就把子元素拉满宽（`docs/ui/layout.md`），
	// 而 Grow 是纵向的「吃掉剩下的空间」——写在这里，它在折叠的那一组里
	// 会把下面的东西顶没了。
	ui.Row(c).Gap(2).Center().Children(func() {
		fold := ui.Button(c, arrow+" "+g.String()).Grow(1)
		if fold.Clicked() {
			s.toggleFavoriteFolded(g, folded)
		}
		// ⚠️ **这一组折叠着的时候不画 +**：Biu 那边这个 + 是
		// `renderFavoriteGroup` 的 `titleExtra`，而折叠的那一支只画标题，
		// `titleExtra` 跟着一起折了（`collection/index.tsx:432-434`）。
		// 侧栏**整体**折叠是更上面那一道（`viewFavoriteGroup` 直接 return），
		// 和这一个不是同一件事。
		if g == app.FavoritesCreated && !folded {
			s.viewNewFavoriteButton(c)
		}
	})
}

// viewNewFavoriteButton 是「我创建的」那一组标题右边那个 +。
//
// ⚠️ **图标用「+」**：Biu 那个是 `RiAddLine`（remixicon 的图标），本项目还没
// 接图标资源，而字形它自带。`Label` 补上「新建收藏夹」是为了读屏和 Tooltip——
// 画面上只有一个 +，光看不知道是什么。
func (s *Shell) viewNewFavoriteButton(c *ui.Context) {
	if ui.Button(c, "+").Tooltip("新建收藏夹").Label("新建收藏夹").Clicked() {
		s.openFolderEdit(0)
	}
}

// toggleFavoriteFolded 记下这一组折叠了没有。折叠态也存——Biu 就是存进
// `sideMenuCollectionFolded` 的（`collection/index.tsx:73-80`），下次打开
// 侧栏还是那个样子。
func (s *Shell) toggleFavoriteFolded(g app.FavoriteGroup, folded bool) {
	if _, err := s.app.UpdateSettings(func(st *app.Settings) {
		if g == app.FavoritesCollected {
			st.SideMenuCollectionFolded.Collected = !folded
		} else {
			st.SideMenuCollectionFolded.Created = !folded
		}
	}); err != nil {
		s.addToast("折叠状态存不住：" + err.Error())
	}
}

// favoriteFolded 是这一组此刻折叠了没有。
func favoriteFolded(st app.Settings, g app.FavoriteGroup) bool {
	if g == app.FavoritesCollected {
		return st.SideMenuCollectionFolded.Collected
	}
	return st.SideMenuCollectionFolded.Created
}

// viewFavoriteList 是一组里能拖的那一列。
func (s *Shell) viewFavoriteList(c *ui.Context, g app.FavoriteGroup, items []app.Favorite) {
	ls := &s.sideLists[g]

	// 🔴 铁律四：每行的 key 用收藏夹的 ID，不用序号。序号是「父 ID + 兄弟
	// 序号」那一套，在前面插一条、或者拖完顺序变了，后面所有行的状态
	// （选中、正在被拖、焦点）会整个串到别的收藏夹上——实测就是「拖完选中
	// 的那一首变成了旁边另一首」。
	ls.Key = func(i int) any { return items[i].ID }
	ls.Label = func(i int) string { return items[i].Title }
	ls.Reorder = func(rows []int, to int) {
		s.reorderFavorite(g, items, rows, to)
	}

	ui.List(c, ls, len(items), func(i int) {
		s.viewFavoriteRow(c, g, items[i])
	})
}

// reorderFavorite 是拖完那一刻把顺序落下去。
//
// `to` 是 MyGo 给的**落点**：要插到第 `to` 行**前面**；`to` 等于列表的行数
// 就是排到最后面。依据是 `mygo/ui/list.go:727` 的 dropAt——「the row before
// which rows dropped at y go」——落点那条指示线就画在第 `to` 行的**上边缘**
// （`list.go:744` 的 boundary）。
//
// ⚠️ 这里只把序号换成 ID，**不许**再自己挪一格。之前写的是「to 超过末行就改用
// 最后一行」，那把「插到某行前面」当成了「挪到某行的位置」：往下拖的时候指示
// 线画在目标上边缘、落下去却停在目标**下面**，差一格，而且错的那条顺序会立刻
// 被 ReorderFavorite 写进设置文件。
//
// 末尾那一档交给 beforeID = 0 表示（见 app.Favorites.Reorder），别拿最后一行
// 当目标顶替——那会把「拖到最后面」变成「拖到最后一行前面」。
func (s *Shell) reorderFavorite(g app.FavoriteGroup, items []app.Favorite, rows []int, to int) {
	// 侧栏一次只拖一行（没有多选）。真来了多行，跳过而不是做半截：
	// 做一半的表现是「拖了三条只动了一条」，比不动更难解释。
	if len(rows) != 1 || len(items) == 0 {
		return
	}
	// before 保持 0 时，app 那边理解为「挪到最后面」。
	before := int64(0)
	if to >= 0 && to < len(items) {
		before = items[to].ID
	}
	if _, err := s.app.ReorderFavorite(g, items[rows[0]].ID, before); err != nil {
		s.addToast("顺序存不住：" + err.Error())
	}
}

// viewFavoriteRow 是侧栏里的一行。
func (s *Shell) viewFavoriteRow(c *ui.Context, g app.FavoriteGroup, f app.Favorite) {
	t := c.Theme()
	cur := s.router.Path() == collectionPath(f.ID)

	row := ui.Column(c).Key(favRowKey(g, f.ID)).Grow(1).Radius(6).Padding(6, 8)
	switch {
	case cur:
		row = row.Background(t.Accent)
	case row.Hovered():
		// 悬停要有反应：这一行点得动（进收藏夹页），没有反馈的话用户
		// 不知道它是能点的。Biu 那边是 MenuItem 自带的 hover。
		row = row.Background(t.Surface.Mix(t.Text, 0.08))
	}

	row.Children(func() {
		label := f.Title
		if label == "" {
			label = "（没名字）"
		}
		ui.Text(c, label).FontSize(14).TextColor(rowTextColor(t, cur)).Grow(1)
	})

	if row.Clicked() {
		s.navigate(collectionPath(f.ID))
	}

	// 右键菜单挂在**行**上，不是列上：MyGo 取的是最里层那个带菜单的元素
	// （`ui/menu.go` 的 menuTarget），挂在外面的容器上整列共用一份菜单。
	row.ContextMenu(func(m *ui.Menu) {
		s.favoriteMenu(g, f, m)
	})
}

// favRowKey 是一行的 key。用「组 + ID」：两组可能有同一个 ID（B 站那边
// 自己收藏的和自己创建的收藏夹完全可能是同一个），只给 ID 会让两组撞 key
// ——Tester 里撞 key 直接 panic（`ui/diagnostics.go`）。
func favRowKey(g app.FavoriteGroup, id int64) string {
	return "fav-row:" + strconv.Itoa(int(g)) + ":" + strconv.FormatInt(id, 10)
}

// rowTextColor 是收藏夹那一行的字色：当前所在页用主色上的反色，其余用正文色。
func rowTextColor(t *ui.Theme, current bool) ui.Color {
	if current {
		return t.AccentText
	}
	return t.Text
}

// favoriteMenu 建这一行的系统原生右键菜单。
//
// 用 `ContextMenu` 而不是自绘浮层：验收要的是「系统原生样式、方向键能导航」，
// 只有系统菜单免费给这两样。
func (s *Shell) favoriteMenu(g app.FavoriteGroup, f app.Favorite, m *ui.Menu) {
	for _, item := range favMenuItems(g) {
		// ⚠️ 每一项都要 Item 一遍：菜单是这趟全部建出来的，只有 Chosen
		// 的那一项为 true。少建一项，那一项就点不到。
		if m.Item(item.label).Chosen() {
			s.favoriteAction(f, item)
			return
		}
	}
}

// favoriteAction 是右键菜单被选中之后做的事。
//
// 「隐藏」写进 `hiddenMenuKeys`（Biu 就是这么做的，
// `collection/index.tsx:232-244`），「修改」开出编辑弹窗。其余几项还要
// B 站接口，说清楚比按了没反应好。
func (s *Shell) favoriteAction(f app.Favorite, item favMenuAction) {
	switch item.key {
	case "edit":
		// ⚠️ 传的是收藏夹的 **id**，不是序号：B 站的接口要的是 media_id。
		// 传错的话弹窗打开、拉回来的是另一个收藏夹的名字，而用户是在改这一
		// 个——症状是「打开之后标题栏上写着别人的收藏夹名」。
		s.openFolderEdit(f.ID)
	case "hide":
		key := strconv.FormatInt(f.ID, 10)
		_, err := s.app.UpdateSettings(func(st *app.Settings) {
			if !slices.Contains(st.HiddenMenuKeys, key) {
				// Clone 再追加：SettingsSnapshot 给回来的切片是 store 里的
				// 那一份，直接 append 有可能写到别人正在读的那段后面。
				st.HiddenMenuKeys = append(slices.Clone(st.HiddenMenuKeys), key)
			}
		})
		if err != nil {
			s.addToast("隐藏存不住：" + err.Error())
			return
		}
		s.addToast("已把「" + f.Title + "」从侧栏藏起来")
	default:
		s.addToast("「" + item.label + "」还没接收藏夹接口")
	}
}

// hiddenMenuKeys 是被藏起来的那些 ID。Biu 存的是十进制字符串
// （`String(item.id)`），我们照抄——不然两边互导设置时对不上。
func hiddenMenuKeys(st app.Settings) map[string]bool {
	m := make(map[string]bool, len(st.HiddenMenuKeys))
	for _, k := range st.HiddenMenuKeys {
		m[k] = true
	}
	return m
}

// visibleFavorites 是滤掉被藏起来的之后还摆得出来的那些。顺序不变。
func visibleFavorites(items []app.Favorite, hidden map[string]bool) []app.Favorite {
	out := make([]app.Favorite, 0, len(items))
	for _, f := range items {
		if !hidden[strconv.FormatInt(f.ID, 10)] {
			out = append(out, f)
		}
	}
	return out
}
