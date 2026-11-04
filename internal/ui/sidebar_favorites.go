// 侧栏里的收藏夹（原型 `.fav-section`，DESIGN.md §0′.T1）。
//
// 两组：「收藏夹」（我创建的，标题右边有 + 新建键）和「收藏的合集」（我收藏的，
// 有内容才出现）。每组上面是一行 12 号 500 text-3 的小标题（`.side-label`），下面是
// 收藏夹行（`.fav-item`）：32 封面（圆角 4）+ 名称（13 / 500）+ 「N 首 · 私密」
// （11 / text-3）；选中行 `surface-2` 底，悬停 `hover` 底。折叠态（侧栏 72）只剩
// 封面，行高 44，居中。
//
// 数据是真数据：`app.SideFavorites` 两组、封面和数量都来自 B 站列表接口自带的字段，
// 不另发请求。原型的「我喜欢」是假数据里的一个合集，本仓库没有对应的页，不画；
// 「私密」目前列表接口没带，不显示（见 DESIGN.md §0′.T1）。
//
// 原型没有的东西删了入口：两组的折叠开关、折叠态的收藏夹浮层。右键菜单、拖拽排序
// 这些能力照旧在行上。
package ui

import (
	"slices"
	"strconv"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/rowkey"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

const favGroups = 2

// 原型 `.fav-item` / `.fav-cover` / `.fav-text` 的尺寸。
const (
	favCoverSize = 32
	favCoverGap  = space.Px10
)

// favGroupLabels 是两组的小标题（原型 `.side-label`）：我创建的 = 「收藏夹」，
// 我收藏的 = 「收藏的合集」。
var favGroupLabels = [favGroups]string{"收藏夹", "收藏的合集"}

// viewFavorites 是侧栏里的收藏夹那一片。
//
// 折叠态：不画小标题和说明，只画一列封面（行高 44）。登录前只有小标题和一句
// 「登录后能看到收藏夹。去登录」（原型 `loggedOut` 那一支）。
func (s *Shell) viewFavorites(c *ui.Context, st app.Settings, collapsed bool) {
	hidden := hiddenMenuKeys(st)
	items := make([][]app.Favorite, favGroups)
	for g := range favGroups {
		items[g] = visibleFavorites(s.app.SideFavorites(app.FavoriteGroup(g)), hidden)
	}
	loggedIn := s.app.LoggedIn()
	// ⚠️ **Key 挂在这一层 Column 上**：它不是处理输入的控件（里面的按钮各自有 key）。
	ui.Column(c).Key("fav-area").FillWidth().Children(func() {
		for g := range favGroups {
			grp := app.FavoriteGroup(g)
			// 「收藏的合集」没有内容就不出现（原型 `subs.length ?`）；「收藏夹」总在。
			if g == int(app.FavoritesCollected) && len(items[g]) == 0 {
				continue
			}
			if !collapsed {
				s.viewFavoriteLabel(c, grp, loggedIn || len(items[g]) > 0)
			}
			if len(items[g]) > 0 {
				s.viewFavoriteList(c, grp, items[g], collapsed)
			}
		}
		if collapsed {
			return
		}
		s.viewFavoritesRefreshing(c)
		// 空状态只有一句，且只在「收藏夹」（第一组）为空时说：它说的是这一片的状态。
		if len(items[app.FavoritesCreated]) == 0 && len(items[app.FavoritesCollected]) == 0 {
			s.viewFavoritesNote(c, s.favEmptyText(loggedIn))
		}
	})
}

// viewFavoriteLabel 是组的小标题（原型 `.side-label`：12 / 500 / text-3、字距 0.04em、
// 左右 12、上 16 下 8），「收藏夹」右边有一枚 24×24 的 + 键（圆角 4，text-3）。
func (s *Shell) viewFavoriteLabel(c *ui.Context, g app.FavoriteGroup, canNew bool) {
	p := tok.Of(c.Theme())
	ui.Row(c).Key("fav-label:"+strconv.Itoa(int(g))).FillWidth().AlignItems(ui.Center).
		Padding(space.Px16, space.Px12, space.Px8, space.Px12).Children(func() {
		ui.Text(c, favGroupLabels[g]).FontSize(scale.Font(c, scale.Size12)).FontWeight(500).
			LetterSpacing(0.48).TextColor(p.Text3).SingleLine().Grow(1)
		if g != app.FavoritesCreated || !canNew {
			return
		}
		b := ui.ButtonBase(c).Key("fav-new").Label("新建收藏夹").Size(24, 24).Radius(tok.R4).
			AlignItems(ui.Center).Justify(ui.Center)
		fg := p.Text3
		if b.Hovered() {
			b = b.Background(p.Hover)
			fg = p.Text
		}
		b.Transition(motion.Fade).Children(func() {
			ui.Icon(c, icon(pudPlus)).FontSize(14).TextColor(fg)
		})
		s.withMenuTip(c, b, "新建收藏夹")
		if b.Clicked() {
			s.openFolderEdit(0)
		}
	})
}

// viewFavoritesNote 是小标题下面那一句说明（原型：13 号 text-2、左右 12），后面跟着
// 一个能点的下一步（去登录 / 重试 / 新建收藏夹，主色字）。
func (s *Shell) viewFavoritesNote(c *ui.Context, text string) {
	p := tok.Of(c.Theme())
	_, label, run := s.favEmptyAction(s.app.LoggedIn())
	ui.Column(c).Key("fav-empty").FillWidth().Padding(space.None, space.Px12).Children(func() {
		ui.Row(c).Wrap().AlignItems(ui.Center).Children(func() {
			ui.Text(c, text+"。").FontSize(scale.Font(c, scale.Size13)).TextColor(p.Text2)
			if label == "" {
				return
			}
			b := ui.ButtonBase(c).Key("fav-empty-act").Label(label).AlignItems(ui.Center)
			b.Children(func() {
				t := ui.Text(c, label).FontSize(scale.Font(c, scale.Size13)).TextColor(p.AccentText).SingleLine()
				if b.Hovered() {
					t.Underline()
				}
			})
			if b.Clicked() {
				run()
			}
		})
	})
}

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

// favEmptyText 是侧栏里那一句空状态。
//
// ⚠️ **三档三句话，缺一句就是撒谎**（AGENTS.md §4：「没有」和「没显示出来」
// 在画面上长得一样）：空列表有三种来由，而只有一种能被说成「还没有收藏夹」。
//
//   - **真的一 个都没有**（拉成功、B 站那边确实没有）→ 「还没有收藏夹」。
//   - **还没拉**（没登录、刚启动还没轮到）→ 不能说「还没有」——那是在替
//     B 站回答一个我们还没问过的问题。
//   - **拉失败了**（断网、登录态过期）→ 更不能说「还没有」：用户读到它就会
//     以为 B 站那边确实空着，而真相是我们没拉到。**这一档必须说得出
//     「可以重试」**——那是用户知道自己该怎么办的唯一线索。
//
// ⚠️ **它说的是这一片收藏夹，不是一组**：两组的空状态合成一句（报告 C3），
// 而合成的那一句得站在**最要紧的那一档**上——有一组拉失败了就是「拉不到」，
// 哪怕另一组拉到了而且是空的。
func (s *Shell) favEmptyText(loggedIn bool) string {
	loaded, failed := s.favAreaState()
	switch {
	case failed:
		return "拉不到收藏夹，可以重试"
	case !loaded && !loggedIn:
		return "登录后能看到收藏夹"
	case !loaded:
		return "还没拉收藏夹"
	default:
		return "还没有收藏夹"
	}
}

// favAreaState 报这一片收藏夹此刻的状态。
//
// 第一项是「都拉到了吗」，第二项是「有哪一组拉失败了吗」。
//
// ⚠️ **失败优先于「都拉到了」**：一组成功了、另一组失败了的时候，
// 「还没有收藏夹」是撒谎——真相是有一组我们没拉到，而用户看到的是空的。
func (s *Shell) favAreaState() (loaded, failed bool) {
	loaded = true
	for g := range favGroups {
		st := s.app.FavoriteState(app.FavoriteGroup(g))
		if st.Err != "" {
			failed = true
		}
		if !st.Loaded {
			loaded = false
		}
	}
	return loaded, failed
}

// favEmptyAction 是空状态那一句旁边的「下一步」：图标、按钮字、点了做什么。
// 按钮字空串 = 这一档没有下一步（还没拉：等就行）。
//
// 和 `favEmptyText` 同一个四档判据：拉失败 → 重试；没登录 → 去登录；
// 真的没有 → 新建收藏夹。
func (s *Shell) favEmptyAction(loggedIn bool) (ic, label string, run func()) {
	loaded, failed := s.favAreaState()
	switch {
	case failed:
		return iconRefresh, "重试", s.app.RefreshFavorites
	case !loaded && !loggedIn:
		return iconLoginCircle, "去登录", func() { s.goLogin(homePath) }
	case !loaded:
		return iconFolder, "", nil
	default:
		return iconFolderAdd, "新建收藏夹", func() { s.openFolderEdit(0) }
	}
}

// refreshLine 是「正在刷」那一行的字：头像下拉里「刷新数据」那一项的在飞状态。
const refreshLine = "正在刷新收藏夹…"

// viewFavoritesRefreshing 是那一行（12 号 text-3，左右 12）。折叠态不画。
func (s *Shell) viewFavoritesRefreshing(c *ui.Context) {
	if !s.app.FavoritesRefreshing() {
		return
	}
	ui.Text(c, refreshLine).FontSize(scale.Font(c, scale.Size12)).
		TextColor(tok.Of(c.Theme()).Text3).Padding(space.Px4, space.Px12)
}

// onCollectionPage 报「当前就在某个收藏夹页上」（认路径前缀）。
func (s *Shell) onCollectionPage() bool {
	return strings.HasPrefix(s.router.Path(), collectionPathPrefix)
}

// viewFavoriteList 是一组里能拖的那一列。
func (s *Shell) viewFavoriteList(c *ui.Context, g app.FavoriteGroup, items []app.Favorite, collapsed bool) {
	ls := &s.sideLists[g]

	// 🔴 铁律四：每行的 key 用收藏夹的 ID，不用序号（拖完顺序变了、前面插一条，
	// 序号会让选中 / 拖动 / 焦点串到别的收藏夹上）。身份是预先装箱好的那一份，
	// 现算的话每帧每行分配一次（票 #313）。
	ls.Key = func(i int) any {
		return s.sideRowKeys[g].At(i, items[i].ID, rowkey.Identity64)
	}
	ls.Label = func(i int) string { return items[i].Title }
	ls.Reorder = func(rows []int, to int) {
		s.reorderFavorite(g, items, rows, to)
	}

	e := ui.List(c, ls, len(items), func(i int) {
		s.viewFavoriteRow(c, g, items[i], collapsed)
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

// collectionPathOf 是「侧栏上这一行点进去」那个地址。
//
// ⚠️ **类型要带上**：收藏夹（11）、合集（21）、系列（31）是三个不同的东西，
// 而 `collectionPath` 只给 id——于是点一个合集会**按收藏夹打开**：打不开，
// 或者打开的是另一个容器。
//
// ⚠️ **今天侧栏上只有收藏夹那一种行**（合集 / 系列被 `app.collectedFavorites`
// 滤掉了——它们的内容列表本项目还没接）。这一条是**路由本身**认得出类型：
// 等那一行放开时，地址已经是对的。
//
// ⚠️ **收藏夹（11）那一档 URL 上不写 `?type=`**：那是默认值，而 Biu 的地址
// 也是不带的那一种（`/collection/{id}`）——两边的地址长得一样，导来导去才对得上。
func collectionPathOf(f app.Favorite) string {
	q := collectionTypeQueryOf(f)
	if q == "" {
		return collectionPath(f.ID)
	}
	return collectionPath(f.ID) + "?" + collectionTypeQuery + "=" + q
}

// collectionTypeQueryOf 是这一行的类型在 URL 上写成什么。
//
// 认不出来的类型（别的页面填的）当成收藏夹，而收藏夹**不写**——
// 见 `collectionPathOf` 的注释。
func collectionTypeQueryOf(f app.Favorite) string {
	t := bili.CollectionType(f.Type)
	if !t.Valid() || t == bili.CollectionFavorite {
		return ""
	}
	return strconv.Itoa(int(t))
}

// viewFavoriteRow 是侧栏里的一行（原型 `.fav-item`）：内边距 6/12、缝 10、圆角 8；
// 32 封面 + 名称（13 / 500）+ 「N 首」（11 / text-3）。选中行 `surface-2` 底，名称
// 正文色；悬停 `hover` 底。折叠态只剩封面，行高 44、居中，无障碍名字是收藏夹名。
func (s *Shell) viewFavoriteRow(c *ui.Context, g app.FavoriteGroup, f app.Favorite, collapsed bool) {
	p := tok.Of(c.Theme())
	// ⚠️ **当前项要连类型一起比**：合集（21）和收藏夹（11）是两个不同的容器，
	// 而 id 各自一套。
	cur := s.router.Path() == collectionPath(f.ID) &&
		s.router.Query(collectionTypeQuery) == collectionTypeQueryOf(f) && !f.Invalid

	label := f.Title
	if label == "" {
		label = "（没名字）"
	}
	row := ui.Row(c).Key(favRowKey(g, f.ID)).Grow(1).Radius(tok.R8).AlignItems(ui.Center)
	if collapsed {
		row = row.Height(sideIconCell).Justify(ui.Center).Label(label)
	} else {
		row = row.Gap(favCoverGap).Padding(space.Px6, space.Px12)
	}
	// 🔴 这一行也得能进键盘焦点（票 #453）。
	row = sideFocusable(row)
	switch {
	case cur:
		row = row.Background(p.Surface2)
	case row.Hovered():
		row = row.Background(p.Hover)
	}
	row = row.Transition(motion.Fade)

	row.Children(func() {
		s.viewFavoriteCover(c, f)
		if collapsed {
			return
		}
		fg := p.Text2
		if cur || row.Hovered() {
			fg = p.Text
		}
		ui.Column(c).Grow(1).AlignItems(ui.Start).Children(func() {
			// 🔴 单行 + 省略号：名字一长这一行会长高，把下面的行整体顶下去。
			ui.Text(c, label).FontSize(scale.Font(c, scale.Size13)).FontWeight(500).
				TextColor(fg).Grow(1).MaxLines(1).Ellipsis("…")
			if f.Invalid {
				// 「已失效」和列表页上那枚同一个。
				list.InvalidTag(c)
			}
			ui.Text(c, favCountText(f)).FontSize(scale.Font(c, scale.Size11)).
				TextColor(p.Text3).Grow(1).MaxLines(1)
		})
	})
	if collapsed {
		s.sideItemTip(c, row, label)
	}

	if row.Clicked() && !f.Invalid {
		s.navigate(collectionPathOf(f))
	}

	// 右键菜单挂在**行**上，不是列上：MyGo 取的是最里层那个带菜单的元素。
	row.ContextMenu(func(m *ui.Menu) {
		s.favoriteMenu(g, f, m)
	})
}

// viewFavoriteCover 是那一行左边那枚方形封面（32，圆角 4，surface-2 占位底）。
//
// 封面三层兜底：① 接口给的封面（第一首）；② 它没有或拉不下来就沿用夹里下一首有效的
// 封面；③ 一首都没有就画默认封面。不给它发请求：封面地址是列表接口本来就给的。
func (s *Shell) viewFavoriteCover(c *ui.Context, f app.Favorite) {
	cover := f.Cover
	if cover == "" || s.images.Get(imgcache.CoverURL(cover, imgcache.ParamsSquare)).Err != nil {
		// ⚠️ **换算走 `app.CollectionRefOf`，别在这里自己拼**。
		cover = s.app.FolderFallbackCover(app.CollectionRefOf(f))
	}
	if cover == "" {
		defaultCover(c, favCoverSize)
		return
	}
	imgcache.ViewRadius(c, s.images, imgcache.CoverURL(cover, imgcache.ParamsSquare), favCoverSize, favCoverSize, tok.R4)
}

// favCountText 是「N 首」那一句。
//
// ⚠️ **这个数是 B 站上次统计的**（`media_count`），不是实时的——用户在 B 站
// 网页上刚加了一首，这边要等下一次刷新才数得对。**那不是 bug**（票 #270 的
// 验收标准），所以这里不写「大约」也不加波浪号。
func favCountText(f app.Favorite) string {
	return strconv.Itoa(f.MediaCount) + " 首"
}

// favRowKey 是一行的 key。用「组 + ID」：两组可能有同一个 ID（B 站那边
// 自己收藏的和自己创建的收藏夹完全可能是同一个），只给 ID 会让两组撞 key
// ——Tester 里撞 key 直接 panic（`ui/diagnostics.go`）。
func favRowKey(g app.FavoriteGroup, id int64) string {
	return "fav-row:" + strconv.Itoa(int(g)) + ":" + strconv.FormatInt(id, 10)
}

// favoriteMenu 建这一行的系统原生右键菜单。
//
// 用 `ContextMenu` 而不是自绘浮层：验收要的是「系统原生样式、方向键能导航」，
// 只有系统菜单免费给这两样。
func (s *Shell) favoriteMenu(g app.FavoriteGroup, f app.Favorite, m *ui.Menu) {
	for _, item := range favMenuItems(g) {
		// ⚠️ 每一项都要 Item 一遍：菜单是这趟全部建出来的，只有 Chosen
		// 的那一项为 true。少建一项，那一项就点不到。
		it := m.Item(item.label)
		// ⚠️ **失效行的「播放」「添加到播放列表」是灰的**：那一行是墓碑，
		// 内容已经不在了——摆得出来按不下去是撒谎，而「取消收藏」「隐藏」
		// 照样能用（那两件事动的是我这边的记录，不是它）。
		if f.Invalid && (item.key == "play" || item.key == "add-to-playlist") {
			it.Disabled(true)
		}
		if it.Chosen() {
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
