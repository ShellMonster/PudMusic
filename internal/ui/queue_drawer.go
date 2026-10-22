// 播放列表抽屉：Biu 的 `components/music-playlist-drawer`。行高 64
// （`index.tsx:12` 的 `RowHeight`，也是 `scrollToPlayItem` 那个
// `targetIndex * 64` 的出处）、按判重标识去重（同一处的 `uniqBy`，键在
// `app.Item.Identity` 上）、行菜单五项（`menu.tsx` 的 `getMenus`，理由见
// `queueMenuItems`）。
//
// 它浮在主布局**外面**：`ui.Overlay` + `Absolute`（MyGo 文档
// `docs/ui/overlays.md` 里有一个抽屉的例子），不在 `viewMain` 那个 `Row` 里
// 留一列——留在那儿抽屉一开窗口就窄一块，页面被挤着走，那不是抽屉。
package ui

import (
	"strconv"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/shadow"
	"github.com/egoist/mygo/ui"
)

// 尺寸照 Biu：行高 64（`RowHeight`）、宽 384（HeroUI Drawer 的 size="sm"）、封面
// 48（`h-12 w-12`）、行内边距 8（`p-2`，48+8+8 正好 64）、顶上那条 52。
const (
	queueRowHeight    = 64
	queueDrawerWidth  = 384
	queueCoverSize    = 48
	queuePadY         = 8
	queuePadX         = 8
	queueHeaderHeight = 52
)

// queueRow 是抽屉里画出来的一行。
//
// `at` 是它在**队列**里的下标：去重之后行号和下标不再是一回事（同一个键的第一条
// 留在队列第 3 位、在抽屉里是第 1 行），点一行 / 放一首 / 删一行用的都是 `at`。
type queueRow struct {
	item app.Item
	at   int
	// key 是这一行的身份（铁律四）。见 queueRowKey。
	key string
}

// queueRowKey 是这一行的 key。
//
// 🔴 铁律四：**行的身份是这一首歌的判重标识，不是行号**。默认的「父 ID +
// 兄弟序号」在前面插一行 / 删掉一行之后会让后面所有行的状态整体错配——
// 抽屉上最直接的表现是「正在播的那首」高亮到了旁边那一行。
//
// 认不出是哪一首的项（Identity 返回空串，见它的注释）退回用队列下标当身份：
// **不能给它们同一个空串**，那会让 MyGo 在 Tester 里当场 panic（重复的 key），
// 真窗口里则是两行串在一处、删一行连着删掉两首。
//
// ⚠️ 退回的那一支前缀用 `!` 而不是别的：`Identity` 造出来的键一律是
// `<local|mv|audio>:<值>`，冒号是它的一部分。所以「`queue-row:` + 标识」和
// 「`queue-row!` + 下标」这两串**永远不会相等**——写成 `queue-row:#3` 的话，
// 一首标识正好是 `#3` 的歌就会和队里第三行那个认不出的项撞上（虽然今天造不出
// 那种标识，但这里是唯一一处拿两样用户数据拼一个键的地方，别留这个口子）。
func queueRowKey(id string, at int) string {
	if id != "" {
		return "queue-row:" + id
	}
	return "queue-row!" + strconv.Itoa(at)
}

// queueRowsFor 是抽屉这一帧要画的那几行：队列按判重标识去重之后的样子。
//
// **保留第一次出现的**（`uniqBy` 的语义）：那一行在队列里的位置（`at`）也还是
// 第一次入队那个，播放条和「下一首」走的是队列，抽屉跟着走同一个才不至于各说各话。
func queueRowsFor(items []app.Item) []queueRow {
	rows := make([]queueRow, 0, len(items))
	seen := make(map[string]bool, len(items))
	for i, it := range items {
		id := it.Identity()
		if id != "" {
			if seen[id] {
				continue
			}
			seen[id] = true
		}
		rows = append(rows, queueRow{item: it, at: i, key: queueRowKey(id, i)})
	}
	return rows
}

