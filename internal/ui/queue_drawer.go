// 播放列表抽屉：Biu 的 `components/music-playlist-drawer` + 顶上那个开抽屉的
// 按钮（`components/open-playlist-drawer-button`，Biu 把它放在播放条右边）。
//
// 三件事，各对一处上游：
//
//   - **行高 64**：`music-playlist-drawer/index.tsx:12` 的 `const RowHeight = 64`。
//     「定位当前播放」滚到的 `targetIndex * 64` 就是这一行的出处（同一份文件里
//     的 `scrollToPlayItem`）。
//   - **按去重标识去重**：同一个文件里的 `uniqBy`，键是 `local:{id}` /
//     `mv:{bvid}` / `audio:{sid}`。判重标识在 `app.Item.Identity` 上（见
//     internal/app/queue.go），不在这里再拼一遍。
//   - **行菜单 5 项**：`music-playlist-drawer/menu.tsx` 的 `getMenus`。
//
// ## 为什么它浮在主布局外面
//
// Biu 那边是右侧浮出来的 Drawer，底下那一层页面照旧看得见。我们也走
// `ui.Overlay` + `Absolute`（MyGo 文档 `docs/ui/overlays.md` 里就有一个抽屉的
// 例子），**不**在 `viewMain` 那个 `Row` 里给它留一列：留在那儿的话抽屉一开
// 窗口就窄一块，页面被挤着走，那不是抽屉。
package ui

