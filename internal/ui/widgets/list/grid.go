// 网格视图：一行几首歌，列数按宽度分档换。
//
// 列数**不是**连续算出来的，是四个断点分档换，照 Biu 的
// `src/components/virtual-grid-page-list/index.tsx` 里那份 `columns`：
//
//	>= 1536 → 6 列	>= 1280 → 5 列
//	>= 1024 → 4 列	>=  768 → 3 列	比 768 还窄 → 2 列
//
// ⚠️ 别拿 `ui.GridView` 顶掉这一层：它是「按最小格宽连续算列数」
// （`mygo/ui/gridview.go`：`cols = (width+gap)/(minWidth+gap)`），拖窗口的
// 时候列数一跳一跳地变。Biu 要的是分档：拖过 768 才从 2 列变 3 列，拖到一半
// 停在 2 列和 3 列中间时**还是 3 列**，不闪。
//
// ⚠️ **换列数时滚动锚点要保住**（见 keepAnchor）：6 列的第 120 行是 2 列的第
// 360 行，同一首歌在屏幕上的位置完全不同。滚动量不动的话，用户盯着的那首会
// 整个跳走，拖一下窗口边就找不到刚才在看哪一首了。
//
// ⚠️ 一格画崩了要单独兜住，兜法和列表那一行一样（shared.go 的 guardFailed），
// 页级的 `internal/ui.Guard` 是页级的，两者不重复。
package list

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/egoist/mygo/ui"
)

const (
	// gridGap 是格子之间的间距。Biu 的 `virtual-grid-page-list` 里是
	// `ROW_GAP = 16`（CSS grid 的 `gap`，横竖一样）。
	gridGap = 16

	// gridCoverH 是封面那一块的高。Biu 的 music-card 是 `height={140}`，
	// 宽度跟着格子走（`width="100%"`）——所以封面不是方的，列数变多它就变窄，
	// 高度一直不变。这里照它：格子的高只由歌名折几行决定。
	gridCoverH = 140
)

// breakpoint 是「到这个宽度就换成这么多列」。从宽到窄排，碰到的第一个算数。
//
// ⚠️ 这四个数和它们的对应关系是从 Biu 抄的，一个字都不能改：用户在同一个
// 窗口宽度下看到的列数要和 Biu 一样，改一位就整体对不上。
type breakpoint struct {
	width float32
	cols  int
}

// gridBreakpoints 是 Biu 那四档。Biu 的表是 1536/1280/1024/768 → 6/5/4/3，
// 比 768 窄还有一档 2 列（`if (!width) return 2` 是「还没量出宽度」的兜底，
// 和最窄那一档同一个值）。
var gridBreakpoints = []breakpoint{
	{1536, 6},
	{1280, 5},
	{1024, 4},
	{768, 3},
}

// gridMinCols 是最窄那一档的列数，也是宽度还没量出来时用的那个。
const gridMinCols = 2

// columnsFor 是容器有 w 那么宽时排几列。
//
// 判据是**容器**的宽，不是窗口的：Biu 量的是网格自己那个 wrapper
// （`useSize(wrapperRef)`），页面左边还有侧栏，窗口宽不等于网格宽。MyGo 侧
// 量的是这个列表元素自己的盒子（见 View 末尾那句 Bounds）。
func columnsFor(w float32) int {
	for _, b := range gridBreakpoints {
		if w >= b.width {
			return b.cols
		}
	}
	return gridMinCols
}

// cellWidth 是一列有多宽：容器减去列间的缝，平分。
//
// 和 CSS grid 的 `repeat(n, minmax(0, 1fr))` 同一个算法（`ui.Grid` 的
// `Columns` 也是这么分的），所以算出来的数正好是这一格实际拿到的宽，封面按
// 它画不会差那半像素。
func cellWidth(w float32, cols int) float32 {
	if w <= 0 || cols <= 1 {
		return w
	}
	return (w - float32(cols-1)*gridGap) / float32(cols)
}