// viewQueueDrawer 是播放列表抽屉。没开就一个字都不建。
//
// 🔴 铁律二：**这一层不调 SetTheme**——它在 `viewMain` 的同一个 build pass 里建
// （`viewMain` 第一行就 `applyFrameTheme` 过），读到的 `c.Theme()` 已经是用户选
// 的那一档。跟 `viewToasts` 一样。
//
// ⚠️ 外面那层 `Guard` 是 `viewMain` 加的（铁律三）：抽屉一崩，崩掉的是**整个窗口**。
func (s *Shell) viewQueueDrawer(c *ui.Context) {
	if !s.drawerOpen {
		return
	}
	// 记一笔「抽屉建了一遍」——给「一帧建了几遍」那条测试数（见 builds.go）。
	//
	// ⚠️ 这一句数的是**两个调用方**：满屏播放器开着的时候抽屉由满屏那一支
	// 建（`viewFullPlayer` 末尾那一句），而 `viewMain` 必须让开。两处都建
	// 的话这里会是 2——而画面上看着没差。
	s.builds.drawer++
	t := c.Theme()
	s.queueRows = queueRowsFor(s.app.Queue().Items())

	ui.Overlay(c, func() {
		// ⚠️ 底边留出底下那一条的高度（Biu 的 `data-[placement=right]:mb-22` 就是
		// 88）。这一层是吃点击的（见下面 back.Clicked 那段），不留出这一条播放条
		// 就**整个被它盖住**：抽屉开着时按播放、上一首、下一首都按不动。
		//
		// 满屏开着的时候底下那一条是满屏的底栏（进度 + 控制键，92 高，见
		// `fullControlsHeight`），不是播放条——让出的高度要跟着它，否则进度条
		// 上面那十几像素会被蒙版盖住、点一下就变成「关抽屉」。
		bottomInset := float32(playbarHeight)
		if s.full.open {
			bottomInset = fullControlsHeight
		}
		back := ui.Row(c).Key("queue-backdrop").
			Absolute().Left(0).Top(0).Right(0).Bottom(bottomInset).
			Justify(ui.End).AlignItems(ui.Stretch).
			// 没有底色：Biu 的 `backdrop="transparent"`，底下那一层照旧看得见。
			Modal()
		back.Children(func() {
			// ⚠️ **那一圈影是上游给的**：Biu 那一枚是 HeroUI 的
			// `<Drawer radius="md" shadow="md">`（`components/music-playlist-drawer/
			// index.tsx:116`），而 HeroUI 的 Drawer 是拿 Modal 画的，那一档影落在
			// **面板本身**上。抽屉是浮在页面右边的一整条，没有影的话它和底下那一页
			// 是「同一块底上的两块颜色」——深色下尤其如此（上游的卡片底和背景几乎
			// 同色，**靠阴影分层**，见 DESIGN.md §4）。
			//
			// ⚠️ **面板底色取 `t.Background`（= 上游的 `bg-background`）**：上游
			// 那一枚 Drawer 没有自己的底色类，走的是 HeroUI Drawer 的默认
			// `bg-background`，而上游主题把 `background` 这个色定义成了**页面底色**
			// （`src/common/constants/theme.ts`：深色 #17181a、浅色 #ffffff）。所以
			// 那一块和底下那一页是**同一个颜色**，分开的正是上面那圈影。
			//
			// 之前这里取 `t.Surface`（深色下 #27272a）——那正是 DESIGN.md §四说的
			// 「比背景亮一级的板」。
			panel := shadow.HeroMedium(ui.Column(c).Key("queue-drawer").
				Width(queueDrawerWidth).FillHeight().
				Background(t.Background).BorderStartWidth(1).BorderColor(t.Border), t)
			panel.Role(ui.RoleDialog).Label("播放列表").
				Children(func() {
					s.viewQueueHeader(c, t)
					s.viewQueueBody(c, t)
				})
		})

		// 关抽屉的两条路：点底下那一层、按 Esc。少一条的表现是「我明明点了外面，
		// 它还在」，另一条少的是「按了 Esc 什么都没发生」。
		//
		// ⚠️ 用 `back.Clicked()`，**不要用 `panel.PressedOutside()`**：后者问的是
		// 「指针按在了不是这个元素的地方」，问的**不是这一层的框**——播放条上按
		// 一下也算「按在外面」，于是按播放会顺手把抽屉关了。而播放条是这一层
		// 特意让开的（Biu 的 `mb-22`）：抽屉开着时它照常用，关抽屉是另一件事。
		//
		// 副作用是底下那一层（侧栏、页面）在抽屉开着的时候点不动——Biu 的透明
		// 蒙版也是这样，它只是看不见，不是空的。这和上面那个 Modal 是同一件事。
		//
		// ⚠️ 按下 Esc 时焦点得在抽屉里才轮得到它（Modal 把焦点关进来了）。
		if back.Clicked() || back.OverlayShortcut(0, ui.KeyEscape) {
			s.drawerOpen = false
		}
	})
}