import (
	"strconv"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// 抽屉的尺寸，数字照 Biu：
//
//	行高 64     music-playlist-drawer/index.tsx:12 的 RowHeight
//	宽 384     HeroUI Drawer 的 size="sm"（w-[384px]），和那一行
//	           `h-12 w-12` 的 48 封面、`p-2` 的 8 内边距配得上（48+8+8 = 64）
const (
	queueRowHeight   = 64
	queueDrawerWidth = 384

	// queueCoverSize 是行里那张封面。Biu 是 `h-12 w-12`。
	queueCoverSize = 48

	// queuePadY / queuePadX 是行的上下和左右内边距，Biu 的 `p-2`。
	queuePadY = 8
	queuePadX = 8

	// queueHeaderHeight 是顶上那一条多高。Biu 是 `px-4 py-3` 加一行字（12+12+24
	// = 48），这里留到 52 让两个按钮有地方站。
	queueHeaderHeight = 52
)

// queueRow 是抽屉里画出来的一行。
//
// `at` 是它在**队列**里的下标：去重之后行号和下标不再是一回事（同一个 `local:`
// 键的第一条留在队列第 3 位，在抽屉里就是第 1 行），点一行、放一首、清掉一行
// 用的都是 `at`，拿行号去调队列就是删错/放错那一首。
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
// **保留第一次出现的**（`uniqBy` 的语义）：同一首在队列里排了两遍时，用户最先
// 看见的那一行就是他要的那一行，而它在队列里的位置（`at`）仍然是第一次入队
// 那个——播放条和「下一首」走的是队列，抽屉跟着走同一个才不至于各说各话。
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
// 🔴 铁律二：**这一层不调 SetTheme**。它在 `viewMain` 的同一个 build pass 里
// 建（`viewMain` 第一行就 `applyFrameTheme` 过，`ui.Overlay` 也是当场建），读到
// 的 `c.Theme()` 已经是用户选的那一档。跟 `viewToasts` 一样。
//
// ⚠️ 外面那层 `Guard` 是 `viewMain` 加的（铁律三）：抽屉一崩，崩掉的是**整个
// 窗口**——它是浮在所有东西上面的一层，一空就空一大块，而出错的地方（队列的
// 下标、菜单的项数）都在页面上。
func (s *Shell) viewQueueDrawer(c *ui.Context) {
	if !s.drawerOpen {
		return
	}
	t := c.Theme()
	s.queueRows = queueRowsFor(s.app.Queue().Items())

	ui.Overlay(c, func() {
		// ⚠️ 底边留出播放条的高度（Biu 的 `data-[placement=right]:mb-22` 就是
		// 88）。这一层是吃点击的（见下面 back.Clicked 那段），不留出这一条播放条
		// 就**整个被它盖住**：抽屉开着时按播放、上一首、下一首都按不动。
		back := ui.Row(c).Key("queue-backdrop").
			Absolute().Left(0).Top(0).Right(0).Bottom(playbarHeight).
			Justify(ui.End).AlignItems(ui.Stretch).
			// 没有底色：Biu 的 `backdrop="transparent"`，底下那一层照旧看得见。
			Modal()
		back.Children(func() {
			ui.Column(c).Key("queue-drawer").Width(queueDrawerWidth).FillHeight().
				Background(t.Surface).BorderStartWidth(1).BorderColor(t.Border).
				Role(ui.RoleDialog).Label("播放列表").
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

// viewQueueHeader 是顶上那一条：标题 + 定位 + 清空。
//
// 两个键只在队列非空时才摆（Biu 的 `Boolean(pureList?.length) && …`）：空队列
// 上摆一个按下去只会说「没有歌」的按钮，不如不摆。
func (s *Shell) viewQueueHeader(c *ui.Context, t *ui.Theme) {
	rows := s.queueRows

	ui.Row(c).Key("queue-header").Height(queueHeaderHeight).FillWidth().
		PaddingY(10).PaddingX(12).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "播放列表").FontSize(15).Bold().TextColor(t.Text).Grow(1)
		if len(rows) == 0 {
			return
		}
		ui.Text(c, "("+strconv.Itoa(len(rows))+")").FontSize(12).TextColor(t.TextMuted)

		if ui.Button(c, "定位当前播放").FontSize(12).Clicked() {
			s.locateCurrent()
		}
		if ui.Button(c, "清空").FontSize(12).Clicked() {
			s.app.ClearQueue()
		}
	})
	ui.Row(c).Key("queue-header-rule").FillWidth().Height(1).Background(t.Border)
}

// viewQueueBody 是抽屉下面那一块：有歌就是那一列，没有歌是一句空状态。
//
// 两者的分岔是 Biu 那样写的（`{list.length ? <VirtualList/> : <Empty/>}`）：
// 空列表也建一列的话，画出来的是一片什么都没有，窗口再大也不会自己长出字来。
func (s *Shell) viewQueueBody(c *ui.Context, t *ui.Theme) {
	if len(s.queueRows) == 0 {
		ui.Column(c).Key("queue-empty").Grow(1).Center().Padding(20, 16).Children(func() {
			ui.Text(c, queueEmpty.Title).FontSize(15).TextColor(t.Text)
			ui.Text(c, queueEmpty.Hint).FontSize(12).TextColor(t.TextMuted)
		})
		return
	}
	s.viewQueueList(c, t)
}

// viewQueueList 是抽屉里那一列队列。
//
// 行身份是判重标识（铁律四），行高定死 64——「定位当前播放」滚到的
// `targetIndex * 64` 靠的就是这个定值：行高一变，那个乘法算出来的就不是这一行
// 的顶边了。
func (s *Shell) viewQueueList(c *ui.Context, t *ui.Theme) {
	rows := s.queueRows
	playing := playingIdentity(s.app.Queue().Current())
	// 「正在放的那一行」在**建列表之前**找出来，不要在每一行的回调里找：那是一帧
	// 里 len(rows) 次全表扫，队列长一点就是几万次判重标识的比较，而每帧都来一遍。
	cur := currentRow(rows, playing)

	// 🔴 铁律四：Key 每一帧都重装，装的是这一首自己的判重标识。
	// ⚠️ 别改成行号：那正是实测踩过的坑（前面删掉一行，后面所有行的状态整体
	// 错配，抽屉上表现为「正在播的那首」高亮到了旁边那一行）。
	s.queueList.Key = func(i int) any { return rows[i].key }
	// Label 是这一行给无障碍和「打字选中」用的名字。
	s.queueList.Label = func(i int) string { return rows[i].item.Title }

	ui.List(c, &s.queueList, len(rows), func(i int) {
		s.viewQueueRow(c, rows[i], i == cur, t)
	}).
		// 滚动位置写进自己的句柄，「定位当前播放」改的就是它（ScrollTo 带动画）。
		TrackScroll(&s.queueScroll).
		Grow(1).FillWidth()
}

// currentRow 是「正在放的那一首」在抽屉里的行号，没有是 -1。
//
// 按**判重标识**认，不按下标认：队列里同一首排了两遍时，正在放的可能不是抽屉
// 留下的那一条（留下的是第一次出现的那条）。按标识认出来的是同一首歌——这正是
// 用户看到「这一行在放」的意思，也正是 Biu 的 `isSame(playItem, item)`。
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

// playingIdentity 是正在放的那一首的判重标识，没有在放（或认不出是哪一首）是空串。
//
// **空串必须和「没有在放」是一回事**：认不出的项之间身份都是空串，拿它去比会
// 让抽屉一开就有一行莫名高亮。
func playingIdentity(cur app.Item, ok bool) string {
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
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, row.item.Title).FontSize(15).
					TextColor(playlistTextColor(t, playing)).SingleLine()
				ui.Text(c, itemSubtitle(row.item)).FontSize(12).
					TextColor(playlistSubColor(t, playing)).SingleLine()
			})
			// Biu 的那一颗「⋯」是个 DropdownTrigger。这里给它挂系统的菜单
			// （Element.Menu），点它和右键这一行是同一份菜单——`queueMenu` 只
			// 写一遍，两条路不可能点出不同的东西来。
			ui.Button(c, "⋯").FontSize(13).
				Menu(func(m *ui.Menu) { s.queueMenu(m, row) })
		})
	})

	// 点这一行 = 从这儿听（Biu 的 `onPress` → `playListItem`）。
	//
	// ⚠️ **点正在放的那一行不重头播**（见 app.PlayAt）：Biu 的 playListItem 开头
	// 就是一句 `if (playId === id) return`。重头播是播放条上「播放」那个键的活。
	if rowEl.Clicked() {
		if err := s.app.PlayAt(row.at); err != nil {
			s.addToast("放不了这一首：" + err.Error())
		}
	}
	// 右键菜单挂在**行**上（不是外面那个 List，也不是 ⋯ 那个按钮）：MyGo 取的是
	// 最里层那个带菜单的元素（`ui/menu.go` 的 menuTarget），挂错了整列共用一份，
	// 而且 ⋯ 上按右键会开成一份一模一样的、看不出是哪一行的菜单。
	rowEl.ContextMenu(func(m *ui.Menu) { s.queueMenu(m, row) })
}