// Grid 是同一个列表的另一种摆法：网格。数据、封面缓存、操作和 `List` 一模
// 一样，摆在页面上顶掉 `List` 就是网格页。
//
// **建一次，存进页面自己的状态里，每一帧调 View**：`ui.ListState` 记着滚动
// 位置，新建一个等于每帧跳回顶上（理由同 `List`）。
type Grid struct {
	songs []Music
	state ui.ListState

	// sel 是选中的那一**首**，-1 是没选。帧里的点击由每一格自己写进来。
	//
	// ⚠️ 是「第几首」不是「第几格」：格子是布局，插入一首歌之后同一首歌的
	// 格子位置就变了（铁律四）。
	sel int

	// cache 是封面缓存，整个窗口共用一个，和 `List` 一样由壳传进来。
	cache *imgcache.Cache

	// acts 是这个网格会做的操作，见 actions.go。
	acts Actions

	// failed 是画崩了的**格**、failedRow 是画崩了的**行**（都按那一首的 ID 记），
	// 崩过的不再重试。理由见 shared.go。
	//
	// ⚠️ 分成两张表是必须的：行的键是这一行**第一首**的 ID，和格子的键会撞上
	// （第一格崩了记的就是第一首的 ID）。共用一张表的话，下一帧行先短路成
	// 「这一行画不出来」，同一行里另外几首好的也跟着没了——按格兜就白兜了。
	failed, failedRow map[string]bool

	// anchorID 是滚动锚点守着的那**一首歌**的 ID。
	//
	// ⚠️ 必须是歌，不是「第几行」：换列数时行要重新分，歌在第几行是**算出来**
	// 的（歌的下标除以列数），一取整就丢。存行号的话每跨一档就丢一次，而人拖
	// 窗口边正是一帧一帧跨过去的——6→5→4→3→2 丢四次，用户盯着的那首就整个掉
	// 出画面了（实测拖 1600→500 漂 6 首，一步跳到同一个宽度则漂 0 首）。
	anchorID string

	// width 是这个网格**上一帧**量出来的容器宽，用来判断点。
	width float32

	// cols 是上一帧的列数，keepAnchor 靠它把行号换算成歌的下标。
	cols int

	// started 是「已经画过一帧」。头一帧没有上一帧的行高和滚动位置可依据，
	// 头几帧也可能还没有，都不能拿来做锚点。
	started bool
}

// NewGrid 造一个网格。
//
// ⚠️ cache 不能是 nil：和 `New` 一样，imgcache.View 一拿到非空的封面地址就在
// 它上面取状态，nil 会让**带封面的每一格**画不出来（每格 recover 一次并记一行
// 日志）。
func NewGrid(cache *imgcache.Cache, acts Actions) *Grid {
	return &Grid{cache: cache, acts: acts, sel: -1}
}

// SetSongs 换掉网格里的歌。
//
// 🔴 和 `List.SetSongs` 一样，选中项跟着**那一首**走：原来选中的那首还在新的一
// 份里，选的还是它（哪怕它在前面插了一首之后挪到了别的格子）。那一首没了就
// 没有选中了——不留在原地指着下一首。
//
// 框架替列表做这件事（`ListState.Key` + `Selected`），网格的选中项是自己的
// 字段（框架那一层是**行**的选中，一个行里有好几首，按不了格子），所以这里要
// 自己按 ID 重新对一次位。
//
// ⚠️ 只能由界面线程调（就是跑 view 的那个线程）：它改的正是 view 每帧读的
// 那份状态。
func (g *Grid) SetSongs(songs []Music) {
	// ⚠️ 先记下**旧的那一份**里选中的是哪一首，再换。顺序反了的话
	// selectedID() 读的是新切片同一个下标上的歌（插在头上时它是另一首），
	// 重新对位就对到错的歌上去了。
	was := g.selectedID()
	g.songs = songs
	g.sel = indexOfID(songs, was)
	warnDuplicateIDs(songs)
}