// viewQueueHeader 是顶上那一条：标题 + 定位 + 清空。两个键只在队列非空时才摆
// （Biu 的 `Boolean(pureList?.length) && …`）。
//
// 字号照上游（`music-playlist-drawer/index.tsx:131-133`）：标题是 `h3`，也就是
// `text-lg` = 18 / 400（Preflight 把标题字重重置成 inherit，不是加粗）；条数是
// 同一处的 `text-sm` = 14。原来这里是 15 / 700 和 12——标题小一号还重两档，读起来
// 像一句标语。
func (s *Shell) viewQueueHeader(c *ui.Context, t *ui.Theme) {
	rows := s.queueRows

	ui.Row(c).Key("queue-header").Height(queueHeaderHeight).FillWidth().
		PaddingY(10).PaddingX(12).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "播放列表").FontSize(scale.Font(c, 18)).TextColor(t.Text).Grow(1)
		if len(rows) == 0 {
			return
		}
		ui.Text(c, "("+strconv.Itoa(len(rows))+")").FontSize(scale.Font(c, 14)).TextColor(t.TextMuted)

		// 逐字照 Biu 那一段（`components/music-playlist-drawer/index.tsx:137-141`）：
		// 两个 16px 的 `IconButton`（`RiFocus3Line` / `RiDeleteBinLine`）各带
		// tooltip。**无障碍名字一个字没少**——原来那两个文字键念出来是「定位当前
		// 播放」「清空」，换成图标之后靠 `Label` 念的还是这两句，而它们同时也是
		// 无头 Tester 认这两个按钮的唯一凭据（`queue_drawer_test.go` 靠它点）。
		if s.withTip(c, iconButton(c, iconLocate, "定位当前播放", iconSm).
			Size(iconHit, iconHit).Padding(0, 9),
			"把队列滚到正在放的那一首").Clicked() {
			s.locateCurrent()
		}
		if s.withTip(c, iconButton(c, iconDelete, "清空", iconSm).
			Size(iconHit, iconHit).Padding(0, 9), "清空播放列表").Clicked() {
			s.app.ClearQueue()
		}
	})
	// 头下面那条线用框架的 `ui.Divider`：1 DIP、无圆角、无内边距，取的就是
	// `t.Border`（`mygo/ui/widgets.go` 的 `coreDivider`）——和原来手搓的这一行
	// 逐字一样，只是换了个出处。它看自己在 `Row` 还是 `Column` 里决定给宽度还是
	// 高度，这里外面是 `Column`，所以画出来还是横着那一条。
	ui.Divider(c).Key("queue-header-rule")
}

// viewQueueBody 是抽屉下面那一块：有歌就是那一列，没有歌是一句空状态（分岔照
// Biu 的 `{list.length ? <VirtualList/> : <Empty/>}`）。
func (s *Shell) viewQueueBody(c *ui.Context, t *ui.Theme) {
	if len(s.queueRows) == 0 {
		// ⚠️ 那两行走 `emptyCopy`（empty.go），和七个页面的空状态同一份字号、
		// 同一份主次色。原先这里自己抄了一份 15 / 12——而抄一份就会漂开。
		ui.Column(c).Key("queue-empty").Grow(1).Center().Padding(20, 16).Children(func() {
			emptyCopy(c, queueEmpty)
		})
		return
	}
	s.viewQueueList(c, t)
}