// viewQueueCover 是行里那张封面。
//
// 四种状态（没有地址 / 载入中 / 就绪 / 加载失败）都由 imgcache.View 画，这里不
// 重复它的判断。悬停时上面浮一个「▶」（Biu 的 `group-hover:opacity-100` 那层
// 蒙版 + 播放图标）：正在放的那首不浮——它已经在放了，再盖一个「可以播」的
// 提示是假的。
func (s *Shell) viewQueueCover(c *ui.Context, item app.Item, playing, hovered bool) {
	ui.Row(c).Width(queueCoverSize).FillHeight().Children(func() {
		imgcache.View(c, s.images, item.Cover, queueCoverSize, queueCoverSize)
		if playing || !hovered {
			return
		}
		// 半透明黑底 + 一个播放三角，就是 Biu 那层 `bg-[rgba(0,0,0,0.35)]`。
		//
		// PassThrough：**不让这一层吃掉点击**。它盖在封面上面，不放行的话
		// 点它就不是「点这一行」了（那一行跳过去放歌正是要点封面附近的）。
		ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Radius(6).
			PassThrough().Center().Background(ui.RGBA(0, 0, 0, 0.35)).Children(func() {
			ui.Text(c, "▶").FontSize(14).TextColor(ui.RGBA(255, 255, 255, 0.92))
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

// itemSubtitle 是歌名下面那一行。
//
// Biu 摆的是 UP 主名（本地音乐摆「本地音乐」）。我们这一层还没有 UP 主名——
// `app.Item` 上没有那个字段，而拿类型去冒充它是编一句话：用户会以为那是个人名。
// 所以这里摆的是能从这一项**真的读出来**的来源。
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
// 滚到 `targetIndex * 64`（Biu 的 `scrollToPlayItem` 逐字抄的那个乘法）：
// 列表是定高 64 的，第 i 行的顶边就在 i*64 处，所以目标偏移是**算出来的**，
// 不是量出来的——这也是行高必须定死的原因。
//
// `ScrollState.ScrollTo` 带的是 `ui.Smooth`：Biu 那边是
// `scrollTo({behavior: "smooth"})`，一并抄过来。**别换成 `ListState.ScrollTo`**
// ——那个是一跳到位（`mygo/ui/list.go` 的 ScrollTo），用户按下去会「啪」地一下
// 跳过去，而验收要的是「平滑滚过去」。
//
// 两种「滚不了」都要说话（Biu 的两个 addToast）：没在放、这一首不在抽屉里。默默
// 不动的话，用户分不清是按钮坏了还是自己记错了。
func (s *Shell) locateCurrent() {
	cur, ok := s.app.Queue().Current()
	if !ok {
		s.addToast("当前没有正在播放的歌曲")
		return
	}
	id := cur.Identity()
	if id == "" {
		s.addToast("当前这一首认不出来，没法定位")
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
		s.addToast("播放列表里没有当前播放的歌曲")
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

// queueMenuItems 是这一行的菜单，项和顺序照 Biu
// `src/components/music-playlist-drawer/menu.tsx` 的 `getMenus`。
//
// ⚠️ **本地项只摆「从列表删除」**：Biu 那边收藏 / 下载 / 在 B 站打开三项对本地项
// 都是 `hidden` 的。「在 B 站打开」本地项更加没有意义——它连来处都没有，
// 按下去只能打开一个打不开的网页。照抄它的 hidden，别自己加项。
func queueMenuItems(item app.Item) []queueMenuItem {
	if item.Source == "local" || item.Type == "local" {
		return []queueMenuItem{{key: "del", label: "从列表删除"}}
	}
	return []queueMenuItem{
		{key: "favorite", label: "收藏"},
		{key: "download-audio", label: "下载音频"},
		{key: "download-video", label: "下载视频"},
		{key: "bililink", label: "在 B 站打开"},
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
// 五项里现在真的接上的是两项：「在 B 站打开」和「从列表删除」。其余三项要 B 站
// 的收藏接口和下载任务队列，那两张票还没做（issue #15 / #29 / #31）——
// **说清楚是哪一项还没接**，而不是按了没反应。说清名字是有用的：菜单有五项，
// 只说「还没接」的话用户不知道说的是哪一项，得自己再点一遍试。
func (s *Shell) queueAction(row queueRow, item queueMenuItem) {
	switch item.key {
	case "bililink":
		// 内容列表那边的「在 B 站打开 / 在访达中显示」接的就是它（`list.Actions`），
		// 这里复用同一份：本地项先判访达、再判 B 站地址。自己照着再写一遍的话，
		// 抽屉和列表会对同一项给出两种说法。
		list.AppActions(s.app, s.addToast).Open(row.item)
	case "del":
		if err := s.app.RemoveAt(row.at); err != nil {
			s.addToast("删不掉：" + err.Error())
		}
	default:
		s.addToast("「" + item.label + "」还没接上")
	}
}
