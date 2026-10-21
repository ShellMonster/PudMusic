// 音乐卡片：一行长什么样。
//
// 结构照 Biu 的 `src/components/music-list-item/index.tsx`（那一行的七列网格
// 里取这里用得上的几列）：序号、封面、标题、UP 名、时长、操作。封面用已经建好的
// `widgets/imgcache`，它管空地址 / 载入中 / 就绪 / 失败四态，这里不重复画
// 占位（重复画就会出现两个占位，或者一个图上面盖着一个框）。
//
// # ⚠️ **行高是定死的，歌名只占一行**
//
// 上游那一行的歌名是 `truncate`（**一行**，多了就省略号），而**网格卡片**那一格
// 是 `line-clamp-2`（两行）。我们原来两处都按两行画，于是列表的行高在 64 和 76
// 之间跳——一屏能看几首歌会随着歌名长短变来变去，而用户看的是「这一页有多少歌」。
//
// 现在列表这一档：**歌名一行，行高 64**（封面 48 + 上下内边距 8×2）。
// 网格那一档**不动**：它那两行是上游的（`music-card/index.tsx` 的
// `line-clamp-2`），而且网格的行高本来就是按内容量的（见 `grid.go`）。
//
// ⚠️ **别把行高改回「按内容量」**：那一条路是框架的（`ui.List` 按量出来的高度
// 校正它对未量到的行的估计，`mygo/ui/list.go` 的 listHeights），网格那一档还在走。
// 列表这一档钉死之后，它那部分机制就只剩网格在用了。
package list

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

const (
	// coverSize 是封面边长。Biu 那一行是 48（`music-list-item` 的封面）。
	coverSize = 48

	// cardPadY / cardPadX 是一行的内边距（`Padding` 的 CSS 写法：上下、左右）。
	// Biu 是 `p-2`，四边都是 8，这里照它来。两个名字是为了以后只想改一边时
	// 不用重写调用处。
	cardPadY = 8
	cardPadX = 8

	// cardGap 是行里几块之间的间距。Biu 是 grid 的 gap，这里取 8。
	cardGap = 8

	// indexWidth 是序号那一列的宽度。Biu 是 `min-w-8 text-center`，8 个字宽
	// 够放四位数的歌单序号（9999 首）。
	indexWidth = 40

	// durationWidth 是时长那一列的宽度。写死是为了让时长上下对齐（Biu 的
	// `tabular-nums`）；不写的话 3:05 和 10:05 两列会左右跳。
	durationWidth = 48

	// listRowHeight 是列表那一行的高。**它是定死的**：封面 48 + 上下内边距 8×2。
	//
	// ⚠️ 它不是「量出来的」而是「定下来的」：歌名只占一行（见文件头），这一行
	// 的内容就是那 48 的封面撑着，量出来也是 64——写死是为了让这个数**保证**是
	// 64，而不是「碰巧量出来是 64」（歌名那一列要是哪天多了一行，量出来的就变了）。
	listRowHeight = 64

	// listTitleLines 是列表那一行歌名最多几行。**1**——上游那一行是 `truncate`
	// （多了就省略号），而网格卡片那一格是两行（`titleLines`）。
	listTitleLines = 1

	// titleLines 是**网格卡片**那一格歌名最多几行。2 是 Biu 的 `line-clamp-2`。
	//
	// ⚠️ **别拿它当列表那一行的行数**：那两处是两件事。网格那一格是方的，歌名
	// 折两行不影响别的；列表那一行折两行，整张列表的行高就跟着跳（文件头那一段）。
	titleLines = 2

	// textGap 是歌名和 UP 名之间的距离。Biu 是 `mt-1`（4）。
	textGap = 4

	// titleLeading 是歌名的行高倍数。1.5 是 Tailwind 的默认行高，也是 Biu 那边
	// `text-base` 的行高。
	//
	// ⚠️ 列表那一行现在只有一行歌名了，这一条决定的是那一行**比 48 的封面矮多少**：
	// 一行歌名 21 + 缝 4 + UP 名 12 = 37，比封面矮 11——所以行高由封面撑着，
	// 歌名和 UP 名在那一行里居中（上游的 `items-center` 也是一个意思）。
	titleLeading = 1.5

	// ownerLeading 是 UP 名的行高倍数。1 是 Biu 那一行的 `leading-none`。
	ownerLeading = 1
)

