// 侧栏里那两组收藏夹：Biu 的 SD-05~SD-09。
//
// 两组（我创建的 / 我收藏的）各有折叠、右键菜单、拖拽排序，顺序由
// `internal/app/favorites.go` 落盘；菜单里那几件能动的事（播放 / 添加到列表 /
// 删除 / 取消收藏）在 `app/sidebar_folders.go` 里——那一层认识 B 站，这一层
// 不认识。
//
// ⚠️ **两组都有数据源**：都由 `app.RefreshFavorites` 从 B 站拉下来（见
// `app/favorites_refresh.go`），而登录之后 / 开机时还会**自动**拉一次
// （`app.AutoRefreshFavorites`）——用户不该为了看见自己的收藏夹，先去头像
// 下拉里按「刷新数据」。
//
// 空的时候两组照画标题和折叠开关，**两组的空状态合成一句**（见
// `viewFavorites`），而那一句说的是**这一片**的状态：「还没拉过」/「拉失败了」
// /「真的一个都没有」三句话各说各的（见 `favEmptyText`）——它们在画面上
// 长得一样的话，用户会把「我们没拉到」当成「B 站那边确实空着」。
//
// 「新建收藏夹」建出来的那一行是立刻进侧栏的（`app.CreateFolder` 会
// `Upsert`），所以菜单里那几件事并不是只有测试走得到。
//
// ⚠️ **整个侧栏折叠起来的时候，收藏夹换成一枚图标 + 一层浮层**（见
// viewFavoritesCollapsed）：折叠态原来干脆不画收藏夹，而折叠侧栏是给屏幕小
// 的人用的——恰恰是这类用户最常去收藏夹。
//
// # 这一片的画法（票 #371）
//
// 这一片是 2026-10-24 那次界面重设计（PR #357）之后**没跟上的一处**：重设计
// 改了侧栏本身（展开 220 / 折叠 64、底色用方案的 `sidebar`、选中项底是
// `accentSoft` 淡底），而这一片还是老样子——于是它跟同一根侧栏里的菜单项放
// 在一起是两套风格。
//
// 现在这一片**整块**都按设计规范 §零 的当前风格画：
//
//   - **左缩进 16**：和侧栏的品牌 logo、侧栏分组标题（「发现」「我的音乐」）
//     同一列——这一片是侧栏里的一块，缩进到别处就像是另外贴上去的一件东西。
//   - **外层那一列（36）**：分组标题的字、**收藏夹那一枚封面**、空状态那句、
//     在飞状态那句，都起在这一列上（见 `favTextPad`）。
//   - **分组标题**：一枚 14 的箭头图标 + 12 / 500 / 次要色的组名（和侧栏分组
//     标题同一档）。
//   - **收藏夹名**：14 的正文色；选中那一行是 `accentSoft` 淡底 + 按那层底
//     现算的字（`flatCurrent`）；悬停 / 按下是 `flat.Feel`（压一层半透明的前
//     景色，和侧栏菜单项同一档）。
//   - **折叠态那一层浮层**：底 `surface1`、圆角 `radiusLg`、影 `shadow.Modal`
//     （`popupPanelCard`）。
//
// # 这一片每一行是「图 + 名称 + N 首」（票 #270）
//
// 用户 2026-10-25 给的参考图（Biu 的设计稿
// `Biu_Design/concepts/B3_queue_inspector.png` 左边「创建的歌单」那一组）上，
// 侧栏那两组收藏夹的每一行是**一枚方形封面 + 名称 + 歌曲数量**，不是纯文字。
// 照它画。
//
//   - **封面 32 见方**（圆角走主题），**和分组标题的字同一列**——参考图上
//     「创建的歌单」那几个字和下面那些封面是左对齐的。
//   - **名称 14**（#371 那一档，不动），让开封面之后起在 78。
//   - **「N 首」12 / 次要色**，在名称下面。
//   - **折叠态**：侧栏折起来时这一组**整个不画**（换成一枚图标 + 一层浮层，
//     见 viewFavoritesCollapsed），而**浮层里那几行是同一套**——浮层是同一个
//     入口的另一种展开方式，两处画得不一样的话用户会以为它们是两样东西。
//
// ⚠️ **封面和数量都是白拿的**：`created/list` 和 `collected/list` 这两个
// 接口的返回里本来就带着 `cover` 和 `media_count`（见 `app.Favorite`）——
// **不是每行另发一次请求**（那是 N 次网络往返）。
//
// ⚠️ **颜色和圆角一个都不写死**：底色走方案，文字走主题，圆角走 `t.Radius`。
package ui

