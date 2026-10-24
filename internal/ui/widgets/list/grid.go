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
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/rowkey"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/leading"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scrollbar"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// gridGap 是格子之间的间距。Biu 的 `virtual-grid-page-list` 里是
	// `ROW_GAP = 16`（CSS grid 的 `gap`，横竖一样）。
	gridGap = space.Px16

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

// ─────────────────────────────────────────────────────────────────────────────
// 出口：给「格子不是一首歌」的网格用
// ─────────────────────────────────────────────────────────────────────────────

// GridGap 是格子之间的间距。**格子画的不是歌的那一派网格也要用这一个**。
//
// 为什么不写死在自己的文件里：Biu 那个 `virtual-grid-page-list` 是**同一个**
// 组件换了个 `renderItem`（关注页塞的是用户卡），所以两边的格子间距在同一个
// 窗口宽度下必须是同一个数。这里写死一份的话，首页的 6 列和关注页的 6 列
// 摆出来的宽度会差 `cols-1` 个缝——差几像素，而那正是「两个页面对不上」的第一眼。
const GridGap = gridGap

// CellWidth 是容器有 w 那么宽、排 cols 列时**一列有多宽**。
//
// ⚠️ 和 `GridColumnsFor` 是同一件事的两个半：自己按 `repeat(n, minmax(0, 1fr))`
// 再算一遍的话，某一页算出来的封面宽度会和其余页面差半个像素，而「两个页面对不上」
// 的第一眼往往就是那半个像素。
//
// 用途：给 `Grid` 之外的网格画格子（`internal/ui` 的稍后再看 / 历史记录那一档）。
func CellWidth(w float32, cols int) float32 { return cellWidth(w, cols) }

// GridColumnsFor 是容器有 w 那么宽时排几列。
//
// ⚠️ **判据是容器的宽，不是窗口的**——页面左边还有侧栏，窗口宽不等于网格宽。
// 用它的页面要量自己那个列表元素的盒子（`Element.Bounds()` 量的是**上一帧**
// 的盒子，和 `Grid.View` 末尾那一句同一个节奏）。
//
// 用途是给 `Grid` 之外的网格用（关注页的用户卡，见 `internal/ui/follow.go`）。
// 两者必须用同一个函数，否则用户在同一窗口宽度下会看到两种列数。
func GridColumnsFor(w float32) int { return columnsFor(w) }

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

	// rowKeys 记着每一**行**的身份（**预先装箱好的那一份**）。
	//
	// ⚠️ 网格的行身份是「这一行第一首的 ID」（理由见 View），而装箱那一步
	// 是要分配的——现算的话每帧每行一次，一屏几行就是几次（票 #313）。
	//
	// ⚠️ **它自己知道「数据换了」**（见 `rowkey.Memo` 的文件头），所以这里
	// 不用记得重建。列数变了也自动跟上：换列数之后同一行第一首换了，身份
	// 对不上就重算（而重算出来的是新那一首的 ID——那正是要的）。
	rowKeys rowkey.Memo[string, any]

	// durText / countText / opText 记着三处「每帧每格算一遍、结果不变」的
	// 显示文字。理由和 `List` 那三个一模一样（见 list.go 的同名三个字段）。
	durText   rowkey.Memo[time.Duration, string]
	countText rowkey.Memo[int64, string]
	opText    rowkey.Memo[string, string]

	// ownerTip 记着每一格那句「打开 X 的空间」（票 #360）。理由和 `List` 那一个
	// 一模一样（见 list.go 的同名字段）。
	ownerTip rowkey.Memo[string, string]

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

	// hot 是上一帧画完后量到的「指针压在滚动条那一带」（见 `Hot`）。
	hot bool

	// footer 是末尾多出来的那一格的画法（转圈 / 重试），为 nil 时没有。
	//
	// 它和 `List.footer` 是同一件东西：翻页的哨兵。网格里它占**最后一行的第一格**
	// ——「一行几首」时把哨兵铺满一整行会占掉好几个格子的位置，而哨兵只需要一个
	// 能被看见、能被点的地方。
	//
	// 🔴 **它不是「翻页触发器」**（这一点量过，写错过一次）：翻页的判据是
	// `PagedList.afterFrame` 里那句「末尾那一行进没进画面」（`last < n-1` 就不动作），
	// 而网格里**最后一行本身就是歌的行**，滚到底照样命中——把网格这半边的哨兵拿掉，
	// `TestTheGridKeepsPagingWorking` 和整页那一层都照样绿。
	//
	// ⚠️ 它真正负责的是**翻页失败之后的那条出路**：失败时 `apply` 把 `blocked` 闩上，
	// 而闩住的是自动重试（不然就是「失败 → 解锁 → 下一帧看见容器没填满 → 又拉 →
	// 又失败」那个死循环），**解开它的只有哨兵上那个「点此重试」按钮**。所以网格
	// 这半边没有哨兵时：翻页照样能翻，但某一页一失败就**卡住不动**——没有转圈、
	// 没有重试、已加载的行数也不再变，而歌还在、封面还在、点得动，症状是静默的。
	// 钉住这一条的是 `TestTheGridOffersTheRetryThatTheListOffers`。
	footer func(c *ui.Context)

	// scroll 是这个网格把滚动位置写回来的那个句柄，为 nil 时不写。理由同
	// `List.scroll`：MyGo 的列表不往外说自己滚到哪儿了（回顶按钮和三重保障都要它）。
	scroll *ui.ScrollState

	// sel2 是「多选」那一摊接线，为零值时网格里一格勾选框都不画。理由和字段名
	// 全部照 `List.sel2`：改漏一边的后果是「列表里能批量删、网格里不能」。
	sel2 Selection

	// empty 是一行歌都没有时说的话，和 `List.empty` 同一个约定：空串是不画，
	// 没调过 SetEmpty 用默认那句。
	empty    string
	emptySet bool

	// emptyView 是「一行都没有时那一格」的画法（见 SetEmptyView）。nil 是没装，
	// 那一格走 emptyText() 那句话。理由和 `List.emptyView` 一样。
	emptyView func(c *ui.Context)

	// width 是这个网格**上一帧**量出来的容器宽，用来判断点。
	width float32

	// cols 是上一帧的列数，keepAnchor 靠它把行号换算成歌的下标。
	cols int

	// started 是「已经画过一帧」。头一帧没有上一帧的行高和滚动位置可依据，
	// 头几帧也可能还没有，都不能拿来做锚点。
	started bool

	// keys 记「上一次输入是键盘还是指针」。
	//
	// ⚠️ **网格这一档还没有焦点环**（`List` 那一档有，见 `card.go` 的
	// `SelectedRow`），所以它眼下只干一件事：知道「↑ 在顶上一行时把焦点交回
	// 侧栏」（票 #245 的 E4）。焦点环那一档是另一张票的事。
	keys KeyboardMode
}