// 「紧凑」那一档的尺寸。和上面那一组**分开写**而不是给上面那几个加上「按档位取
// 值」的参数：普通那一行是十几条测试量着的对象，给它套一层按档位分支的取值之后，
// 改坏紧凑那档可能连累普通那档的断言，而症状（行高差了七八像素）在屏幕上像「排版
// 有点挤」——极难往回追。
const (
	// compactCoverSize 是紧凑档的封面边长。48 → 32 是这一档省下来的高度里最大
	// 的一块（Biu 的紧凑视图也是把封面缩到 `w-8` 一路）。
	compactCoverSize = 32

	// compactPadY / compactPadX 是紧凑档的内边距。上下 8 → 4，一行省 8 像素。
	compactPadY = 4
	compactPadX = 6

	// compactGap 是紧凑档行里几块之间的间距。
	compactGap = 6

	// compactIndexWidth 是紧凑档序号那一列的宽。「9999 首」的歌单用 40 太宽，
	// 而这一档省的就是横向空间。
	compactIndexWidth = 32

	// compactDurationWidth 是紧凑档时长那一列的宽。Biu 那边用 `tabular-nums`，
	// 宽度定住上下两行才对得齐。
	compactDurationWidth = 40

	// compactTitleLines 是紧凑档歌名最多几行。**1**——这一档和普通那一档现在歌名
	// 一样都是一行（普通那一档改成一行是这一票的事，见文件头），这一档省下来的
	// 高度是封面和内边距。
	compactTitleLines = 1

	// compactRowHeight 是紧凑档那一行的高：封面 32 + 上下内边距 4×2。
	compactRowHeight = 40
)

// PlayingMark 是「这一行正在放」那个标记。
//
// ⚠️ **它的出处只有一处**：`hover.PlayingMark`。这里和 `internal/ui` 那个都指回来，
// 因为同一个应用里两套列表写着两种说法的话，用户会以为是两个意思（BMG-56 已经付过
// 账的一个结论）。
const PlayingMark = hover.PlayingMark

// rowHeight 是这一档的行高。**列表这两档的行高是定死的**（见文件头），所以
// 「滚过了多少像素」能从「顶上那一行是第几行」算出来（见 `List.ScrollOffset`）。
func (l *List) rowHeight() float32 {
	if l.view == ViewCompact {
		return compactRowHeight
	}
	return listRowHeight
}

// ScrollOffset 是这一屏滚过了多少像素。
//
// ⚠️ **它是从「顶上那一行是第几行」算出来的**，不是量出来的：列表这一档的行高是
// **定死的**（64 / 40），所以「第一行的下标 × 行高」就是滚过的像素数，一个不差。
//
// ## 为什么不挂一个 `ui.ScrollState` 来量
//
// `ui.ScrollState` 那一档是**像素**，而列表自己按**行**记位置（`ui.ListState`）。
// 两者在行高可变的时候会对不上，对不上的时候框架会把它们**换算**一次——而换算要
// 靠「还没量过的行按量过的平均值估」，估错了位置就跳了（实测：切回一个 Tab 之后
// 顶上那一首从第 19 首变成第 6 首）。行高定死之后这个换算不再有误差，于是干脆不
// 换算：直接按行算。
//
// ⚠️ **网格那一档是估的**（`Grid.ScrollOffset`）：它的行高跟着内容走（歌名两行），
// 没有一个定死的数可乘。
func (l *List) ScrollOffset() float32 {
	first, _ := l.state.Visible()
	return float32(first) * l.rowHeight()
}