// selectedID 是此刻选中的那一首的 ID，没选中是空串。
func (g *Grid) selectedID() string {
	if g.sel < 0 || g.sel >= len(g.songs) {
		return ""
	}
	return g.songs[g.sel].ID
}

// indexOfID 是 songs 里 ID 为 id 的那一首的下标，没有是 -1。
func indexOfID(songs []Music, id string) int {
	if id == "" {
		return -1
	}
	for i := range songs {
		if songs[i].ID == id {
			return i
		}
	}
	return -1
}

// Selected 返回选中的那首，没有选中是 ok=false。
//
// 返回的是**歌**不是格子序号，和 `List.Selected` 一个道理：格子是布局。
func (g *Grid) Selected() (Music, bool) {
	if g.sel < 0 || g.sel >= len(g.songs) {
		return Music{}, false
	}
	return g.songs[g.sel], true
}

// ScrollToItem 把网格滚到第 item 首，align 是 ui.Start（顶上）、ui.Center
// （中间）或 ui.End（底下）。
//
// 收的是**第几首**不是第几行（`List.ScrollTo` 收的是行号）：列数会变，同一个
// 行号一会儿是这一首一会儿是那一首，拿行号来滚，滚过去的是哪一首说不准。
//
// 顺带把滚动锚点记成这一首：调用方刚说过「我要看这一首」，之后用户拖窗口边、
// 列数换了，守着的就是它，不用等下一次换列数时猜「他此刻盯着哪首」。
func (g *Grid) ScrollToItem(item int, align ui.Align) {
	// ⚠️ 用局部变量，不要就地改 g.cols：那个字段是「上一帧的列数」，
	// keepAnchor 靠它把行号换算成第几首，这里顺手改掉会算错下一帧的锚点。
	cols := g.cols
	if cols <= 0 {
		cols = gridMinCols // 还没画过一帧，按最窄那档算
	}
	if item >= 0 && item < len(g.songs) {
		g.anchorID = g.songs[item].ID
	}
	g.state.ScrollTo(item/cols, align)
}

// View 画这个网格。页面在每一帧开头调一次。
//
// 摆法和 `List.View` 一样：自己占 Grow(1)，调用方要给它一块地方。
//
// 🔴 断点看的是**容器**的宽（g.width，这个列表元素自己的盒子，见下面那句
// Bounds），不是窗口的：Biu 量的是网格自己那个 wrapper，页面左边有侧栏时两者
// 差着一截，差一档就是差一列。真机上窗口 1400 + 侧栏 220 → 容器 1180 → 4 列；
// 按窗口宽算是 5 列。TestColumnsFollowTheContainersWidthNotTheWindows 钉的就是
// 这一条（别的用例里网格摆在根上，窗口就是容器，分不出这两种做法）。
func (g *Grid) View(c *ui.Context) {
	w := g.width
	if w <= 0 {
		// 头一帧还没量出容器（Bounds 量的是上一帧的盒子），先拿窗口的宽顶着。
		// 那一帧的列数和封面宽是**估的**（窗口比容器宽，估出来偏大），下一帧
		// 量到真的就换过去了。反过来（第一帧量到 0 就当最窄）的话会先闪一下
		// 两列——宁可宽一帧，也不要窄一帧。
		w, _ = c.Size()
	}

	cols := columnsFor(w)
	// ⚠️ 必须在换掉 g.cols **之前**算锚点：keepAnchor 那一段里的行号是用 g.cols
	// （旧的列数）换算的。
	g.keepAnchor(cols)

	rows := (len(g.songs) + cols - 1) / cols

	// 🔴 铁律四：行身份是这一行**第一首的 ID**，不是行号。
	//
	// 换列数是布局变了、数据没变（铁律四的另一面）：换完之后同一个行号装的是
	// 别的歌，用行号当身份，选中状态和滚动位置就会整体错配。行身份跟着这一行
	// 里的歌走，框架按 ID 重新对位。
	g.state.Key = func(row int) any { return g.songs[row*cols].ID }
	g.state.Label = func(row int) string { return g.songs[row*cols].Title }

	cell := cellWidth(w, cols)
	e := ui.List(c, &g.state, rows).
		Grow(1).
		FillWidth().
		// 没有歌时网格是空的，Children 里那一支就是它的空状态。
		Children(func() {
			if len(g.songs) == 0 {
				ui.Text(c, "这里还没有歌").FontSize(13).
					TextColor(c.Theme().TextMuted).Padding(16)
			}
		}).
		Rows(func(r ui.ListRow) { g.viewRow(r, cols, cell) })

	g.cols, g.started = cols, true

	// 量这一帧的容器宽，下一帧用。`Bounds` 读的是**上一帧**量好的盒子
	//（`ui/api_gen.go` 里 Bounds 的注释原话），所以窗口改宽之后列数晚一帧换
	//——和 Biu 那边 ResizeObserver 改 state 再重画是同一个节奏。
	if bw := e.Bounds().W; bw > 0 {
		g.width = bw
	}
}