// NewGrid 造一个网格。
//
// ⚠️ cache 不能是 nil：和 `New` 一样，imgcache.View 一拿到非空的封面地址就在
// 它上面取状态，nil 会让**带封面的每一格**画不出来（每格 recover 一次并记一行
// 日志）。
func NewGrid(cache *imgcache.Cache, acts Actions) *Grid {
	g := &Grid{cache: cache, acts: acts, sel: -1}
	// 「↑ 到顶时把焦点交回侧栏」那一档（票 #245 的 E4）。理由同 `New`。
	g.keys.upAtTop = acts.FocusOut
	return g
}

// TakeFocus 把键盘焦点接到这个网格上，并把光标放到第一首。
//
// ⚠️ **理由和 `List.TakeFocus` 一样**（票 #245 的 E4）：走进来就在第一首上。
func (g *Grid) TakeFocus() {
	if len(g.songs) > 0 {
		g.sel = 0
	}
	g.state.Handle.Focus()
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
func (g *Grid) SetSongs(songs []Music) { g.setSongs(songs, true) }

// setSongs 是 SetSongs 的内里，warn 说要不要再查一遍重复 ID。
//
// ⚠️ **warn 只在真正被 `SetSongs` 调到的那一份上查**：`SwitchView` 两份拿的是同
// 一个切片，让它查两遍就是同一个问题一帧打两行一样的日志（见 switch.go 的
// SetSongs）。
func (g *Grid) setSongs(songs []Music, warn bool) {
	// ⚠️ 先记下**旧的那一份**里选中的是哪一首，再换。顺序反了的话
	// selectedID() 读的是新切片同一个下标上的歌（插在头上时它是另一首），
	// 重新对位就对到错的歌上去了。
	was := g.selectedID()
	g.songs = songs
	g.sel = indexOfID(songs, was)
	if warn {
		warnDuplicateIDs(songs)
	}
}

// selectedID 是此刻选中的那一首的 ID，没选中是空串。
func (g *Grid) selectedID() string {
	if g.sel < 0 || g.sel >= len(g.songs) {
		return ""
	}
	return g.songs[g.sel].ID
}

// SetEmpty 换掉「没有歌时说的那句话」。空串是不画（网格就是空的）。
//
// ⚠️ 网格的默认那句**和列表一样**（「这里还没有歌」），不抄第二份：同一个应用里两种
// 列表写着两种说法，用户会以为是两个意思。
//
// ⚠️ 只能由界面线程调，同 SetSongs。
func (g *Grid) SetEmpty(text string) { g.empty, g.emptySet = text, true }

// SetEmptyView 换掉「一行都没有时**那一格**」的画法。理由见 `List.SetEmptyView`。
//
// ⚠️ **传 nil 是退回那句话**（`SetEmpty` 那一句，或默认那句）。
//
// ⚠️ 只能由界面线程调，同 SetSongs。
func (g *Grid) SetEmptyView(fn func(c *ui.Context)) { g.emptyView = fn }

// SetFooter 装末尾那一格的画法（转圈 / 重试），传 nil 去掉。
//
// ⚠️ 只能由界面线程调，同 SetSongs。
func (g *Grid) SetFooter(fn func(c *ui.Context)) { g.footer = fn }

// SetScroll 让这个网格把滚动位置写进 s（每帧写一次）。理由见那个字段的注释。
//
// ⚠️ 只能由界面线程调，同 SetSongs。
func (g *Grid) SetScroll(s *ui.ScrollState) { g.scroll = s }

// SetSelection 装多选那一摊接线，传零值去掉。理由见 `List.SetSelection`。
//
// ⚠️ 只能由界面线程调，同 SetSongs。
func (g *Grid) SetSelection(s Selection) { g.sel2 = s }

// Len 是这个网格有几**首**歌（末尾那一格哨兵不算）。和 `List.Len` 一个意思：
// 调用方要问的是「这些歌里有哪些」，不是「屏幕上摆了几格」。
func (g *Grid) Len() int { return len(g.songs) }

// At 返回第 i 首那一首，没有是 ok=false。收的是**歌的下标**不是格子序号——
// 格子是布局，列数一变同一首歌就换了格子（和 `List.At` 一个道理）。
func (g *Grid) At(i int) (Music, bool) {
	if i < 0 || i >= len(g.songs) {
		return Music{}, false
	}
	return g.songs[i], true
}

// rows 是这个网格此刻有几**行**，含末尾那一格哨兵。
//
// ⚠️ 列数取的是 `g.cols`，而 `View` 是在**算出这一帧的列数之后、算行数之前**把它
// 换掉的（见那里）——所以这里拿到的就是这一帧真正在画的那一档列数。
func (g *Grid) rows() int {
	cols := g.cols
	if cols <= 0 {
		cols = gridMinCols
	}
	n := len(g.songs)
	if g.footer != nil {
		n++
	}
	return (n + cols - 1) / cols
}

// scrollTop 回到顶上。切路由和点回顶按钮都走它。
//
// ⚠️ 只能由界面线程调。
func (g *Grid) scrollTop() {
	g.state.ScrollTo(0, ui.Start)
}

// visible 是**上一帧**画出来的那几行。`PagedList` 的三重保障问「末尾那行进没进
// 画面」，网格里问的是同一个问题的网格版。
func (g *Grid) visible() (first, last int) { return g.state.Visible() }

// emptyText 是这一帧该说的那句空状态，空串是「什么都不说」。和 `List.emptyText`
// 同一个约定，连默认那句都是同一个（不抄第二份）。
func (g *Grid) emptyText() string {
	if !g.emptySet {
		return defaultEmpty
	}
	return g.empty
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
	// 登记自己是「**此刻内容区里是这一个**」：界面层靠它把焦点交进来
	// （票 #245 的 E4 的「进内容区」）。理由见 focus.go 的文件头。
	if g.acts.Focus != nil {
		g.acts.Focus(g)
	}

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

	// 🔴 铁律四：行身份是这一行**第一首的 ID**，不是行号。
	//
	// 换列数是布局变了、数据没变（铁律四的另一面）：换完之后同一个行号装的是
	// 别的歌，用行号当身份，选中状态和滚动位置就会整体错配。行身份跟着这一行
	// 里的歌走，框架按 ID 重新对位。
	//
	// ⚠️ **末尾哨兵那一行给的是 `footerKey{}`**：它那一行里没有歌（哨兵占的是
	// 最后一格，排在 len(songs) 那一位），照旧写 `g.songs[row*cols].ID` 会越界
	// ——越界是当场 panic（`ui/diagnostics.go`），而它只在「翻页列表切成了网格」
	// 之后才到得了，也就是只有开了这个设置的用户会撞上。
	first := func(row int) int { return row * cols }
	g.state.Key = func(row int) any {
		i := first(row)
		if i >= len(g.songs) {
			return footerKey{}
		}
		// ⚠️ **身份是预先装箱好的那一份**（`g.rowKeys`）：理由同 `List` 那
		// 一处（票 #313）。
		return g.rowKeys.At(row, g.songs[i].ID, rowkey.Identity)
	}
	g.state.Label = func(row int) string {
		i := first(row)
		if i >= len(g.songs) {
			return footerLabel
		}
		return g.songs[i].Title
	}

	// 行数走 `rows()`：它算的是**上一帧的列数**（见那个方法的注释），而上面那个
	// `cols` 是这一帧算出来的。两者只差一帧的宽度变化，不差哨兵——哨兵是同一个
	// 字段算的。
	cell := cellWidth(w, cols)
	// ⚠️ **`g.cols` 必须在算行数之前换成这一帧的**：行数是拿它去除的，而 `viewRow`
	// 收的也是这一帧的 `cols`。晚一步的话（第一帧 `g.cols` 还是 0，按最窄那档 2
	// 列算）行数会比这一帧真正画得下的多，多出来那几行里没有歌，于是走末尾哨兵
	// 那一支——而哨兵压根没装（`g.footer` 是 nil），当场空指针崩在画页脚那里。
	g.cols, g.started = cols, true

	e := ui.List(c, &g.state, g.rows()).
		Grow(1).
		FillWidth().
		// ⚠️ **听一听输入**：为了知道「键盘往回走到顶上了」（票 #245 的 E4）。
		// 返回 false 是刻意的：自己不吃这个事件，方向键挪游标、点击选格、
		// Tab 换焦点都还在。
		HandleInput(g.keys.Input).
		// 没有歌时网格是空的，Children 里那一支就是它的空状态。
		Children(func() {
			if len(g.songs) == 0 {
				// ⚠️ **先看外面递进来的画法**（`SetEmptyView`），理由见
				// `List.SetEmptyView`。
				if g.emptyView != nil {
					g.emptyView(c)
					return
				}
				if txt := g.emptyText(); txt != "" {
					ui.Text(c, txt).FontSize(scale.Font(c, scale.Size13)).
						TextColor(c.Theme().TextMuted).Padding(space.Px16)
				}
			}
		}).
		Rows(func(r ui.ListRow) { g.viewRow(r, cols, cell) })

	// 滚动位置写回调用方的句柄（理由同 `List.scroll`）。**只给正在画的网格**：
	// 切视图时另一个网格也还挂着一个句柄，两边都写就是两个控件抢同一个数。
	if g.scroll != nil {
		e.TrackScroll(g.scroll)
	}

	// 量这一帧的容器宽，下一帧用。`Bounds` 读的是**上一帧**量好的盒子
	//（`ui/api_gen.go` 里 Bounds 的注释原话），所以窗口改宽之后列数晚一帧换
	//——和 Biu 那边 ResizeObserver 改 state 再重画是同一个节奏。
	if bw := e.Bounds().W; bw > 0 {
		g.width = bw
	}

	// 量滚动条的悬停（理由同 `List.View` 末尾那一行）。
	g.hot = scrollbar.Hot(e)
}

// Hot 报上一次 View 时指针是不是压在这个网格的滚动条上。见 `List.Hot`。
func (g *Grid) Hot() bool { return g.hot }

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

	// 哨兵**独占一行**时（`len(g.songs)` 正好是列数的整数倍）：这一行里没有歌。
	if first >= len(g.songs) {
		g.viewFooterRow(c, cols, cell)
		return
	}
	m := g.songs[first]

	// 崩过的那一行**不再画**（和 List.viewRow 一个写法）：不然它每一帧崩一次、
	// 每帧打一整份 debug.Stack，日志被刷爆、帧率拖垮，比「这一行画不出来」
	// 糟得多。
	//
	// ⚠️ 记在 failedRow 里，不是 failed（格子的那张表）：两张表的键都是那一首
	// 的 ID，第一格崩了记的就是第一首的 ID，共用一张表的话下一帧行先短路成
	// 「这一行画不出来」，同行里另外几首好的也跟着没了。
	if g.failedRow[m.ID] {
		ui.Text(c, "这一行画不出来").FontSize(scale.Font(c, scale.Size13)).TextColor(c.Theme().TextMuted).
			Padding(cardPadY, cardPadX).MaxLines(1)
		return
	}
	guardFailed(&g.failedRow, "网格行", m.ID, func() {
		// ⚠️ Stretch：格子要撑满这一行的高，选中那一格的底色才是一整块，
		// 而不是只有歌名那一小块。
		ui.Grid(c).AlignSelf(ui.Stretch).Columns(cols).Gap(gridGap).Children(func() {
			for k := range cols {
				i := first + k
				// 越过最后一格，或者这一格是「歌画完了、又没装哨兵」的那一格：
				// 空着（CSS grid 也是这么摆的，空格子不占位也不画边框）。
				//
				// ⚠️ **第二个条件不能省**：最后一行没排满时 `i == len(g.songs)` 也会
				// 走到这里，而那时根本没有哨兵（`rows()` 不把哨兵算成一行，
				// `PagedList.syncList` 早就 `SetFooter(nil)` 了）。不挡的话它会落到
				// 下面那一支去**越界取 `g.songs[i]`**，当场崩在画这一行上，屏幕上留下
				// 「这一行画不出来」——`TestEveryViewSurvivesTheNarrowestWindow`
				// 量到的就是它（奇数首歌 + 网格，最窄窗口）。
				if i > len(g.songs) || (i == len(g.songs) && g.footer == nil) {
					return
				}
				// 🔴 **哨兵落在这一行的第 k 格**（不是第 0 格时）。哨兵排在
				// `len(g.songs)` 那一位，而行是按列数切出来的——`len % cols` 不是 0
				// 时它就在某一行的中间。第一版只判了「整行从哨兵开始」那一支
				// （`first >= len`），中间那一支漏了，于是**歌数除不尽列数时网格里
				// 看不见「正在加载 / 点此重试」**——翻页失败之后页面上什么都不剩，
				// 而 `blocked` 闩住的是自动重试。
				// `TestTheGridStillOffersARetryOnARealPage` 钉的就是这一条
				// （40 首、3 列、哨兵落在第 2 格）。
				if i == len(g.songs) {
					g.viewFooterCell(c, cell)
					continue
				}
				g.viewCellGuarded(c, i, cell)
			}
		})
	})
}