// rowColors 是这一行文字该用的两个颜色。
//
// 「选中」和「正在放」是**两件事**，所以是两档而不是一种底色（理由见下面那个
// switch）：选中跟着光标走（点哪行亮哪行），正在放跟着声音走（声音从哪来亮哪行）。
// 滚歌单的时候光标停在上头、正在放的往往在屏幕中间——不分开的话用户得靠播放条
// 才知道放的是哪首。
func rowColors(t *ui.Theme, selected, playing bool) (title, owner ui.Color) {
	switch {
	case selected:
		// 主色底上的字。UP 名那一行压到 0.75，主色底太亮时它比正文色更
		// 站得住，也还是「次要信息」的样子。
		return t.AccentText, t.AccentText.Alpha(0.75)
	case playing:
		// 主色当**字**：浅色下要一个与底色对比度 ≥4.5:1 的变体（默认那个绿
		// 压在白底上只有 1.9:1），深色下原样。算它的理由见 `widgets/ink`。
		//
		// ⚠️ 别写回 `t.Accent`：深色下看不出差别，浅色下那一行就是「正在放的
		// 那首歌读不出来」——而它恰恰是最该一眼认出的那一行。
		return ink.AccentOn(t), t.TextMuted
	default:
		return t.Text, t.TextMuted
	}
}

// viewCard 画一行：Biu 的 music-list-item 在 MyGo 里的样子。
//
// ⚠️ 选中那一行的底色和字色由 `ui.List` 自己刷（主色底 + 主色上的反色字，
// `ui/table.go` 的 node.Selected），这里**不要**再给文字设固定颜色——一设就
// 盖掉它，选中行会变成主色底上的深色字，深色主题下直接看不见。所以正文色是
// 跟着选中与否给的。
func (l *List) viewCard(r ui.ListRow) {
	if l.view == ViewCompact {
		l.viewCompactCard(r)
		return
	}

	c, i := r.Context, r.Index
	t := c.Theme()
	m := l.songs[i]
	playing := l.isPlaying(m)

	titleColor, ownerColor := rowColors(t, r.Selected(), playing)

	// ⚠️ **hover 必须在 Children 之前问**：`Hovered` 报的是**这一帧**指针在不在
	// 上面，而「按下去有反应」这件事本身要等 Children 建它。MyGo 自己的例子
	// 也是这个写法（`ui/ui_test.go` 的 TestHoverWhilePressed）。
	//
	// ⚠️ 局部变量叫 `onRow` 而不是 `hover`：`hover` 是**包名**，在这里把它当变量
	// 用的话，这一行以下所有用到那个包的地方都得绕开（编译器会让 `hover.RowCover`
	// 报「hover 是个 float32」）。
	//
	// ⚠️ **行高写死 64**（见文件头）：歌名一行，这一行由 48 的封面撑着。
	row := ui.Row(c).AlignSelf(ui.Stretch).Gap(cardGap).Padding(cardPadY, cardPadX).
		Height(listRowHeight).
		// 右键菜单挂在**这一行**上：MyGo 取的是最里层那个带菜单的元素
		//（`ui/menu.go` 的 menuTarget），挂到列表外面的话整列共用一份菜单。
		ContextMenu(func(m *ui.Menu) { l.buildMenu(m, i) })
	onRow := row.Hovered()

	row.Children(func() {
		// ⓪ 多选态：这一行最左边的勾选框。没装多选就一格都不画
		//（见 SetSelection）。
		l.viewCheckbox(c, i, m.Title)

		// ① 序号；正在放的那一行换成一段跳动的波形。见 viewIndex。
		l.viewIndex(c, i, playing, titleColor, m.Title, indexWidth, 12)

		// ② 封面，四态由 imgcache 画（空地址画空框，不画「失败」），上面压着
		// hover 反馈：压暗 + 播放三角。上游 `music-list-item/index.tsx:100-107`。
		hover.RowCover(c, l.cache, m.Cover, coverSize, onRow, m.Title,
			func() { l.playFrom(i) })

		// ③ 标题 + UP 名。这一列 Grow(1)，把剩下的宽度都吃掉。
		//
		// ⚠️ 歌名**只占一行**（上游那一行是 `truncate`）：折行的话整张列表的行高
		// 就跟着歌名长短跳（见文件头）。
		ui.Column(c).Grow(1).AlignItems(ui.Start).Gap(textGap).Children(func() {
			ui.Text(c, m.Title).FontSize(scale.Font(c, 14)).LineHeight(titleLeading).
				TextColor(titleColor).MaxLines(listTitleLines).Ellipsis("…")
			if m.Owner != "" {
				ui.Text(c, m.Owner).FontSize(scale.Font(c, 12)).LineHeight(ownerLeading).
					TextColor(ownerColor).MaxLines(1).Ellipsis("…")
			}
		})

		// ④ 时长。
		//
		// ⚠️ **没有时长也留着那么宽的空位**：行尾那枚「⋯」的位置跟着这一列走，
		// 而表头那一列是永远在的（表头只说这一页真有的列）。少留的话「⋯」在有
		// 时长的行上靠右、在没有的靠左——而表头上那个空位对着的是一个固定位置。
		ui.Box(c).Width(durationWidth).Children(func() {
			if s := formatDuration(m.Duration); s != "" {
				ui.Text(c, s).Width(durationWidth).TextAlign(ui.End).
					FontSize(scale.Font(c, 12)).TextColor(t.TextMuted).MaxLines(1)
			}
		})

		// ⑤ 行尾那枚「⋯」。点开的是**这一行的菜单**，和右键同一份
		//（`operation.go` 的文件头）。
		ui.Box(c).Width(OperationColWidth).Children(func() {
			Operation(c, OperationLabel(m.Title), titleColor,
				func(m *ui.Menu) { l.buildMenu(m, i) })
		})
	})
}