// viewQueueList 是抽屉里那一列队列。行身份是判重标识（铁律四），行高定死 64——
// 「定位当前播放」滚到的 `targetIndex * 64` 靠的就是这个定值。
func (s *Shell) viewQueueList(c *ui.Context, t *ui.Theme) {
	rows := s.queueRows
	playing := s.playingIdentity()
	// 「正在放的那一行」在**建列表之前**找出来，不要在每一行的回调里找：那是一帧
	// 里 len(rows) 次全表扫，队列长一点就是几万次判重标识的比较，而每帧都来一遍。
	cur := currentRow(rows, playing)

	// 🔴 铁律四：Key 每一帧都重装，装的是这一首自己的判重标识。
	// ⚠️ 别改成行号：那正是实测踩过的坑（前面删掉一行，后面所有行的状态整体
	// 错配，抽屉上表现为「正在播的那首」高亮到了旁边那一行）。
	s.queueList.Key = func(i int) any { return rows[i].key }
	// Label 是这一行给无障碍和「打字选中」用的名字。
	s.queueList.Label = func(i int) string { return rows[i].item.Title }

	e := ui.List(c, &s.queueList, len(rows), func(i int) {
		s.viewQueueRow(c, rows[i], i == cur, t)
	}).
		// 滚动位置写进自己的句柄，「定位当前播放」改的就是它（ScrollTo 带动画）。
		TrackScroll(&s.queueScroll).
		Grow(1).FillWidth()
	s.trackScrollbar(e)
}

// currentRow 是「正在放的那一首」在抽屉里的行号，没有是 -1。
//
// 按**判重标识**认，不按下标认（同一首排了两遍时，正在放的可能不是抽屉留下的
// 那一条）。这也是 Biu 的 `isSame(playItem, item)`。
func currentRow(rows []queueRow, playing string) int {
	if playing == "" {
		return -1
	}
	for i, r := range rows {
		if id := r.item.Identity(); id != "" && id == playing {
			return i
		}
	}
	return -1
}

// playingIdentity 是「正在放的那一首」的判重标识。没有歌在放、或者认不出是哪一首
// 时都是空串——这两个情形必须一样：认不出的项之间身份都是空串，拿它去比会让抽屉
// 一开就有一行莫名高亮。
//
// ⚠️ 判据是**队列的当前项 **加**「确实有歌在放」**（`Snapshot().HasTrack`），
// 不是光看队列的当前项。光看当前项的话：删掉正在放的那首、而下一首打不开时
// （`App.playNextAfterCurrent` 会停掉播放，但队列的当前项已经落到下一首了），抽屉
// 会把**一首放不出来、也没在放**的歌涂成正在放，播放条上却是空的——界面说的和它做
// 的不一致，「定位当前播放」还会把用户滚到那一行中间。
//
// 队列的**下标**不动是有意的：停在那儿之后按「下一首」能从它后面接着走，点那一行
// 也能重试（`App.PlayAt`）。丢的是「它在放」这件事，不是「我们走到哪儿了」。
func (s *Shell) playingIdentity() string {
	if !s.app.Snapshot().HasTrack {
		return ""
	}
	cur, ok := s.app.Queue().Current()
	if !ok {
		return ""
	}
	return cur.Identity()
}