// viewFooterRow 画哨兵**独占一行**时的那一行：哨兵在第一格，其余格子空着。
//
// ⚠️ 画在第一格而不是居中：Biu 的网格页脚是 `justify-center` 的一行，而把它摆在
// 网格里的话，「转圈」会跟着列数在屏幕上左右跳——拖一次窗口边就换一个位置，而它
// 是「这里正在加载」唯一的视觉标记。钉在左边至少是不动的。
func (g *Grid) viewFooterRow(c *ui.Context, cols int, cell float32) {
	ui.Grid(c).AlignSelf(ui.Stretch).Columns(cols).Gap(gridGap).Children(func() {
		for k := range cols {
			if k > 0 {
				return
			}
			g.viewFooterCell(c, cell)
		}
	})
}

// viewFooterCell 画末尾哨兵的那一格，画崩了只废掉那一格（理由同 viewRow 的两层
// 兜法）。
func (g *Grid) viewFooterCell(c *ui.Context, cell float32) {
	if g.failed[footerFailed] {
		g.failedCellText(c)
		return
	}
	guardFailed(&g.failed, "网格末尾那一格", footerFailed, func() {
		ui.Box(c).Width(cell).AlignSelf(ui.Start).Children(func() { g.footer(c) })
	})
}

// viewCellCheckbox 画这一格最上面那个勾选框，没装多选就什么都不画。
//
// ⚠️ 收的是**第几首**而不是第几格（理由同 `List.sel2`：`On` / `Toggle` 收的都是
// 歌的下标，而格子是布局）。接线和值都照 `List.viewCheckbox` 那一处——两个视图的
// 多选必须说同一件事，否则「在列表里勾了、切成网格就没了」是同一个功能的两种答案。
func (g *Grid) viewCellCheckbox(c *ui.Context, i int, title string) {
	if g.sel2.On == nil {
		return
	}
	checked := g.sel2.On(i)
	box := ui.Checkbox(c, &checked, "")
	if box.Changed() && g.sel2.Toggle != nil {
		g.sel2.Toggle(i)
	}
	box.Label("选择 " + title)
}