// viewCompactCard 画「紧凑」那一档的一行。
//
// ## 和普通那一行差在哪
//
// 四处，**一处都不碰语义**：封面 48 → 32、上下内边距 8 → 4、行内间距 8 → 6、
// 行高 64 → 40。选中 / 正在放 / 右键菜单 / 勾选框 / 封面上的 hover 反馈 / 行尾
// 那枚「⋯」，这些**一个不少**——紧凑是「同一种行排得更密」，不是「另一种行」。
// 少一样就变成两个控件，而不是同一个控件的两档，用户会以为是两个功能。
//
// ⚠️ **字号只降了歌名那一处**（14 → 13）。UP 名和时长还是 12：它们本来就比正文小，
// 再降一档在 12pt 的基础上会糊，而这一档省的是高度不是宽度。
func (l *List) viewCompactCard(r ui.ListRow) {
	c, i := r.Context, r.Index
	t := c.Theme()
	m := l.songs[i]
	playing := l.isPlaying(m)

	titleColor, ownerColor := rowColors(t, r.Selected(), playing)

	row := ui.Row(c).AlignSelf(ui.Stretch).Gap(compactGap).Padding(compactPadY, compactPadX).
		Height(compactRowHeight).
		// 右键菜单挂在**这一行**上，和普通那一行同一个理由（见 View 里的注释）。
		ContextMenu(func(m *ui.Menu) { l.buildMenu(m, i) })
	onRow := row.Hovered()

	row.Children(func() {
		l.viewCheckbox(c, i, m.Title)
		l.viewIndex(c, i, playing, titleColor, m.Title, compactIndexWidth, 11)

		// 封面 + 它上面那圈 hover 反馈，理由同普通那一行。
		hover.RowCover(c, l.cache, m.Cover, compactCoverSize, onRow, m.Title,
			func() { l.playFrom(i) })

		ui.Column(c).Grow(1).AlignItems(ui.Start).Gap(2).Children(func() {
			ui.Text(c, m.Title).FontSize(scale.Font(c, 13)).LineHeight(1.2).
				TextColor(titleColor).MaxLines(compactTitleLines).Ellipsis("…")
			if m.Owner != "" {
				ui.Text(c, m.Owner).FontSize(scale.Font(c, 12)).LineHeight(ownerLeading).
					TextColor(ownerColor).MaxLines(1).Ellipsis("…")
			}
		})

		// 时长那一列：没有时长也留着位置，理由同普通那一行（见那里）。
		ui.Box(c).Width(compactDurationWidth).Children(func() {
			if s := formatDuration(m.Duration); s != "" {
				ui.Text(c, s).Width(compactDurationWidth).TextAlign(ui.End).
					FontSize(scale.Font(c, 12)).TextColor(t.TextMuted).MaxLines(1)
			}
		})

		// 行尾那枚「⋯」，理由同普通那一行。
		ui.Box(c).Width(OperationColWidth).Children(func() {
			Operation(c, OperationLabel(m.Title), titleColor,
				func(m *ui.Menu) { l.buildMenu(m, i) })
		})
	})
}