// viewQueueRow 是抽屉里的一行：封面 + 歌名 + 来源 + 右端那个「⋯」。
//
// ⚠️ 行高定在 64（Biu 的 RowHeight），**内容按封面 48 + 上下各 8 摆进去**。
// 别在这里改内边距：改了的话行高和 `targetIndex*64` 就不一样了，「定位当前
// 播放」会停在差几像素的地方——不难看出来，但用户每次按都差一点。
func (s *Shell) viewQueueRow(c *ui.Context, row queueRow, playing bool, t *ui.Theme) {
	rowEl := ui.Column(c).Key(row.key).Grow(1).Height(queueRowHeight).
		Padding(queuePadY, queuePadX).Radius(6).AlignItems(ui.Stretch)
	// 悬停要读在 Children 之前：指针是在布局那一步才知道落在哪儿的。
	hovered := rowEl.Hovered()
	switch {
	case playing:
		// Biu 的 `variant="flat" color="primary"`。
		rowEl = rowEl.Background(t.Accent)
	case hovered:
		// 悬停要有反应：这一行点得动（跳过去放它），没有反馈用户不知道它是能点的。
		rowEl = rowEl.Background(t.SurfaceHover)
	}

	rowEl.Children(func() {
		ui.Row(c).Grow(1).FillHeight().Gap(8).AlignItems(ui.Center).Children(func() {
			s.viewQueueCover(c, row.item, playing, hovered)
			// 歌名和来源两行（Biu 的 `space-y-1`，这里 Gap(2)）竖着堆，居中放在
			// 48 那张封面旁边。
			//
			// 字号照上游 `music-playlist-drawer/list-item.tsx:68-80`：歌名
			// `text-base` = 16、来源 `text-sm` = 14——也就是 DESIGN.md 里「列表」
			// 那一档（歌名 16 / UP 14）。这里原来是 15 / 12，副题比列表行的 UP 名
			// 还小一档，同一列两种刻度。
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, row.item.Title).FontSize(scale.Font(c, 16)).
					TextColor(playlistTextColor(t, playing)).SingleLine()
				ui.Text(c, itemSubtitle(row.item)).FontSize(scale.Font(c, 14)).
					TextColor(playlistSubColor(t, playing)).SingleLine()
			})
			// Biu 的那一颗「⋯」是个 DropdownTrigger。这里给它挂系统的菜单
			// （Element.Menu），点它和右键这一行是同一份菜单——`queueMenu` 只
			// 写一遍，两条路不可能点出不同的东西来。
			//
			// 扁平：调研点名的描边药丸里就有这一颗（报告 §1.1 的 F1，证据那一段
			// 点名的是 `queue_drawer.go` 里「行尾的 ⋯」）。旁边的行本身没有边框，
			// 唯独行尾飘一个描边药丸，混在一列无边框的行里更显眼。
			//
			// 图标是上游那一枚（`music-list-item/operation.tsx:22` 的
			// `RiMore2Line`，16px）：这一颗画出来就是三个点，而「⋯」那几个
			// 字符跟着系统字体走——同一个意思在两台机器上是两个不同的宽高。
			flatIconButton(c, iconMore, "这一首的操作", 16).
				Menu(func(m *ui.Menu) { s.queueMenu(m, row) })
		})
	})

	// 点这一行 = 从这儿听（Biu 的 `onPress` → `playListItem`）。
	//
	// ⚠️ **点正在放的那一行不重头播**（见 app.PlayAt）：Biu 的 playListItem 开头
	// 就是一句 `if (playId === id) return`。重头播是播放条上「播放」那个键的活。
	if rowEl.Clicked() {
		if err := s.app.PlayAt(row.at); err != nil {
			s.addToast("放不了这一首："+err.Error(), notice.Danger)
		}
	}
	// 右键菜单挂在**行**上（不是外面那个 List，也不是 ⋯ 那个按钮）：MyGo 取的是
	// 最里层那个带菜单的元素（`ui/menu.go` 的 menuTarget），挂错了整列共用一份，
	// 而且 ⋯ 上按右键会开成一份一模一样的、看不出是哪一行的菜单。
	rowEl.ContextMenu(func(m *ui.Menu) { s.queueMenu(m, row) })
}

// viewQueueCover 是行里那张封面。四种状态（没有地址 / 载入中 / 就绪 / 加载失败）
// 都由 imgcache.View 画，这里不重复它的判断。悬停时上面浮一枚 20 的播放三角
// （Biu 那层蒙版 + `RiPlayFill`）；正在放的那首不浮——它已经在放了。
func (s *Shell) viewQueueCover(c *ui.Context, item app.Item, playing, hovered bool) {
	ui.Row(c).Width(queueCoverSize).FillHeight().Children(func() {
		imgcache.View(c, s.images, item.Cover, queueCoverSize, queueCoverSize)
		if playing || !hovered {
			return
		}
		// 半透明黑底 + 一个播放三角，就是 Biu 那层 `bg-[rgba(0,0,0,0.35)]`。
		//
		// 三角是**图标**不是字符（上游 `list-item.tsx:49` 的 `<RiPlayFill
		// size={20}/>`）。原来这里写的是「▶」14 号字：字形跟着系统字体走，同一个
		// 意思在两台机器上是两个不同的宽高，而且 14 号字在 48 的封面上太小。
		// 图标不走 `scale.Font`：上游的 `size={20}` 是固定像素，图标不跟系统字号
		// 缩放（见 widgets/scale 的文件头）。
		//
		// PassThrough：**不让这一层吃掉点击**。它盖在封面上面，不放行的话
		// 点它就不是「点这一行」了（那一行跳过去放歌正是要点封面附近的）。
		ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Radius(6).
			PassThrough().Center().Background(ui.RGBA(0, 0, 0, 0.35)).Children(func() {
			ui.Icon(c, icon(iconPlayFill)).FontSize(20).
				TextColor(ui.RGBA(255, 255, 255, 0.92))
		})
	})
}