// keepAnchor 是「换列数时滚动锚点保持」的那一段（验收第 2 条）。
//
// 换列数是**布局变了，数据没变**：同一首歌在 6 列时是第 120 行，在 2 列时是
// 第 360 行。什么都不做的话滚动量不变，同一个滚动量在两种布局下指的是完全不同的
// 歌——用户盯着的那首会整个跳走，拖一下窗口边就丢了位置。
//
// 🔴 **锚点记的是那一首歌（anchorID），不是「第几行」**：滚动单位是行，而歌在
// 第几行是**算出来**的（歌的下标除以列数），一取整就丢。记行号的话每跨一档丢一次，
// 而人拖窗口边正是一帧一帧跨过去的——6→5→4→3→2 连丢四次，误差累加起来用户盯着
// 的那首就整个掉出画面（实测：拖 1600→500 漂 6 首，一步跳到同样宽度漂 0 首）。
// 记歌就没有这个问题：跨多少档都是同一首，它最多换到顶上那一行的另一格。
//
// 什么时候换锚：锚点守着的歌**还在画面上就不换**（换列数不该把用户盯着的那首
// 换成别的歌）；它滚出画面了（用户自己滚走了）才换人守，换的是这一刻顶上那一行
// 的第一首。
//
// ⚠️ 为什么不等框架的 `ListState.Key` 自己找回：它按 key 找回锚点时**只在
// 前后各 1000 行里线性找**（`mygo/ui/list.go` 的 listSearch）。一千首的歌单
// 从 2 列换到 6 列，行号差着几千，够不着——那一次它会把锚点留在原地（或者
// 落在越界被夹回来的地方），用户看到的是「跳到别的位置去了」，比跳回顶部还糟。
// 所以这里自己算，框架那套留给「在头上插一行」那种小挪动。
func (g *Grid) keepAnchor(cols int) {
	if !g.started || cols == g.cols || len(g.songs) == 0 {
		return
	}
	first, last := g.state.Visible()
	if last < first {
		// 还没量出顶上那一行（头几帧，或者这一帧刚被换掉）。没有可记的锚点，
		// 硬滚反而会跳。
		return
	}
	// ⚠️ 这一段里的行号都除以 g.cols——**旧**的列数：歌的下标是它在旧布局里的
	// 位置，换算回去也用旧的。View 是在 keepAnchor 之后才把 g.cols 换成新值的。
	j := indexOfID(g.songs, g.anchorID)
	if j < 0 || j/g.cols < first || j/g.cols > last {
		if j = first * g.cols; j >= len(g.songs) {
			return // 这一行是空的（歌没了），没有可守的
		}
		g.anchorID = g.songs[j].ID
	}
	g.state.ScrollTo(j/cols, ui.Start)
}