// failedCellText 是「这一格画不出来」那一块的占位（哨兵那一格也用它）。
//
// ⚠️ 和画崩了的那**一行**（「这一行画不出来」）说的不是同一件事：前者是一首歌的
// 封面坏了，后者是这一行的布局坏了。两句分开是照着 `List` 那边已有的两句来的。
func (g *Grid) failedCellText(c *ui.Context) {
	ui.Text(c, "这一格画不出来").FontSize(scale.Font(c, scale.Size13)).TextColor(c.Theme().TextMuted).
		Padding(cardPadY, cardPadX).MaxLines(1)
}

// viewCellGuarded 画一格，画崩了只废掉这一格。
func (g *Grid) viewCellGuarded(c *ui.Context, i int, cell float32) {
	m := g.songs[i]
	if g.failed[m.ID] {
		g.failedCellText(c)
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
// ⚠️ **封面底下渐变条的左边是播放量**（上游 `music-card/index.tsx:50-55`）：填的是
// `Music.View`。**没有这个数的那几页（本地音乐、历史记录、搜索的「用户」那一档）
// 那一半不画**——画一列空白比不画更难看，而用户看见半行字会以为这一格「数据不全」
// （理由见 `Music.View`）。
func (g *Grid) viewCell(c *ui.Context, i int, cell float32) {
	t := c.Theme()
	m := g.songs[i]
	playing := isPlaying(g.acts, m.Item)

	// ⚠️ **选中不再改这一格的颜色**（票 #197）：原来选中的那一格铺一整块实心主色底
	// （和列表选中行是同一套，`ui/table.go` 的 node.Selected），而拍板的是「点一行
	// 不再整行刷绿」——上游压根没有这一层。所以选中的那一格和没选中的格子长得
	// **一个像素都不差**。
	//
	// 剩下的唯一一档主色是「正在放」那一格：它跟着声音走，不跟着光标走。
	//
	// ⚠️ **「正在放」和「选中」同时成立时，「正在放」赢**（理由同 `card.go` 的
	// `rowColors`）：主色底已经没有了，两件事里只有一件还画得出来。
	titleColor, metaColor := t.Text, t.TextMuted
	if playing {
		// 主色当**字**：浅色下要一个与底色对比度 ≥4.5:1 的变体（理由见
		// `widgets/ink`）；深色下原样。
		titleColor = ink.AccentOn(t)
	}

	box := ui.Column(c).Radius(t.Radius).
		// 右键菜单挂在**这一格**上，和列表挂在行上是同一件事：MyGo 取的是
		// 最里层那个带菜单的元素（`ui/menu.go` 的 menuTarget），挂到网格外面
		// 的话整页共用一份菜单。
		ContextMenu(func(m *ui.Menu) { buildSongMenu(m, g.songs, g.acts, i, g.playFrom) }).
		// 点一下选中这一首。Biu 的卡片单击是直接播，这里只选中：桌面端单击
		// 就换队列太容易误触，播放走双击和右键菜单。
		OnClick(func() { g.sel = i })

	// ⚠️ **hover 要在 `Children` 之前问**（同 `card.go` 里那一处）：它报的是这一帧指针
	// 在不在这一格上，而封面上面那圈反馈要等 Children 才建得出来。
	onCell := box.Hovered()

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
		// ⓪ 多选态：这一格最上面的勾选框。没装多选就一格都不画。
		g.viewCellCheckbox(c, i, m.Title)

		// ⓪⓪ 格子右上角那枚「⋯」。点开的是**这一格的菜单**，和右键同一份
		//（`operation.go` 的文件头）。
		//
		// ⚠️ **它压在封面上**（右上角那一块），所以自带一层半透明的深底——理由见
		// `OperationCorner`。
		// ⚠️ **无障碍名字是记好的那一份**（`g.opText`）：理由同 `List` 那处。
		OperationCorner(c, g.opText.At(i, m.Title, OperationLabel), func(m *ui.Menu) {
			buildSongMenu(m, g.songs, g.acts, i, g.playFrom)
		})

		// ① 封面 + 它上面那圈 hover 反馈：右下角一个 40 的主色圆播放钮，封面底部
		// 一条 black/90 的渐变（左播放量 / 右时长），都 200ms 淡入。
		hover.CardCover(c, g.cache, m.Cover, cell, gridCoverH, onCell,
			g.countText.At(i, m.View, PlayCountText),
			g.durText.At(i, m.Duration, formatDuration), m.Title,
			func() { g.playFrom(i) })

		// ② 歌名 + UP 名/时长。
		//
		// ⚠️ 别给这几块设固定高度（Height/SingleLine）：**网格这一档的行高是量出来
		// 的**，歌名折行时那一行就高一点（上游的 music-card 是 `line-clamp-2`，两行）。
		// 列表那一档的行高是定死的（`card.go` 的文件头），网格这一档不是——两处是
		// 两件事，别把一边改成另一边的做法。
		sh := showOf(m)
		ui.Column(c).Grow(1).AlignItems(ui.Start).Gap(textGap).PaddingY(cardGap).
			Children(func() {
				ui.Text(c, sh.title).FontSize(scale.Font(c, scale.ListTitle)).LineHeight(leading.Title).
					TextColor(titleColor).MaxLines(titleLines).Ellipsis("…")
				ui.Row(c).Gap(cardGap).Children(func() {
					if sh.owner != "" || sh.live {
						ownerLine(c, ownerRow{
							shown: sh,
							name:  m.Owner,
							mid:   m.Item.ArtistMid,
							// ⚠ **提示是记好的那一份**（`g.ownerTip`）：现算的
							// 话每帧每一格一次字符串拼接（票 #313 那一类）。
							tip:   g.ownerTip.At(i, m.Owner, OwnerTipText),
							size:  scale.Font(c, scale.ListOwner),
							lh:    leading.Owner,
							color: metaColor,
							grow:  true,
							acts:  g.acts,
						})
					}
					// 正在放的那一格：时长换成一句「正在播放」。
					//
					// ⚠️ **换掉时长而不是加在后面**：一格就那么宽，两样都画必然把
					// 时长挤成「0:」——而用户读的是「正在放」不是那半截时长。
					if playing {
						// 标签带上歌名，理由见 card.go 那一处。颜色和上面那个歌名
						// 是同一档「主色当字」（`ink.AccentOn`）。
						ui.Text(c, PlayingMark).Label(m.Title + " " + PlayingMark).
							FontSize(scale.Font(c, scale.Size11)).TextColor(ink.AccentOn(t)).MaxLines(1)
						return
					}
					// 时长没有就不画（Biu 的 `durationText` 为空时那一块
					// 整个不渲染）。
					if s := g.durText.At(i, m.Duration, formatDuration); s != "" {
						ui.Text(c, s).FontSize(scale.Font(c, scale.Size12)).TextColor(metaColor).MaxLines(1)
					}
				})
			})
	})
}