import (
	"slices"
	"strconv"
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/rowkey"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/flat"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// favGroups 是侧栏上有几组收藏夹。app 那边也是两组（见 app.FavoriteGroup）。
const favGroups = 2

// 这一片的版式（票 #371）。
//
// ⚠️ **三个数是一套**：左边距 → 图标 → 缝 → 文字列。改一个，另外两个要跟着
// 重新算——而算错了的样子是「标题的字和收藏夹名的字不在一条竖线上」，那个
// 差别一眼看不出来，得靠量。
const (
	// favIndent 是这一片（两组收藏夹 + 空状态 + 在飞状态）的左缩进。
	//
	// ⚠️ **16 是侧栏那一列的位置**：品牌 logo（`viewSideBrand`）、侧栏分组标题
	// 「发现」「我的音乐」（`viewSideGroupTitle`）都起在这儿——这一片是侧栏里的
	// 一块，缩进到别的地方，就像是另外贴上去的一件东西。
	favIndent = space.Px16

	// favIconW 是折叠指示那一枚图标的边长。**14**（`iconSm` 那一档）。
	//
	// ⚠️ 别拿 18（`iconMd`）那一档：那一档是「菜单项那一枚图标」的号，而这一枚
	// 挨着的是 12 的组名——图标比字大一号，那一枚就比它旁边的字还重。
	favIconW = iconSm

	// favIconGap 是图标和组名之间的缝（6）。
	favIconGap = space.Px6

	// favTextPad 是这一片里**文字列**相对整片左沿的偏移：图标 + 缝 = 20。
	//
	// ⚠️ **分组标题的字、空状态那句、在飞状态那句，都起在这一列上**
	// （绝对位置 36）。它们不在一条竖线上的时候，这一片看着就是两段东西
	// （报告 C3 那条理由）：一句话顶格在最左边，像一句不属于任何一组的说明。
	//
	// ⚠️ **收藏夹名的字不在这儿了**（票 #270）：那一行左边多了一枚方形封面，
	// 名字得让开它（见 `favNamePad`）。**在这一列上的是封面**（见
	// `favCoverPad`）——而封面和分组标题的字左对齐，正是参考图上的样子。
	favTextPad = favIconW + favIconGap

	// favGroupGap 是两组收藏夹之间留的那一截（12）。
	//
	// ⚠️ **和侧栏分组标题上面那一截是同一个数**（`viewSideGroupTitle` 的
	// `sideGroupGapTop`）：两个分组标题之间要有一道呼吸，不然第二组的标题紧贴
	// 着上一组最后一条，看着像上一组多出来的一个条目。
	favGroupGap = space.Px12
)

// 这一片每一行的版式（票 #270）：**一枚方形封面 + 名称 + 「N 首」**。
//
// # 数是**对着参考图量**的
//
// 参考图是用户 2026-10-25 给的 Biu 设计稿
// （`Biu_Design/concepts/B3_queue_inspector.png` 左边「创建的歌单」那一组）。
// 那张稿子上的实测值：
//
//	封面 40×40，图与字之间的缝 14，名称的字约 16，「N 首」约 13，行与行之间 18。
//
// ⚠️ **那张稿子的侧栏比我们的宽**（289 对 220），字也比我们的**大一号**
// （名称 16 对 14）——所以它不能 1:1 抄。按我们的字号缩：封面 40 → 35、
// 缝 14 → 12、名称 16 → 14、「N 首」13 → 11。
//
// ⚠️ **缝从 12 收到 10**：一枚 32 的封面 + 12 的缝，这一行在 220 宽的侧栏
// 里就只剩 134 给名字了，而名字是这一行里唯一要读的东西（上游那种
// 「【长期主义】2026 年度歌单合集·最终版」有二十来个字）。
const (
	// favCoverSize 是那枚方形封面的边长。**32**（刻度上最近的一档）。
	favCoverSize = space.Px32

	// favCoverGap 是封面和右边那两行字之间的缝。**10**。
	favCoverGap = space.Px10

	// favTextGap 是名称和「N 首」之间的缝。**2**（和侧栏菜单项行内那一档
	// 一样紧——那两个字是一块，不是两段）。
	favTextGap = space.Px2

	// favCoverPad 是那枚封面的左沿。**和分组标题的字同一列**（`favTextPad`）。
	//
	// ⚠️ **参考图上「创建的歌单」那几个字和下面那些封面是左对齐的**——
	// 照它摆，这一片看着是「标题 + 它下面缩进一层的东西」，而不是两段。
	favCoverPad = favTextPad

	// favNamePad 是收藏夹名的字起在哪：封面 + 缝。
	//
	// ⚠️ **它不再和分组标题的字同列**（票 #371 那条「字起在 36」对**收藏夹名**
	// 这一项已经不成立了）：名字左边有一枚封面，它得让开。
	favNamePad = favCoverPad + favCoverSize + favCoverGap

	// favCoverX 是封面那一格的**绝对**左缘：这一片的左缩进 + 那一列的偏移。
	//
	// ⚠️ **它是给测试和文档用的**：代码里那两处（整片的左缩进、行自己的左内边
	// 距）各自写死了，凑出这个数只是为了「和某一列对齐」这件事能一眼验——
	// 而「和参照物对齐」正是这一族测试的判据（票 #371）。
	favCoverX = favIndent + favTextPad
)

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
// ⚠️ **空状态只有一句**（报告 C3）：两组各顶一句**一模一样**的话，用户读到两句
// 一样的话，得自己判断「这是说一组还是两组都没有」。所以那一句**只落在最后
// 一个空的那一组底下**——一组有内容、另一组空的时候落在空的那一组底下，
// 两组都空的时候落在下面那一组底下。
//
// ⚠️ **那一句是**这一片**收藏夹的状态，不是一组的**（见 `favEmptyText`）：
// 两组的空状态合成一句，而合成的那一句得站在最要紧的那一档上。
//
// # 这一片是**一个块**（票 #371）
//
// 外面套的那一层只做两件事：把左缩进钉在侧栏那一列（`favIndent`），以及让这
// 一片里的字都起在同一列上（`favTextPad`）。
func (s *Shell) viewFavorites(c *ui.Context, st app.Settings, collapsed bool) {
	hidden := hiddenMenuKeys(st)
	if collapsed {
		// ⚠️ 折叠态**不画这一片**（只画那一枚图标 + 浮层），所以在飞状态那句
		// 也不画——理由见 viewFavoritesRefreshing。
		s.viewFavoritesCollapsed(c, st)
		return
	}

	// 先算一遍：哪几组是空的、那一句落在哪一组底下。
	items := make([][]app.Favorite, favGroups)
	lastEmpty := -1
	loggedIn := s.app.LoggedIn()
	for g := range favGroups {
		grp := app.FavoriteGroup(g)
		items[g] = visibleFavorites(s.app.SideFavorites(grp), hidden)
		// ⚠️ **折叠着的那一组不算空**：用户自己把它折起来了，这时候在它底下
		// 摆一句「还没有收藏夹」是在说一件他刚刚自己决定不看的事。
		if favoriteFolded(st, grp) || len(items[g]) > 0 {
			continue
		}
		lastEmpty = g
	}

	// ⚠️ **Key 挂在这一层 Column 上**：它不是处理输入的控件（里面的按钮各自
	// 有自己的 key），而挂在会处理输入的元素上会 panic（`ui/element.go:658`）。
	//
	// ⚠️ **整片上面留 12**（`favGroupGap`）：这一片是侧栏的**第三段**（前两段是
	// 「发现」和「我的音乐」），而侧栏的分组标题上方留的就是 12——不留这一截，
	// 第一组的标题就**紧贴着上面那一条**（实测只有 5.9 的缝，而组内是 7.9），
	// 这一片看着像上一段的延续，而不是自己的一段。
	ui.Column(c).Key("fav-area").
		Margin(favGroupGap, space.None, space.None, space.None).
		Padding(space.None, space.None, space.None, favIndent).
		Children(func() {
			s.viewFavoritesRefreshing(c)
			for g := range favGroups {
				text := ""
				if g == lastEmpty {
					text = s.favEmptyText(loggedIn)
				}
				// ⚠️ **组与组之间要分开**（`favGroupGap`）：两组是**两伙**收藏夹
				// （自己建的 / 别人建的），而组内那一条缝只有 2——不额外留一截的
				// 话，第二组的标题紧贴着第一组最后一条，看着像第一组多出来的一个
				// 条目。
				top := float32(space.None)
				if g > 0 {
					top = favGroupGap
				}
				s.viewFavoriteGroup(c, st, app.FavoriteGroup(g), items[g], text, top)
			}
		})
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

// favEmptyLine 是侧栏里那一句空状态。
//
// ⚠️ **它左边缩进和分组标题对齐**：它原来顶格在侧栏最左边（x=0），而那时的
// 分组标题在 x=43 / x=65（**两个还不一样**——标题那一枚是 Grow(1) 的按钮，
// 框架的按钮把内容居中，于是「我创建的」那组右边多一个「+」，标题就往左挪
// 22 像素）。一句话顶格在最左边，看着像侧栏底部的一句独立说明，而不属于
// 任何一组（报告 C3）。
//
// 现在这一句和这一片里其他的字**都在同一列**（`favTextPad`，票 #371）：分组
// 标题的**字**（标题左边那一枚箭头图标在 16）、收藏夹名的字，都在这一列。
//
// ⚠️ **对齐的是「标题那一块」的字**，不是那枚图标：这一句是替下面那些**收藏
// 夹名**说话的（名字也起在 `favTextPad` 上），而跟图标同列的话，这一句会比
// 下面那些名字更靠左，看着像是另一个层级的东西。
func (s *Shell) viewFavoritesEmptyLine(c *ui.Context, text string) {
	t := c.Theme()
	ic, label, run := s.favEmptyAction(s.app.LoggedIn())
	// ⚠️ **不再是一句孤零零的字**：它和下面的收藏夹行是同一个版式——左边一枚方形
	// 图标块（和封面同大同位置），右边一句话 + 一个能点的下一步（去登录 / 重试 /
	// 新建收藏夹）。一句只会说「没有」的字，没给用户下一步。
	ui.Row(c).Key("fav-empty").Gap(favCoverGap).AlignItems(ui.Center).
		Padding(space.Px6, space.Px8, space.Px6, favCoverPad).Children(func() {
		ui.Box(c).Size(favCoverSize, favCoverSize).Radius(t.Radius).
			Background(t.Background.Mix(t.Text, 0.05)).Border(1, t.Border).Center().Children(func() {
			ui.Icon(c, icon(ic)).FontSize(iconMd).TextColor(t.TextMuted)
		})
		ui.Column(c).Grow(1).Gap(favTextGap).AlignItems(ui.Start).Children(func() {
			ui.Text(c, text).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).Wrap()
			if label != "" && flatLink(c, label).FontSize(scale.Font(c, scale.Size12)).Clicked() {
				run()
			}
		})
	})
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

// refreshLine 是「正在刷」那一行的字。
//
// ⚠️ 界面上**不许**把这句话当成唯一的在飞证据：它会随侧栏的折叠 / 展开出现又
// 消失。真正的闸门在应用层（`app.RefreshFavorites` 的 busy），这一行只是让用户
// 看得见——「按下去没反应」是这一项最容易出的那种错。
const refreshLine = "正在刷新收藏夹…"

// viewFavoritesRefreshing 是那一行。
//
// 折叠态不画，理由同 viewLoggedOutHint：64 宽摆不下，而且整个收藏夹那一块
// 在折叠态压根不画（`viewFavoritesCollapsed` 那一支）——单独留一句悬在
// 空侧栏里，用户会以为收藏夹跑到别处去了。
//
// ⚠️ **它缩在这一片的文字列上**（`favTextPad`，票 #371）：这一句说的是「这一
// 片」，而它摆在最上面——缩进和下面那些字不一样的话，它像是一句悬在上面的
// 说明，而不是这一片的状态。
func (s *Shell) viewFavoritesRefreshing(c *ui.Context) {
	if !s.app.FavoritesRefreshing() {
		return
	}
	t := c.Theme()
	ui.Text(c, refreshLine).FontSize(scale.Font(c, scale.Size12)).
		TextColor(t.TextMuted).Padding(space.Px4, favTextPad)
}

// viewFavoriteGroup 是一组：折叠标题 + （展开时的）那一列。
//
// 折叠态只画标题，和 Biu 一样（`collection/index.tsx:432-434`）。顺带把
// 折叠态下的拖拽也一起禁掉了——列表根本没建，「禁用」是自然发生的，
// 不需要另写一条分支（SD-09 要的就是这个结果）。
//
// emptyText 是空状态那一句；**空串表示这一组底下不摆那一句**（空状态只有
// 一句，而它落在最后一个空的那一组底下，见 viewFavorites）。
//
// top 是这一组**上面**留的那一截（组与组之间要分开，见 `favGroupGap`）。
//
// ⚠️ **折叠态（整个侧栏折起来）不在这儿**：那一档走的是一枚「收藏夹」图标 +
// 一层浮层（见 viewFavoritesCollapsed）——整组收藏夹在折叠态**整个不见**的话，
// 用户会以为收藏夹没了（报告 C4）。
func (s *Shell) viewFavoriteGroup(c *ui.Context, st app.Settings, g app.FavoriteGroup,
	items []app.Favorite, emptyText string, top float32) {
	folded := favoriteFolded(st, g)

	// Key 是这一组的序号：两组各自独立，折叠/展开不会让另一组的状态串过来。
	//
	// ⚠️ **上边距用 `Margin(top, …)`**（不是 `MarginY`）：`MarginY` 设的是上下
	// 两个，而这里要的是**上面**那一截——下面那一截由这一组自己的内容决定。
	ui.Column(c).Key("fav-group:"+strconv.Itoa(int(g))).Gap(space.Px2).
		Margin(top, space.None, space.None, space.None).Children(func() {
		s.viewFavoriteHeader(c, g, folded, len(items))
		if folded {
			return
		}
		if len(items) == 0 {
			if emptyText != "" {
				s.viewFavoritesEmptyLine(c, emptyText)
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
func (s *Shell) viewFavoriteHeader(c *ui.Context, g app.FavoriteGroup, folded bool, n int) {
	ui.Row(c).Gap(space.Px2).Center().Children(func() {
		fold := s.favoriteFoldButton(c, g, folded, n).Grow(1)
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

// favoriteFoldButton 是那一组的折叠开关：一枚箭头图标 + 组名。
//
// # 箭头是**图标**，不是字符（票 #371）
//
// 原来摆的是字符「▾」「▸」。而同一个侧栏里别的东西（菜单项、图标键、折叠
// 键）都是 RemixIcon 的图——一个字符摆在那一列里，它和邻居不是一套，而且它
// 是**字**：字会跟着字号缩放、跟着字体渲染，而图标的边长是画出来的那一个。
//
// ⚠️ **朝下 = 展开着，朝上 = 折起来了**：和 `iconChevronDown` /
// `iconChevronUp` 这两枚图自己的形状对得上（展开的那一支是「可以往下收起」
// 的那一枚）。
//
// # 组名是 **12 / 500 / 次要色**（票 #371）
//
// 和侧栏的分组标题（「发现」「我的音乐」）**同一档**（`viewSideGroupTitle`）。
// 上游的分组标题是弱化的次级文字（`layout/side/collection/index.tsx:425` 一带
// 的次级文字），而菜单项和收藏夹名才是正文色——这一层要是和下面那些一样黑，
// 六个分组标题就会比六个名字还抢眼。
//
// # 无障碍名字说的是「按下去会发生什么」
//
// 「折叠 我创建的」/「展开 我创建的」。画面上只有一枚箭头和一个组名，念不出
// 它是干什么的；而读屏软件念的是这个名字（`Label`），画面上不多画一个字。
func (s *Shell) favoriteFoldButton(c *ui.Context, g app.FavoriteGroup, folded bool, n int) ui.Element {
	t := c.Theme()
	name, tip := iconChevronDown, "折叠 "+g.String()
	if folded {
		name, tip = iconChevronUp, "展开 "+g.String()
	}
	// ⚠️ **不补可见文字以外的名字**：画面上那两个字就是组名，而箭头是图标
	// （图标对辅助技术是不可见的，`mygo/ui/svg.go` 的 Icon 文档）。
	btn := ui.ButtonBase(c).Justify(ui.Start).Gap(favIconGap).Radius(t.Radius).
		Padding(space.Px2, space.None).Label(tip)
	btn.Children(func() {
		ui.Icon(c, icon(name)).FontSize(favIconW).TextColor(t.TextMuted)
		ui.Text(c, g.String()).FontSize(scale.Font(c, scale.Size12)).
			FontWeight(weightMedium).TextColor(t.TextMuted)
		// 这一组有几个：一枚小数字，贴在组名后面（折起来时也看得见里面有多少）。
		if n > 0 {
			ui.Text(c, strconv.Itoa(n)).FontSize(scale.Font(c, scale.Size11)).TextColor(t.TextMuted)
		}
	})
	// ⚠️ **悬停 / 按下是 `flat.Feel`**（压一层半透明的前景色）：和侧栏菜单项
	// 同一档（`widgets/flat` 的文件头写着为什么是「压」不是「换」——换底会把
	// 别的底整块顶掉）。
	return flat.Feel(c, btn)
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
//
// ⚠️ **当前就在某个收藏夹页上时，那一枚要亮起来**（票 #371）：和侧栏菜单项在
// 折叠态里的画法一样（`flatCurrent`——强调色淡底 + 按那层底现算的字）。折叠
// 起来之后侧栏里只剩这一枚图标，它就是「当前在哪」唯一的指示——不亮的话用户
// 在收藏夹那一页上，却看不出来自己在哪一页。
func (s *Shell) viewFavoritesCollapsed(c *ui.Context, st app.Settings) {
	anchor := flatIconCellButton(c, iconEmptyFolder, favCollapsedName, sideIconSize, sideIconCell).
		AlignSelf(ui.Center)
	if s.onCollectionPage() {
		anchor = flatCurrent(c, anchor)
	}
	s.sideItemTip(c, anchor, favCollapsedName)
	if anchor.Clicked() {
		s.favPanelOpen = !s.favPanelOpen
	}
	s.viewFavoritesPanel(c, st, anchor)
}

// onCollectionPage 报「当前就在某个收藏夹页上」。
//
// ⚠️ **认的是路径前缀**：收藏夹页的地址是 `/collection/{id}`（`collectionPath`），
// 而合集 / 系列也是同一个前缀（类型在查询参数里）。
func (s *Shell) onCollectionPage() bool {
	return strings.HasPrefix(s.router.Path(), collectionPathPrefix)
}

// viewFavoritesPanel 是折叠态那枚图标点开之后的那层浮层。
//
// ⚠️ **它走 `ui.PopoverBase`**（而不是自己拼一个浮层）：框架那一份自带
// 「点外面 / 按 Esc 关掉」，而自己拼的话得自己实现那两件事——漏了就是面板
// 关不掉（点外面也没反应）。
//
// ⚠️ **它不建在侧栏那个滚动容器里**：`PopoverBase` 的面板建在**浮层那一层**
// （`mygo/ui/base.go` 的 `popover`），所以它不会被侧栏的滚动裁掉。
//
// # 这一层是**卡片那一档**（票 #371）
//
// 底色 `surface1`、圆角 `radiusLg`、影 `shadow.Modal`——走 `popupPanelCard`
// 这一个出口（设计规范 §零「侧栏收藏夹」那一条：折叠态那层浮层是卡片那一档）。
//
// ⚠️ **别用 `popupPanel` 那一档**（底 = 页面底、圆角写死的 8、影是
// `shadow.Popover`）：那是「贴着一层底的小浮层」，而这一层是一张浮在侧栏旁边
// 的卡片——头像下拉是同一块料。
func (s *Shell) viewFavoritesPanel(c *ui.Context, st app.Settings, anchor ui.Element) {
	if !s.favPanelOpen {
		return
	}
	t := c.Theme()
	hidden := hiddenMenuKeys(st)
	ui.PopoverBase(c, anchor, &s.favPanelOpen, func(panel ui.Element) {
		popupPanelCard(s, t, panel.
			AttachTo(anchor, ui.AnchorEnd, ui.AnchorStart).MarginStart(favPanelGap).
			Width(favPanelW).Padding(space.Px4)).
			Children(func() {
				// ⚠️ **空状态也只有一句**（和侧栏那一句同一个道理，报告 C3）：
				// 两组都空的时候摆两句一模一样的话，用户得自己判断那是说一组
				// 还是两组都没有。那一句落在**最后一个空的那一组**底下。
				//
				// ⚠️ **它说的和侧栏那一句是同一句**（`favEmptyText`）：浮层是
				// 侧栏的另一个入口，两处说的话不一样的话，用户会以为收藏夹
				// 在两个地方的状态不一样。
				items := make([][]app.Favorite, favGroups)
				lastEmpty := -1
				loggedIn := s.app.LoggedIn()
				for g := range favGroups {
					grp := app.FavoriteGroup(g)
					items[g] = visibleFavorites(s.app.SideFavorites(grp), hidden)
					if len(items[g]) > 0 {
						continue
					}
					lastEmpty = g
				}
				emptyText := ""
				if lastEmpty >= 0 {
					emptyText = s.favEmptyText(loggedIn)
				}
				for g := range favGroups {
					// ⚠️ **组名是 12 / 500 / 次要色**（票 #371）：和侧栏那两枚
					// 分组标题、关注页那一列的顶上那一行，同一档。
					ui.Text(c, app.FavoriteGroup(g).String()).FontSize(scale.Font(c, scale.Size12)).
						FontWeight(weightMedium).TextColor(t.TextMuted).
						Padding(space.Px4, space.Px8)
					for _, f := range items[g] {
						s.viewFavoritesPanelRow(c, f)
					}
					if g == lastEmpty {
						ui.Text(c, emptyText).FontSize(scale.Font(c, scale.Size12)).
							TextColor(t.TextMuted).Padding(space.Px2, space.Px8)
					}
				}
			})
	})
}

// viewFavoritesPanelRow 是浮层里那一个收藏夹。
//
// # 它和侧栏里那一行是**同一套**（票 #371）
//
// 字号（14）、圆角（`t.Radius`）、悬停（`flat.Feel`）——侧栏里那一行怎么画，
// 这里就怎么画。浮层是同一个入口的另一种展开方式，两处画得不一样的话，用户
// 会以为它们是两样东西。
//
// ⚠️ **票 #270 之后连「封面 + N 首」也是同一套**：浮层里那一行同样是
// 「一枚方形封面 + 名称 + N 首」——不然两处摆着同一个收藏夹，一边一个样。
//
// ⚠️ **这里不画「当前」那一行的淡底**（侧栏里那一行有）：两处都分不出组——
// 同一个收藏夹完全可能既是自己建的、又是自己收藏的（B 站上很常见），而路由里
// 只有 id（`/collection/{id}`），没有组——于是「当前」那一格会**两行同时亮**，
// 那比不亮更糟。
//
// 而且这一层**开着的时候几乎不可能在收藏夹页上**：点一行就把它收掉
// （`s.favPanelOpen = false`），唯一的来路是先在展开态点进收藏夹页、再折起侧栏
// ——那时候浮层还没开。「当前在哪」在这一层里由**折叠侧栏上那枚图标**说
// （见 viewFavoritesCollapsed）。
func (s *Shell) viewFavoritesPanelRow(c *ui.Context, f app.Favorite) {
	t := c.Theme()
	label := f.Title
	if label == "" {
		label = "（没名字）"
	}
	row := ui.Row(c).FillWidth().Padding(space.Px4, space.Px8).Radius(t.Radius).
		Gap(favCoverGap).AlignItems(ui.Center)
	row = flat.Feel(c, row)
	// 🔴 **这一行也得能进键盘焦点**（票 #453）：折叠态的收藏夹只有这层浮层里
	// 看得见，所以「键盘走不到」在这里就是「折叠侧栏之后收藏夹整类都走不到」。
	// 理由和口径见 `sidebar.go` 的 `sideFocusable`。
	row = sideFocusable(row)
	row.Children(func() {
		s.viewFavoriteCover(c, f)
		ui.Column(c).Grow(1).Gap(favTextGap).AlignItems(ui.Start).Children(func() {
			ui.Text(c, label).FontSize(scale.Font(c, scale.FavPanelItem)).Grow(1).
				MaxLines(1).Ellipsis("…")
			ui.Text(c, favCountText(f)).FontSize(scale.Font(c, scale.Size12)).
				TextColor(t.TextMuted).Grow(1).MaxLines(1)
		})
	})
	if row.Clicked() {
		// ⚠️ **进去之后要把浮层收掉**：它开着的话会盖在收藏夹那一页上面，
		// 而用户已经到那一页了。
		s.favPanelOpen = false
		s.navigate(collectionPathOf(f))
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

// viewFavoriteRow 是侧栏里的一行。
//
// # 一行是「封面 + 名称 + N 首」（票 #270）
//
// 用户 2026-10-25 给的参考图（Biu 的设计稿）上，侧栏那两组收藏夹的每一行是
// 「一枚方形封面 + 名称 + 数量」，不是纯文字。照它画。
//
// ⚠️ **封面和数量都是白拿的**：那两个列表接口本来就给这两格
// （`created/list` 和 `collected/list` 的 `list[].cover` / `media_count`），
// 见 `app.Favorite` 的注释——**不是每行另发一次请求**。
func (s *Shell) viewFavoriteRow(c *ui.Context, g app.FavoriteGroup, f app.Favorite) {
	t := c.Theme()
	// ⚠️ **当前项要连类型一起比**：合集（21）和收藏夹（11）是两个不同的容器，
	// 而 id 各自一套——只比路径的话，在合集那一页上，同号的收藏夹也会被
	// 画成「当前」。
	cur := s.router.Path() == collectionPath(f.ID) &&
		s.router.Query(collectionTypeQuery) == collectionTypeQueryOf(f)

	// ⚠️ **左内边距是 `favCoverPad`**（票 #270）：这一行左边是一枚封面，而
	// **封面和分组标题的字同一列**（36）——参考图上「创建的歌单」那几个字和
	// 下面那些封面是左对齐的。名字让开封面之后在 78（`favNamePad`）。
	//
	// ⚠️ **右边还是 8**：那一头留白和左边那一列对称（右 8 对上侧栏右缘那条
	// 分隔线），而收藏夹名很长（上游那种「【长期主义】2026 年度歌单合集·最终
	// 版」），右边多留一点白是有用的。
	row := ui.Row(c).Key(favRowKey(g, f.ID)).Grow(1).Radius(t.Radius).
		Gap(favCoverGap).AlignItems(ui.Center).
		Padding(space.Px6, space.Px8, space.Px6, favCoverPad)
	// 🔴 **这一行也得能进键盘焦点**（票 #453）：`Clicked()` 只设 `flagClickable`
	// ——鼠标点得动，键盘走不到（报告 (b) 一节的第一个断口：「侧栏的收藏夹列表
	// 键盘完全走不到」）。理由和口径见 `sidebar.go` 的 `sideFocusable`。
	row = sideFocusable(row)
	switch {
	case cur:
		// 当前就在这一页：和侧栏菜单当前项**同一套**画法（`flatCurrent`——
		// 强调色淡底 + 按那层底现算的字，深浅两套同一个数）。
		//
		// ⚠️ **原来这里是实心主色**（`Background(t.Accent)`）：那是审计 F-02 /
		// F-06 点名的那一处——同一个屏上（关注页、动态流页）左栏是实心绿大块、
		// 侧栏是淡底，两种画法并排，而实心那一块比主操作还响。
		row = flatCurrent(c, row)
	}
	// ⚠️ **悬停 / 按下是 `flat.Feel`**（压一层半透明的前景色），和侧栏菜单项、
	// 关注页那一列同一档（票 #371）。
	//
	// ⚠️ **原来这里是「换成 `t.Surface.Mix(t.Text, 0.08)`」**：那是换底，而换底
	// 会把选中那一层的淡底整块顶掉——「当前在哪」是这一列里唯一的方向标，被
	// 一次悬停盖掉比多画一层底更坏（`widgets/flat` 的文件头写着同一件事）。
	//
	// ⚠️ **压在选中那一层上面是故意的**：压一层前景色在任何底上都成立（透
	// 明底上是变灰，淡底上是变深），而换底只在前一种底上成立。
	row = flat.Feel(c, row)

	row.Children(func() {
		s.viewFavoriteCover(c, f)
		// ⚠️ **两行字是一块**：名称在上、「N 首」在下，右对齐到同一列。
		ui.Column(c).Grow(1).Gap(favTextGap).AlignItems(ui.Start).Children(func() {
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
			ui.Text(c, label).FontSize(scale.Font(c, scale.Size14)).
				TextColor(rowTextColor(t, cur)).Grow(1).
				MaxLines(1).Ellipsis("…")
			// 「N 首」：小字、次要色。
			ui.Text(c, favCountText(f)).FontSize(scale.Font(c, scale.Size12)).
				TextColor(t.TextMuted).Grow(1).MaxLines(1)
		})
	})

	if row.Clicked() {
		s.navigate(collectionPathOf(f))
	}

	// 右键菜单挂在**行**上，不是列上：MyGo 取的是最里层那个带菜单的元素
	// （`ui/menu.go` 的 menuTarget），挂在外面的容器上整列共用一份菜单。
	row.ContextMenu(func(m *ui.Menu) {
		s.favoriteMenu(g, f, m)
	})
}

// viewFavoriteCover 是那一行左边那枚方形封面。
//
// ⚠️ **拉不到时画什么**：三档都由 `imgcache` 自己管——
//
//   - **正在加载**：一块底色；
//   - **拉到了**：图；
//   - **拉失败 / 没有地址**：一枚图片图标的占位（`widgets/imgcache` 那一档）。
//
// ⚠️ **「没有地址」不是「拉失败」**：用户一个收藏夹都没设过封面，B 站那边
// 就是没有——那是常态，画占位图就是对的（而不是发一次请求去问「它到底有没有
// 封面」，那一问的答案已经在这两个接口的返回里了）。
//
// ⚠️ **不给它发请求**：封面地址是那两个列表接口本来就给的一格（见
// `app.Favorite` 的注释），每行另发一次就是 N 次网络往返。
func (s *Shell) viewFavoriteCover(c *ui.Context, f app.Favorite) {
	// `CoverURL` 顺手把协议补上（`//i0.hdslb.com/…` → `https://…`）并拼上
	// 图床的裁剪参数——侧栏这一枚只有 32 宽，要原图是白下几十倍。
	imgcache.View(c, s.images, imgcache.CoverURL(f.Cover, imgcache.ParamsSquare),
		favCoverSize, favCoverSize)
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