// viewRow 画一行：一行 cols 首歌。
//
// ⚠️ 行里 panic 会一路穿出 Cocoa 事件循环把整个进程带走，所以这里兜两层：
//
//   - **每一格**自己兜一层（viewCellGuarded）：一张封面坏掉只废掉那一格，隔壁
//     那几格照画。按行兜的话网格一行有六格，一张坏封面要带走五首好的。
//   - 行再兜一层：格子以外的地方（`ui.Grid` 自己那一层）崩了，也不至于把整页
//     带走。
//
// 两层都是 shared.go 的 guardFailed，崩过的按 ID 记下来，下一帧不再重试。
func (g *Grid) viewRow(r ui.ListRow, cols int, cell float32) {
	c, first := r.Context, r.Index*cols
	m := g.songs[first]

	// 崩过的那一行**不再画**（和 List.viewRow 一个写法）：不然它每一帧崩一次、
	// 每帧打一整份 debug.Stack，日志被刷爆、帧率拖垮，比「这一行画不出来」
	// 糟得多。
	//
	// ⚠️ 记在 failedRow 里，不是 failed（格子的那张表）：两张表的键都是那一首
	// 的 ID，第一格崩了记的就是第一首的 ID，共用一张表的话下一帧行先短路成
	// 「这一行画不出来」，同行里另外几首好的也跟着没了。
	if g.failedRow[m.ID] {
		ui.Text(c, "这一行画不出来").FontSize(13).TextColor(c.Theme().TextMuted).
			Padding(cardPadY, cardPadX).MaxLines(1)
		return
	}
	guardFailed(&g.failedRow, "网格行", m.ID, func() {
		// ⚠️ Stretch：格子要撑满这一行的高，选中那一格的底色才是一整块，
		// 而不是只有歌名那一小块。
		ui.Grid(c).AlignSelf(ui.Stretch).Columns(cols).Gap(gridGap).Children(func() {
			for k := range cols {
				i := first + k
				if i >= len(g.songs) {
					// 最后一行不满：剩下的格子空着（CSS grid 也是这么摆的，
					// 空格子不占位也不画边框）。
					return
				}
				g.viewCellGuarded(c, i, cell)
			}
		})
	})
}

// viewCellGuarded 画一格，画崩了只废掉这一格。
func (g *Grid) viewCellGuarded(c *ui.Context, i int, cell float32) {
	m := g.songs[i]
	if g.failed[m.ID] {
		ui.Text(c, "这一格画不出来").FontSize(13).TextColor(c.Theme().TextMuted).
			Padding(cardPadY, cardPadX).MaxLines(1)
		return
	}
	guardFailed(&g.failed, "网格格", m.ID, func() { g.viewCell(c, i, cell) })
}