// playFrom 把队列**换成**第 i 首往后的那一段并开始播。菜单的「播放」和双击这一格都用它。
func (g *Grid) playFrom(i int) { playFrom(g.songs, g.acts, i) }

// gridRowHeight 是网格那一行**大概**多高。
//
// ⚠️ **它是估的**：网格这一行的高跟着内容走（封面 140 + 歌名 + UP 名那一行），
// 歌名折成两行就高 24（16 × 1.5）。所以按行算出来的像素是**大概**的——回顶按钮早一点或晚一点
// 出现，仅此而已。
//
// 这个数是**一行的常见高度**（歌名一行）：140 + 8（`cardGap`）+ 歌名一行 24
// （16 × 1.5）+ 缝 4 + UP 名那一行 14 = 190，和 `grid_test.go` 的 `gridH` 那句注释
// （「网格每行约 190」）对得上。
const gridRowHeight = 190

// ScrollOffset 是这一屏滚过了多少像素。
//
// ⚠️ **它是估的**（见 `gridRowHeight`）：网格的行高跟着内容走，没有一个定死的行高
// 可乘。列表那一档是准的（`List.ScrollOffset`）。
func (g *Grid) ScrollOffset() float32 {
	first, _ := g.state.Visible()
	return float32(first) * gridRowHeight
}
