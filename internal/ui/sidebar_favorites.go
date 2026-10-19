// 侧栏里那两组收藏夹：Biu 的 SD-05~SD-09。
//
// 两组（我创建的 / 我收藏的）各有折叠、右键菜单、拖拽排序，顺序由
// `internal/app/favorites.go` 落盘；菜单里那几件能动的事（播放 / 添加到列表 /
// 删除 / 取消收藏）在 `app/sidebar_folders.go` 里——那一层认识 B 站，这一层
// 不认识。
//
// ⚠️ **只有「我创建的」那一组有数据源**：它由头像下拉里「刷新数据」真的从
// B 站拉下来（见 `app/favorites_refresh.go`）。「我收藏的」那一组本项目还没接
// 那个接口，条目由谁调 `SetFavorites` 谁负责——所以它空的时候两组照画标题和
// 折叠开关，里面一行「还没有收藏夹」，不假装有内容；而「新建收藏夹」建出来
// 的那一行是立刻进侧栏的（`app.CreateFolder` 会 `Upsert`），所以菜单里那几件
// 事并不是只有测试走得到。
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
//
// 顶上那一行「正在刷新收藏夹…」是头像下拉里「刷新数据」那一项的在飞状态
// （见 viewFavoritesRefreshing）。它排在两组**外面**而不是某一组底下：刷的是
// 这一片，不是其中一组——摆在某一组底下的话，另一组会像是也在动。
func (s *Shell) viewFavorites(c *ui.Context, st app.Settings) {
	hidden := hiddenMenuKeys(st)
	s.viewFavoritesRefreshing(c, st)
	for g := range favGroups {
		s.viewFavoriteGroup(c, st, app.FavoriteGroup(g), hidden)
	}
}

// refreshLine 是「正在刷」那一行的字。
//
// ⚠️ 界面上**不许**把这句话当成唯一的在飞证据：它会随侧栏的折叠 / 展开出现又
// 消失。真正的闸门在应用层（`app.RefreshFavorites` 的 busy），这一行只是让用户
// 看得见——「按下去没反应」是这一项最容易出的那种错。
const refreshLine = "正在刷新收藏夹…"

// viewFavoritesRefreshing 是那一行。
//
// 折叠态不画，理由同 viewLoggedOutHint：72 宽摆不下，而且整个收藏夹那一块
// 在折叠态压根不画（`viewFavoriteGroup` 第一行就 return 了）——单独留一句悬在
// 空侧栏里，用户会以为收藏夹跑到别处去了。
func (s *Shell) viewFavoritesRefreshing(c *ui.Context, st app.Settings) {
	if st.SideMenuCollapsed || !s.app.FavoritesRefreshing() {
		return
	}
	ui.Text(c, refreshLine).FontSize(12).
		TextColor(c.Theme().TextMuted).Padding(4, 8)
}