// isPlaying 报这一首是不是正在放的那首。判据和理由见 shared.go 的同名函数。
func (l *List) isPlaying(m Music) bool { return isPlaying(l.acts, m.Item) }

// viewIndex 是那一行最左边：平时是序号，**正在放的那一行换成一段跳动的波形**。
//
// ## 波形摆在序号那一格，而不是行尾那句「正在播放」
//
// 上游 `music-list-item/index.tsx` 是后者：序号那一格在放的时候被一段波形占住，而
// 「行尾挂一句『正在播放』」是这一版早先的做法。两者的差别不是好不好看——是**视线**：
// 序号那一列是所有行都对齐的一列，波形出现在那里时眼睛是竖着扫下去的，一眼就知道是
// 哪一行；挂在行尾的话每一行的那句话位置都不一样，得一列一列对过去。
//
// ⚠️ **color 由调用方给**（就是这一行歌名的那个颜色）：波形摆的那一格在选中时是**主色
// 底**，主色底上画一条主色波形等于没画，所以它跟着歌名走。
//
// ⚠️ **认的是「歌」不是「行」**（`isPlaying` 按判重标识，见 `Actions.IsPlaying`）：换歌
// 之后波形跟着换，而正在放的歌在别的一行时这一行退回一个普通的序号。用行的序号认的
// 话，「下一首播放」一插队它就会钉在错的那一行上。
func (l *List) viewIndex(c *ui.Context, i int, playing bool, color ui.Color, title string, width, font float32) {
	t := c.Theme()
	if playing {
		hover.Wave(c, "wave:"+l.songs[i].ID, width, color, title)
		return
	}
	ui.Text(c, indexText(i)).Width(width).TextAlign(ui.End).
		FontSize(scale.Font(c, font)).TextColor(t.TextMuted).MaxLines(1)
}

// viewCheckbox 画这一行最左边那个勾选框，没装多选就什么都不画。
//
// ⚠️ **勾的值是一帧一个的局部变量**，不是页面存的那份。理由是几千行的歌单
// 存不起几千个 bool（而且只有屏幕上那十几行需要）：每一帧现问接线
// `Selection.On`，框架点完之后翻的是这个局部变量，`Changed()` 一响就把这次
// 翻转交给 `Selection.Toggle` 记进页面自己的那份。
//
// `Label` 要给：勾选框本身没有可见文字，没有名字的话读屏软件只会念一句
// 「复选框」，Tester 也没法 `Click` 它。
//
// ⚠️ **`Width(CheckColWidth)` 不能省**：表头那一行照着同一个数给它留了位置（见
// `header.go` 的 LeadCol），这一边不写死的话两边就差着一个数——而那种差在画面上是
// 「多选一开，右边几列歪了」。今天是同一个数（空标签的 `ui.Checkbox` 就是
// `t.Space(4)` 见方），写死是为了主题的间距一改两边还能一起走。
func (l *List) viewCheckbox(c *ui.Context, i int, title string) {
	if l.sel2.On == nil {
		return
	}
	checked := l.sel2.On(i)
	box := ui.Checkbox(c, &checked, "").Width(CheckColWidth(c))
	if box.Changed() && l.sel2.Toggle != nil {
		l.sel2.Toggle(i)
	}
	box.Label("选择 " + title)
}