// playlistTextColor / playlistSubColor 是正在放的那一行里两行字的颜色。
//
// 正在放的那行是主色底（见上面），字就得是主色上的反色，不然浅色主题下是一行
// 白底浅字——元素摆得好好的，屏幕上什么也看不见（同 viewToast 里那条注释）。
func playlistTextColor(t *ui.Theme, playing bool) ui.Color {
	if playing {
		return t.AccentText
	}
	return t.Text
}

func playlistSubColor(t *ui.Theme, playing bool) ui.Color {
	if playing {
		return t.AccentText
	}
	return t.TextMuted
}

// itemSubtitle 是歌名下面那一行。Biu 摆的是 UP 主名，我们这一层还没有那个字段，
// 而拿类型去冒充它是编一句话——用户会以为那是个人名。所以这里摆的是能从这一项
// **真的读出来**的来源。
func itemSubtitle(item app.Item) string {
	switch item.Identity() {
	case "":
		return "来源还没接上"
	}
	switch {
	case item.Source == "local" || item.Type == "local":
		return "本地音乐"
	case item.Type == "audio":
		return "B 站音乐"
	}
	return "B 站视频"
}

// locateCurrent 是顶上那个「定位当前播放」。
//
// 滚到 `targetIndex * 64`（Biu 的 `scrollToPlayItem` 那个乘法）：列表定高 64，
// 第 i 行的顶边就在 i*64 处，所以偏移是**算出来的**，这也是行高必须定死的原因。
//
// ⚠️ `ScrollState.ScrollTo(..., ui.Smooth)`，**别换成 `ListState.ScrollTo`**——
// 那个是一跳到位（`mygo/ui/list.go`），按下去「啪」地一下就过去了，而验收要的是
// 「平滑滚过去」。末尾几行会按 MaxY 钳住，和 Biu 那个 `Math.min(targetTop, maxTop)`
// 同一件事，这里不自己算一遍。
//
// 两种「滚不了」都要说话（Biu 的两个 addToast）：默默不动的话，用户分不清是按钮
// 坏了还是自己记错了。
func (s *Shell) locateCurrent() {
	// ⚠️ **先看有没有歌在放，再看队列的当前项**（理由同 playingIdentity）：只有队列
	// 那一项的话，接歌失败停下来之后它还指着下一首，按这一下会把用户滚到一首
	// 放不出来、也没在放的歌上面去——而屏幕上播放条是空的。
	if !s.app.Snapshot().HasTrack {
		s.addToast("当前没有正在播放的歌曲", notice.Warning)
		return
	}
	cur, ok := s.app.Queue().Current()
	if !ok {
		s.addToast("当前没有正在播放的歌曲", notice.Warning)
		return
	}
	id := cur.Identity()
	if id == "" {
		s.addToast("当前这一首认不出来，没法定位", notice.Warning)
		return
	}
	idx := -1
	for i, r := range s.queueRows {
		if r.item.Identity() == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.addToast("播放列表里没有当前播放的歌曲", notice.Warning)
		return
	}
	// 末尾那几行：目标偏移会超出内容，框架会把 glide 的目标钳在 MaxY 上
	// （`mygo/ui/scroll.go` 的 glide），和 Biu 那个 `Math.min(targetTop, maxTop)`
	// 同一件事，这里不自己算一遍。
	s.queueScroll.ScrollTo(0, float32(idx)*queueRowHeight, ui.Smooth)
}