// viewFavoriteGroup 是一组：折叠标题 + （展开时的）那一列。
//
// 折叠态只画标题，和 Biu 一样（`collection/index.tsx:432-434`）。顺带把
// 折叠态下的拖拽也一起禁掉了——列表根本没建，「禁用」是自然发生的，
// 不需要另写一条分支（SD-09 要的就是这个结果）。
func (s *Shell) viewFavoriteGroup(c *ui.Context, st app.Settings, g app.FavoriteGroup, hidden map[string]bool) {
	if st.SideMenuCollapsed {
		// 折叠态是 72 宽，只摆得下图标。Biu 摆的是图标，而**这一档还没换**：
		// 收藏夹有几组、每组画哪一枚要对着 Biu 逐条抄，抄错一处那一格就
		// 配错意思（72 宽里只有一枚图标，没有字可以对照着认）。摆一列字会被
		// 裁成半个字。这里干脆不画：拖拽在折叠态本来就是禁用的（SD-09），
		// 摆出来也点不出一件有用的事。
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
	//
	// ⚠️ **灰字**：上游的分组标题是弱化的（`layout/side/collection/index.tsx:425`
	// 一带的次级文字），而菜单项和收藏夹名才是 `t.Text`。这一层要是和下面那些
	// 一样黑，六个分组标题就会比六个名字还抢眼。
	ui.Row(c).Gap(2).Center().Children(func() {
		fold := flatButton(c, arrow+" "+g.String()).Grow(1).
			TextColor(c.Theme().TextMuted)
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

// viewNewFavoriteButton 是「我创建的」那一组标题右边那个「加」。
//
// 图标是 `iconAdd`（上游 `RiAddLine`，逐字取的同一个文件）。`Label` 是「新建
// 收藏夹」：图标对辅助技术是不可见的（`mygo/ui/svg.go` 的 Icon 文档），而这一
// 颗画出来只有一个加号，光看不知道是什么。
func (s *Shell) viewNewFavoriteButton(c *ui.Context) {
	if flatIconButton(c, iconAdd, "新建收藏夹", iconSm).
		Tooltip("新建收藏夹").Clicked() {
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
// 五项里现在**五项都在做那件事**：编辑 / 隐藏是本地的，播放 / 添加到列表是
// 拉内容（`app.PlayFolder` / `app.EnqueueFolder`），删除 / 取消收藏动的是用户
// 真实的数据（`app.DeleteFavoriteFolder` / `app.UnfavoriteFolder`）。
//
// 🔴 **「删除」和「取消收藏」的分界在这一点上**：删除连同里面的歌一起没了，
// 所以先弹二次确认（`viewFavoriteDelete`）；取消收藏只是把我这一份摘掉，
// 歌还在 UP 主那里，所以**不要确认框**——撤回来只要再点一次，而点错之后还弹
// 一个框只会更烦。
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
	case "play":
		s.app.PlayFolder(f, s.sayFolderDone)
	case "add-to-playlist":
		s.app.EnqueueFolder(f, s.sayFolderDone)
	case "delete":
		// 只记，不做：真正那一下在 `viewFavoriteDelete` 里（确认框是系统
		// 模态，在它上面点任何别的控件都不该生效，所以两步必须分开）。
		s.folderDelete = favoriteDeleteAsk{open: true, id: f.ID, title: f.Title}
	case "unfavorite":
		s.app.UnfavoriteFolder(f, s.sayFolderDone)
	default:
		s.addToast("「" + item.label + "」还没接收藏夹接口")
	}
}

// favoriteDeleteAsk 是「删掉这个收藏夹」那个二次确认要跨帧活着的状态。
//
// 挂在壳上而不是局部变量：即时模式下一帧一新建，而确认框要开着不动（铁律一）。
type favoriteDeleteAsk struct {
	open  bool
	id    int64
	title string
}

// viewFavoriteDelete 是那个二次确认。
//
// ⚠️ **它不在侧栏里画**，而是挂在 `viewFolderDialogs` 那儿（模态弹窗都在那儿）。
// 画在侧栏里的后果不是「位置不对」：侧栏整个折叠时不画收藏夹，弹窗就跟着
// 不见了，而用户刚按完「删除」——按下去之后界面毫无反应。
//
// 用 MyGo 的 `AlertDialog`（系统原生确认框）：它免费给到「点外面不关」和
// 「Esc 当取消」——**点外面不关正是二次确认要的**（自绘浮层很容易漏掉，而漏掉
// 的后果是误删整个收藏夹）。
func (s *Shell) viewFavoriteDelete(c *ui.Context) {
	d := &s.folderDelete
	if !d.open {
		return
	}
	switch ui.AlertDialog(c, &d.open,
		"删掉「"+d.title+"」吗？",
		"这个收藏夹和里面的所有歌都会没，撤不回来。", "取消", "删除") {
	case 1:
		// ⚠️ 先关掉再干活：删成功之后侧栏那一行没了，而 `d` 是下一帧还要读
		// 的状态（不清的话下一帧同一个框又弹一次）。
		d.open = false
		s.app.DeleteFavoriteFolder(app.Favorite{ID: d.id, Title: d.title}, s.sayFolderDone)
	case ui.AlertDismissed:
		// Esc = 取消。
		d.open = false
	}
}

// sayFolderDone 是侧栏收藏夹那几个后台动作跑完之后说的话。
//
// ⚠️ **它在后台 goroutine 上被调**（App 那四条都是「立刻返回 + 回调」）：Toast
// 自己带锁，而 `wake.invalidate` 只是叫醒界面，两者都不碰界面状态。
//
// 参数是**已经说全了一整句**的话，成功和失败都是（见
// `app/sidebar_folders.go` 的文件头）——这一层不再加任何前缀。
func (s *Shell) sayFolderDone(msg string) {
	s.addToast(msg)
	s.wake.invalidate()
}

// reportFavoritesRefresh 把「刷新数据」那一趟回来的话弹出来。
//
// **每帧开头调一次**，和 `reportUpdateNotice` 同一排：后台把话攒进信箱，而界面
// 每一帧去取一次。取走而不是读（同 `TakeFolderResult` 的理由）——
//
// ⚠️ 它和侧栏那一行「正在刷新收藏夹…」是**两件事**，别合成一个判据：在飞的时候
// 只有那一行在（话还没攒下），跑完之后话先弹出来、下一帧那一行才收掉。
func (s *Shell) reportFavoritesRefresh() {
	if msg, ok := s.app.TakeFavoritesRefresh(); ok {
		s.addToast(msg)
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