// viewCell 画一格：一首歌的封面、歌名和 UP 名/时长，外加这一格的手势——单击
// 选中、双击播放、右键出菜单（和列表那一行的三个手势一一对应）。
//
// 结构照 Biu 的 `components/music-card/index.tsx`：封面一块，下面 `mt-2` 是
// 歌名，再下面一行是 UP 名（左边）和时长（右边）。
//
// ⚠️ **没有播放量**：Biu 的封面右下角那一条渐变上写着播放量，那个数要 B 站
// 接口才有（本地音乐没有）。等接了 B 站再加，别提前画一列空白。
func (g *Grid) viewCell(c *ui.Context, i int, cell float32) {
	t := c.Theme()
	m := g.songs[i]
	chosen := i == g.sel
	playing := isPlaying(g.acts, m.Item)

	// 选中那一格：主色底 + 主色上的反色字，和列表选中行是同一套颜色
	//（`ui/table.go` 的 node.Selected 就是这么画的）。深色主题下换成别的
	// 颜色会看不清。
	//
	// ⚠️ 「正在放」是**第二档**、不是并进去的第二种底色：它只把歌名染成主色
	// 并挂一句「正在播放」。换底色就和「选中」分不出来了——而这两个问的是两件
	// 不同的事（见 card.go 里同一段）。
	titleColor, metaColor := t.Text, t.TextMuted
	switch {
	case chosen:
		titleColor, metaColor = t.AccentText, t.AccentText.Alpha(0.75)
	case playing:
		titleColor = t.Accent
	}

	box := ui.Column(c).Radius(t.Radius).
		// 右键菜单挂在**这一格**上，和列表挂在行上是同一件事：MyGo 取的是
		// 最里层那个带菜单的元素（`ui/menu.go` 的 menuTarget），挂到网格外面
		// 的话整页共用一份菜单。
		ContextMenu(func(m *ui.Menu) { buildSongMenu(m, g.songs, g.acts, i, g.playFrom) }).
		// 点一下选中这一首。Biu 的卡片单击是直接播，这里只选中：桌面端单击
		// 就换队列太容易误触，播放走双击和右键菜单。
		OnClick(func() { g.sel = i })
	if chosen {
		box = box.Background(t.Accent)
	}

	// 双击这一格 = 从这一首往后播，和列表双击那一行是一个意思
	//（Biu 的 `onDoubleClick`）。两个视图别一个能双击播放一个不能。
	//
	// ⚠️ 这是**在 view 里查**出来的，不是回调：`DoubleClicked` 报的是「这一帧
	// 这一格被双击了」，查了它就置上 `rt.consumed`，框架会**把这一帧重建一遍**
	// （`runtime.go` 的 runFrame 原话：「An event handled while building may
	// change what was built before it: build again」），所以队列换完这一帧画出来
	// 就是换完的样子。一个双击只报一次——`endPass` 每遍画完就把这一遍的输入忘掉。
	if box.DoubleClicked() {
		g.playFrom(i)
	}

	box.Children(func() {
		// ① 封面。四态由 imgcache.View 画（空地址画空框，不画「失败」）。
		imgcache.View(c, g.cache, m.Cover, cell, gridCoverH)

		// ② 歌名 + UP 名/时长。
		//
		// ⚠️ 别给这几块设固定高度（Height/SingleLine）：行高是量出来的，歌名
		// 折行时那一行就高一点（card.go 文件开头写明了为什么）。行高写死了
		// 变高行就没人走了。
		ui.Column(c).Grow(1).AlignItems(ui.Start).Gap(textGap).PaddingY(cardGap).
			Children(func() {
				ui.Text(c, m.Title).FontSize(14).LineHeight(titleLeading).
					TextColor(titleColor).MaxLines(titleLines).Ellipsis("…")
				ui.Row(c).Gap(cardGap).Children(func() {
					if m.Owner != "" {
						ui.Text(c, m.Owner).Grow(1).FontSize(12).LineHeight(ownerLeading).
							TextColor(metaColor).MaxLines(1).Ellipsis("…")
					}
					// 正在放的那一格：时长换成一句「正在播放」。
					//
					// ⚠️ **换掉时长而不是加在后面**：一格就那么宽，两样都画必然把
					// 时长挤成「0:」——而用户读的是「正在放」不是那半截时长。
					if playing {
						// 标签带上歌名，理由见 card.go 那一处。
						ui.Text(c, playingMark).Label(m.Title + " " + playingMark).
							FontSize(11).TextColor(t.Accent).MaxLines(1)
						return
					}
					// 时长没有就不画（Biu 的 `durationText` 为空时那一块
					// 整个不渲染）。
					if s := formatDuration(m.Duration); s != "" {
						ui.Text(c, s).FontSize(12).TextColor(metaColor).MaxLines(1)
					}
				})
			})
	})
}

// playFrom 从第 i 首往后接进队列并开始播。菜单的「播放」和双击这一格都用它。
func (g *Grid) playFrom(i int) { playFrom(g.songs, g.acts, i) }