// queueEmpty 是抽屉里没有歌时那一句。
//
// 标题用共享的「暂无内容」（和 `internal/ui/empty.go` 里那几条一样：上游的
// `<Empty />` 没有专属文案，落到的就是共享默认那句），Hint 写的是**用户能做
// 的下一步**，不是「这个功能还没做」。
var queueEmpty = emptyState{
	Title: "暂无内容",
	Hint:  "点歌右边的「加入队列」，或者用播放条上的「加歌」放几个文件进来",
}

// ---- 那一行的菜单（五项） ----

// queueMenuItem 是行菜单里的一项。
type queueMenuItem struct {
	key   string
	label string
}

// queueMenuItems 是这一行的菜单：Biu `music-playlist-drawer/menu.tsx` 的
// `getMenus` 那一套，一项不多一项不少：
//
//	收藏 / 下载音频 / 下载视频 / 在 B 站打开 / 从列表删除
//
// Biu 对本地项把前四项藏起来（`getMenus({isLogin, isLocal})` 里的 `hidden`）。
// **这里不藏**：今天唯一把项放进队列的入口是 `enqueuePicked`（本地文件），藏起来的
// 话普通用户看到的行菜单**永远只有「从列表删除」一项**，五项一条都见不到。摆出来
// 按不动的那两项（见 `queueAction`）各自说清为什么，不是一句笼统的「还没接」。
//
// 第四项的字按这一项的来处换（本地项写「在访达中显示」），判断和内容列表那一套
// 菜单共用一份（`list.OpenLabel`）。
func queueMenuItems(item app.Item) []queueMenuItem {
	return []queueMenuItem{
		{key: "favorite", label: "收藏"},
		{key: "download-audio", label: "下载音频"},
		{key: "download-video", label: "下载视频"},
		{key: "bililink", label: list.OpenLabel(item)},
		{key: "del", label: "从列表删除"},
	}
}

// queueMenu 建这一行的菜单。
//
// ⚠️ **每一项都要 Item 一遍**：菜单是这趟全部建出来的，只有 Chosen 的那一项为
// true。少建一项，那一项就永远点不到（和 `sidebar_favorites.go` 的 favoriteMenu
// 同一个道理）。
//
// ⚠️ row 什么时候是真的：菜单被点中的那一帧，框架会用**新的一帧重跑一遍**每一行
// 的 row 回调（`ui/menu.go` 里 `rt.consumed = true` 那一句），闭包里的 row 因此
// 总是用户按下去那一刻的那一行。行的身份是判重标识、框架按标识重新对位，所以
// 哪怕「上一首 / 下一首」在这中间挪了 cur，拿到的仍然是用户按的那一行。
func (s *Shell) queueMenu(m *ui.Menu, row queueRow) {
	for _, item := range queueMenuItems(row.item) {
		if m.Item(item.label).Chosen() {
			s.queueAction(row, item)
			return
		}
	}
}

// queueAction 是菜单里那一项被选中之后做的事。
//
// **五项全都真的在做那件事**，这一层只有「点了 → 调服务 → 把结果说给用户」：
//
//	收藏        App.FavoriteSong —— 接口调用、把 bvid 换成 avid、挑哪一格、判重复，
//	            全在 `internal/service/bili/favorite.go` 里（这里一句 B 站的知识都没有）
//	下载音频    App.Download
//	下载视频    App.DownloadVideo —— 取视频流 + 交织成一个 mp4 都在下载服务里
//	打开来处    list.Actions.Open
//	从列表删除  App.RemoveSongFromList
//
// 收藏要问 B 站四到五次，所以走**回调**（见 App.FavoriteSong）；下载是「加进队列、
// 立刻返回」。
func (s *Shell) queueAction(row queueRow, item queueMenuItem) {
	switch item.key {
	case "favorite":
		s.favoriteQueueRow(row)
	case "download-audio":
		s.startDownload(row.item)
	case "download-video":
		s.startDownloadVideo(row.item)
	case "bililink":
		// 内容列表那套 `list.Actions.Open`：本地项先判访达、再判 B 站地址。
		// 自己照着再写一遍的话，抽屉和列表会对同一项给出两种说法。
		list.AppActions(s.app, s.addToast).Open(row.item)
	case "del":
		s.removeQueueRow(row)
	}
}

