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
// 折叠开关，**两组的空状态合成一句**「还没有收藏夹」（见 viewFavorites），
// 不假装有内容；而「新建收藏夹」建出来的那一行是立刻进侧栏的
// （`app.CreateFolder` 会 `Upsert`），所以菜单里那几件事并不是只有测试走得到。
//
// ⚠️ **整个侧栏折叠起来的时候，收藏夹换成一枚图标 + 一层浮层**（见
// viewFavoritesCollapsed）：折叠态原来干脆不画收藏夹，而折叠侧栏是给屏幕小
// 的人用的——恰恰是这类用户最常去收藏夹。
package ui

import (
	"slices"
	"strconv"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/ui/rowkey"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
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
//
// ⚠️ **空状态只有一句**（报告 C3）：两组各顶一句一模一样的「还没有收藏夹」的
// 话，用户读到两句一样的话，得自己判断「这是说一组还是两组都没有」。所以那
// 一句**只落在最后一个空的那一组底下**——一组有内容、另一组空的时候落在空的
// 那一组底下，两组都空的时候落在下面那一组底下。
func (s *Shell) viewFavorites(c *ui.Context, st app.Settings, collapsed bool) {
	hidden := hiddenMenuKeys(st)
	s.viewFavoritesRefreshing(c, st, collapsed)
	if collapsed {
		s.viewFavoritesCollapsed(c, st)
		return
	}

	// 先算一遍：哪几组是空的、那一句落在哪一组底下。
	items := make([][]app.Favorite, favGroups)
	showEmpty := make([]bool, favGroups)
	lastEmpty := -1
	for g := range favGroups {
		grp := app.FavoriteGroup(g)
		items[g] = visibleFavorites(s.app.SideFavorites(grp), hidden)
		// ⚠️ **折叠着的那一组不算空**：用户自己把它折起来了，这时候在它底下
		// 摆一句「还没有收藏夹」是在说一件他刚刚自己决定不看的事。
		showEmpty[g] = !favoriteFolded(st, grp) && len(items[g]) == 0
		if showEmpty[g] {
			lastEmpty = g
		}
	}
	for g := range favGroups {
		s.viewFavoriteGroup(c, st, app.FavoriteGroup(g), items[g], g == lastEmpty)
	}
}

// favEmptyLine 是侧栏里那一句「还没有收藏夹」。
//
// ⚠️ **它左边缩进和分组标题对齐**：原来它顶格在侧栏最左边（x=0），而分组
// 标题在 x=43 / x=65（**两个还不一样**——因为标题那一枚是 Grow(1) 的按钮，
// 框架的按钮把内容居中，于是「我创建的」那组右边多一个「+」，标题就往左挪
// 22 像素）。一句话顶格在最左边，看着像侧栏底部的一句独立说明，而不属于
// 任何一组（报告 C3）。
//
// 现在分组标题**左对齐**（`Justify(Start)`，和菜单项那一列一样），而这一句
// 跟着它缩进——于是这一片是一个整块。
func (s *Shell) viewFavoritesEmptyLine(c *ui.Context) {
	t := c.Theme()
	// ⚠️ **内边距在「壳」上，不在字上**：字那一格的盒子被 Column 拉满宽
	// （默认 Stretch），量出来的 X 是 0，而字实际起头的位置是内边距那一份。
	// 套一层壳，那一句的盒子就从内边距那儿起头——「它和标题对齐」这件事
	// 才量得出来（测试就是这么量的）。
	ui.Row(c).Padding(space.Px6, favIndent(t)).Children(func() {
		ui.Text(c, "还没有收藏夹").FontSize(scale.Font(c, scale.Size12)).
			TextColor(t.TextMuted)
	})
}

// favIndent 是侧栏收藏夹这一片的缩进：分组标题的左内边距（`flat.Button` 的
// 那个 `t.Space(3.5)`，也就是 14）。
//
// ⚠️ **它跟着主题的间距走**，不是写死的 14：主题的间距变了，两处要一起变。
func favIndent(t *ui.Theme) float32 { return t.Space(3.5) }

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
func (s *Shell) viewFavoritesRefreshing(c *ui.Context, st app.Settings, collapsed bool) {
	if collapsed || !s.app.FavoritesRefreshing() {
		return
	}
	ui.Text(c, refreshLine).FontSize(scale.Font(c, scale.Size12)).
		TextColor(c.Theme().TextMuted).Padding(space.Px4, space.Px8)
}

// viewFavoriteGroup 是一组：折叠标题 + （展开时的）那一列。
//
// 折叠态只画标题，和 Biu 一样（`collection/index.tsx:432-434`）。顺带把
// 折叠态下的拖拽也一起禁掉了——列表根本没建，「禁用」是自然发生的，
// 不需要另写一条分支（SD-09 要的就是这个结果）。
//
// ⚠️ **折叠态（整个侧栏折起来）不在这儿**：那一档走的是一枚「收藏夹」图标 +
// 一层浮层（见 viewFavoritesCollapsed）——整组收藏夹在折叠态**整个不见**的话，
// 用户会以为收藏夹没了（报告 C4）。
func (s *Shell) viewFavoriteGroup(c *ui.Context, st app.Settings, g app.FavoriteGroup,
	items []app.Favorite, showEmpty bool) {
	folded := favoriteFolded(st, g)

	// Key 是这一组的序号：两组各自独立，折叠/展开不会让另一组的状态串过来。
	ui.Column(c).Key("fav-group:" + strconv.Itoa(int(g))).Gap(space.Px2).Children(func() {
		s.viewFavoriteHeader(c, g, folded)
		if folded {
			return
		}
		if len(items) == 0 {
			if showEmpty {
				s.viewFavoritesEmptyLine(c)
			}
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
	ui.Row(c).Gap(space.Px2).Center().Children(func() {
		// ⚠️ **左对齐**（`Justify(Start)`）：框架的按钮把内容**居中**，而这一枚
		// 是 `Grow(1)` 的（为了把「+」推到最右边）——于是两个分组标题**起头的
		// 位置不一样**：「我创建的」那组右边多一个「+」，它的标题就往左挪了
		// 22 像素（实测 x=43 和 x=65）。同一个侧栏里两个标题不在一条竖线上，
		// 而它们下面的内容（收藏夹名）又都在最左边。
		//
		// 左对齐之后，两个标题都在 `favIndent` 那一列上，下面那一句话也跟着
		// 那一列——这一片是一个整块（报告 C3）。
		fold := flatButton(c, arrow+" "+g.String()).Grow(1).
			Justify(ui.Start).TextColor(c.Theme().TextMuted)
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
	// 上游是 `Tooltip closeDelay={0}`（`side/collection/index.tsx:486`）。
	if s.withMenuTip(c, flatIconButton(c, iconAdd, "新建收藏夹", iconSm), "新建收藏夹").Clicked() {
		s.openFolderEdit(0)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 折叠态：一枚「收藏夹」图标 + 一层浮层
// ─────────────────────────────────────────────────────────────────────────────

// favCollapsedName 是折叠态那枚图标的名字（无障碍名字 + 悬停提示）。
//
// ⚠️ **它是一个「入口」的名字，不是「收藏夹」那一页的名字**：它是侧栏里那
// 一枚图标，读屏软件念出来得让人知道按下去会看到收藏夹。
const favCollapsedName = "收藏夹"

// 浮层那块的宽和它离图标多远。
//
// ⚠️ **浮层摆在图标的右边**：侧栏贴着窗口左边，往左摆会摆出窗口——框架自己
// 在放不下的时候会翻到另一边（`mygo/ui/element.go` 的 `AttachTo`），而窗口
// 最窄是 900，那一栏右边有的是地方。
const (
	favPanelW   = 200
	favPanelGap = space.Px6
)

// viewFavoritesCollapsed 是**折叠态**下的收藏夹：一枚图标，点开是一层浮层。
//
// ⚠️ **折叠态原来干脆不画收藏夹**（`viewFavoriteGroup` 第一行就 return 了，
// 注释里写着「这里干脆不画」）——而折叠侧栏是**给屏幕小的人用的**（900 宽的
// 窗口、13 寸屏）。恰恰是这类用户，收藏夹往往是他们最常去的地方。折叠一下，
// 侧栏就少了一整类入口，而**界面上没有任何提示说「收藏夹折叠后不见了」**——
// 用户会以为收藏夹没了（报告 C4）。
//
// 所以折叠态留一枚图标：点了弹一层浮层，列出两组收藏夹，点一个就进去。
func (s *Shell) viewFavoritesCollapsed(c *ui.Context, st app.Settings) {
	anchor := flatIconCellButton(c, iconEmptyFolder, favCollapsedName, sideIconSize, sideIconCell).
		AlignSelf(ui.Center)
	s.sideItemTip(c, anchor, favCollapsedName)
	if anchor.Clicked() {
		s.favPanelOpen = !s.favPanelOpen
	}
	s.viewFavoritesPanel(c, st, anchor)
}

// viewFavoritesPanel 是折叠态那枚图标点开之后的那层浮层。
//
// ⚠️ **它走 `ui.PopoverBase`**（而不是自己拼一个浮层）：框架那一份自带
// 「点外面 / 按 Esc 关掉」，而自己拼的话得自己实现那两件事——漏了就是面板
// 关不掉（点外面也没反应）。
//
// ⚠️ **它不建在侧栏那个滚动容器里**：`PopoverBase` 的面板建在**浮层那一层**
// （`mygo/ui/base.go` 的 `popover`），所以它不会被侧栏的滚动裁掉。
func (s *Shell) viewFavoritesPanel(c *ui.Context, st app.Settings, anchor ui.Element) {
	if !s.favPanelOpen {
		return
	}
	t := c.Theme()
	hidden := hiddenMenuKeys(st)
	ui.PopoverBase(c, anchor, &s.favPanelOpen, func(panel ui.Element) {
		popupPanel(t, panel.
			AttachTo(anchor, ui.AnchorEnd, ui.AnchorStart).MarginStart(favPanelGap).
			Width(favPanelW).Padding(space.Px6)).
			Children(func() {
				// ⚠️ **空状态也只有一句**（和侧栏那一句同一个道理，报告 C3）：
				// 两组都空的时候摆两句一模一样的话，用户得自己判断那是说一组
				// 还是两组都没有。那一句落在**最后一个空的那一组**底下。
				items := make([][]app.Favorite, favGroups)
				lastEmpty := -1
				for g := range favGroups {
					items[g] = visibleFavorites(s.app.SideFavorites(app.FavoriteGroup(g)), hidden)
					if len(items[g]) == 0 {
						lastEmpty = g
					}
				}
				for g := range favGroups {
					ui.Text(c, app.FavoriteGroup(g).String()).FontSize(scale.Font(c, scale.Size12)).
						TextColor(t.TextMuted).Padding(space.Px4, space.Px8)
					for _, f := range items[g] {
						s.viewFavoritesPanelRow(c, f)
					}
					if g == lastEmpty {
						ui.Text(c, "还没有收藏夹").FontSize(scale.Font(c, scale.Size12)).
							TextColor(t.TextMuted).Padding(space.Px2, space.Px8)
					}
				}
			})
	})
}

// viewFavoritesPanelRow 是浮层里那一个收藏夹。
func (s *Shell) viewFavoritesPanelRow(c *ui.Context, f app.Favorite) {
	t := c.Theme()
	label := f.Title
	if label == "" {
		label = "（没名字）"
	}
	row := ui.Row(c).FillWidth().Padding(space.Px4, space.Px8).Radius(t.Radius)
	row.Children(func() {
		ui.Text(c, label).FontSize(scale.Font(c, scale.FavPanelItem)).Grow(1).
			MaxLines(1).Ellipsis("…")
	})
	if row.Clicked() {
		// ⚠️ **进去之后要把浮层收掉**：它开着的话会盖在收藏夹那一页上面，
		// 而用户已经到那一页了。
		s.favPanelOpen = false
		s.navigate(collectionPath(f.ID))
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
		s.addToast("折叠状态存不住："+err.Error(), notice.Danger)
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
	// ⚠️ **身份是预先装箱好的那一份**（`s.sideRowKeys[g]`）：装箱那一步要
	// 分配，现算的话每帧每行一次（票 #313）。
	ls.Key = func(i int) any {
		return s.sideRowKeys[g].At(i, items[i].ID, rowkey.Identity64)
	}
	ls.Label = func(i int) string { return items[i].Title }
	ls.Reorder = func(rows []int, to int) {
		s.reorderFavorite(g, items, rows, to)
	}

	e := ui.List(c, ls, len(items), func(i int) {
		s.viewFavoriteRow(c, g, items[i])
	})
	s.trackScrollbar(e)
}

// reorderFavorite 是拖完那一刻把顺序落下去。
//
// `to` 是 MyGo 给的**落点**：要插到第 `to` 行**前面**；`to` 等于列表的行数
// 就是排到最后面。依据是 `mygo/ui/list.go:863` 的 dropAt——「the row before
// which rows dropped at y go」——落点那条指示线就画在第 `to` 行的**上边缘**
// （`list.go:886` 的 boundary）。
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
		s.addToast("顺序存不住："+err.Error(), notice.Danger)
	}
}

// viewFavoriteRow 是侧栏里的一行。
func (s *Shell) viewFavoriteRow(c *ui.Context, g app.FavoriteGroup, f app.Favorite) {
	t := c.Theme()
	cur := s.router.Path() == collectionPath(f.ID)

	row := ui.Column(c).Key(favRowKey(g, f.ID)).Grow(1).Radius(t.Radius).Padding(space.Px6, space.Px8)
	switch {
	case cur:
		// 当前就在这一页：和侧栏菜单当前项**同一套**画法（`flatCurrent`——
		// 中性灰底 + 主色的字，深浅两套同一个数）。
		//
		// ⚠️ **原来这里是实心主色**（`Background(t.Accent)`）：那是审计 F-02 /
		// F-06 点名的那一处——同一个屏上（关注页、动态流页）左栏是实心绿大块、
		// 侧栏是淡底，两种画法并排，而实心那一块比主操作还响。
		row = flatCurrent(c, row)
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
		// 🔴 **单行 + 省略号**：名字一长（上游那种「【长期主义】2026 年度歌单
		// 合集·最终版」）这一行会长高一截，把下面那些收藏夹整体顶下去，
		// 侧栏跟着抖。侧栏只有 160~480 宽，超过约 20 个汉字就换行。
		//
		// ⚠️ **别用 `Wrap()`**：那是给弹性容器用的（让子元素换行），对文字
		// 叶子无效。列表行和播放条那几处都是「单行 + 省略号」这一套
		// （`library_rows.go` / `follow.go`），照它们抄。
		//
		// 上游那边是 CSS 的 `truncate`（`biu/src/components/menu/menu-item.tsx:111`
		// 的 `className="pointer-events-none truncate"`），那是同一个意思。
		ui.Text(c, label).FontSize(scale.Font(c, scale.Size14)).TextColor(rowTextColor(t, cur)).Grow(1).
			MaxLines(1).Ellipsis("…")
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

// rowTextColor 是收藏夹那一行的字色。
//
// ⚠️ **和 `flatCurrent` 配的是同一档**（`flatCurrentText`）：这一行的字是**自己
// 画的**（不是从父元素继承），所以底换了字也得跟着换——深色下当前那一行是
// 主色字，浅色下是**从主色现算的、压到 ≥4.5:1 的那一档**（主色压在浅底上读不
// 出来，见 flatbutton.go 那一段）。
func rowTextColor(t *ui.Theme, current bool) ui.Color {
	return flatCurrentText(t, current)
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
			s.addToast("隐藏存不住："+err.Error(), notice.Danger)
			return
		}
		s.addToast("已把「"+f.Title+"」从侧栏藏起来", notice.Success)
	case "play":
		s.app.PlayFolder(f, s.sayFolderDone)
	case "add-to-playlist":
		s.app.EnqueueFolder(f, s.sayFolderDone)
	case "delete":
		// 只记，不做：真正那一下在 `viewFavoriteDelete` 里（那个框是**模态**，
		// 在它上面点任何别的控件都不该生效，所以两步必须分开）。
		s.folderDelete = favoriteDeleteAsk{open: true, id: f.ID, title: f.Title}
	case "unfavorite":
		s.app.UnfavoriteFolder(f, s.sayFolderDone)
	default:
		// ⚠️ **不许说「还没接接口」**（票 #242，UX 报告 §3-F 的 F2）：那是把
		// 实现细节说给用户听。
		s.addToast("「"+item.label+"」现在还不能用", notice.Warning)
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
// 用**应用自绘**的确认框（`confirmModal`）：上游那一份是
// `components/confirm-modal`，而删收藏夹在那边是 `type: "danger"`
// （`side/collection/index.tsx:249`）。
func (s *Shell) viewFavoriteDelete(c *ui.Context) {
	d := &s.folderDelete
	if !d.open {
		return
	}
	// ⚠️ 确认框自己会把 `d.open` 关掉（确认和取消都关），所以这里不用再关一遍。
	if confirmModal(c, &d.open, confirmSpec{
		tone:    toneDanger,
		title:   "删掉「" + d.title + "」吗？",
		body:    "这个收藏夹和里面的所有歌都会没，撤不回来。",
		confirm: "删除",
		cancel:  "取消",
	}) != confirmAccepted {
		// 取消 / Esc / 点外面：什么都不做。
		return
	}
	s.app.DeleteFavoriteFolder(app.Favorite{ID: d.id, Title: d.title}, s.sayFolderDone)
}

// sayFolderDone 是侧栏收藏夹那几个后台动作跑完之后说的话。
//
// ⚠️ **它在后台 goroutine 上被调**（App 那四条都是「立刻返回 + 回调」）：Toast
// 自己带锁，而 `wake.invalidate` 只是叫醒界面，两者都不碰界面状态。
//
// 参数是**已经说全了一整句**的话，成功和失败都是（见
// `app/sidebar_folders.go` 的文件头）——这一层不再加任何前缀。
func (s *Shell) sayFolderDone(msg string, tone notice.Tone) {
	s.addToast(msg, tone)
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
	if msg, tone, ok := s.app.TakeFavoritesRefresh(); ok {
		s.addToast(msg, tone)
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