// removeQueueRow 是「从列表删除」。
//
// ⚠️ 删的是**全部同身份项**，不是这一行对应的那一份：这一行是按判重标识画的，同一
// 首排了两遍只画一行，只删一份的话它画完还在（顶上的计数也不变），用户会以为按了
// 没反应。Biu 的 `play-list.ts` 里 `del` 用的就是 `isSame` 删全部。
//
// 认不出是哪一项的（`Identity` 是空串）走按位置删那一份——它没有身份可循，而这一行
// 在队列里的位置是准的。
func (s *Shell) removeQueueRow(row queueRow) {
	if row.item.Identity() == "" {
		s.sayQueueErr(s.app.RemoveAt(row.at))
		return
	}
	if _, err := s.app.RemoveSongFromList(row.item); err != nil {
		s.addToast(err.Error(), notice.Danger)
	}
}

// startDownload 是「下载音频」：加进下载队列（`app.Download`，下载那一票的实现）。
func (s *Shell) startDownload(item app.Item) {
	if !s.canDownload(item) {
		s.addToast("这一首已经在你磁盘上了，没有东西可下载", notice.Warning)
		return
	}
	if _, err := s.app.Download(item); err != nil {
		s.addToast("下不了这一首："+err.Error(), notice.Danger)
		return
	}
	s.addToast("已加进下载队列", notice.Success)
}

// startDownloadVideo 是「下载视频」：取视频流、交织成一个 mp4，落在下载目录里。
//
// **画质那一档传 `bili.VideoQualityUnknown`**（让 B 站按它的默认给）：设置页还没接
// 「视频下哪一档」，这里写死一个高画质只会让用户在流量不多的网络下下几百兆，
// 却不知道为什么这么大。
func (s *Shell) startDownloadVideo(item app.Item) {
	if !s.canDownload(item) {
		s.addToast("本地文件没有视频可下载", notice.Warning)
		return
	}
	if _, err := s.app.DownloadVideo(item, bili.VideoQualityUnknown); err != nil {
		s.addToast("下不了这个视频："+err.Error(), notice.Danger)
		return
	}
	s.addToast("已加进下载队列", notice.Success)
}

// canDownload 报这一项有没有东西可下载。
//
// 本地文件没有：B 站上还没有它，而「下载」下的是 B 站上还没下过的那一段。
func (s *Shell) canDownload(item app.Item) bool {
	return !isLocalItem(item) && item.Bvid != ""
}

// isLocalItem 报这一项是不是本地文件。
//
// 判断在 `app.Item.IsLocal` 那边——**别在这里再写一份**：这个项目里已经因为
// 「两处各写一份本地文件长什么样」付过一次代价（少判一个字段的话，手写的
// Item 会被当成本地文件，而症状是「在线的歌在访达里显示」）。
func isLocalItem(item app.Item) bool { return item.IsLocal() }

// favoriteQueueRow 是「收藏」。
//
// 结果走**回调**（`App.FavoriteSong` 要问 B 站四到五次，不该在界面线程上等），回调
// 里弹的那句已经由 app 层用 `bili.Friendly` 说好了——这一层不判断成功失败，它只知道
// 「成了 / 没成，理由是什么」。
func (s *Shell) favoriteQueueRow(row queueRow) {
	if isLocalItem(row.item) || row.item.Bvid == "" {
		s.addToast("这一首是本地文件，B 站上没有对应的东西", notice.Warning)
		return
	}
	s.app.FavoriteSong(row.item, func(folder bili.FavFolder, err error) {
		if err != nil {
			s.addToast("收藏不了："+err.Error(), notice.Danger)
			return
		}
		name := folder.Title
		if name == "" {
			name = "收藏夹"
		}
		s.addToast("已收藏到「"+name+"」", notice.Success)
	})
}

// sayQueueErr 把队列那一层的话原样说给用户听，**不加工任何前缀**：话是 app 那边
// 写的（`RemoveAt` 说的是「已经删掉了，但接不上下一首：…」，不是「删不掉」——
// 删是真的删掉了）。界面再套一句就是在当着用户的面说假话。
func (s *Shell) sayQueueErr(err error) {
	if err != nil {
		s.addToast(err.Error(), notice.Danger)
	}
}
