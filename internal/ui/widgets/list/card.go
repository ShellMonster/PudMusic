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
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/leading"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// coverSize 是封面边长。Biu 那一行是 48（`music-list-item` 的封面）。
	coverSize = 48

	// cardPadY / cardPadX 是一行的内边距（`Padding` 的 CSS 写法：上下、左右）。
	// Biu 是 `p-2`，四边都是 8，这里照它来。两个名字是为了以后只想改一边时
	// 不用重写调用处。
	cardPadY = space.Px8
	cardPadX = space.Px8

	// rowGap 是**列表那一行**里几块（序号 / 封面 / 标题 / 各数字列 / 「⋯」）之间
	// 的缝。Biu 是 grid 的 `gap-4`（= 16，`music-list-item/index.tsx:80`），
	// 两种档位都是它。
	rowGap = space.ListGap

	// cardGap 是**网格卡片**里那几块的缝：封面下面那一块 `mt-2` 的纵向留白，
	// 以及 UP 名和时长那一行的缝（Biu 的 `music-card/index.tsx`）。它和 `rowGap`
	// 是两件事——列表的列间距是 16，而卡片里那圈是 8。
	cardGap = space.Px8

	// indexWidth 是序号那一列的宽度。Biu 是 `min-w-8 text-center`（= 32，居中）。
	// 四位数的歌单序号（9999 首）也放得下。
	indexWidth = 32

	// durationWidth 是时长那一列的宽度。写死是为了让时长上下对齐（Biu 的
	// `tabular-nums`）；不写的话 3:05 和 10:05 两列会左右跳。
	durationWidth = 48

	// playCountWidth / pubTimeWidth 是播放量和时间那两列的宽。
	//
	// 上游是 100 / 110（`music-list-item/styles.ts` 的 grid）。我们收窄一点
	// 不是省地方——是**最窄那一档要留得出标题**：窗口最小 900、侧栏能拖到
	// 480，放开的话右边那几列会顶出内容区。
	//
	// ⚠️ **播放量最长不是「9999.9万」而是「10000.0万」**：`PlayCountText` 对
	// 一亿以下按万折，`FormatFloat(…, 1)` 会把 99999999 进成 10000.0——font 12
	// 下约 60 宽。64 / 80 都还够（紧凑那档也因此是 64，见下面）。
	// 日期最长是「2026-10-21」（约 66 宽），80 够。
	playCountWidth = 64
	pubTimeWidth   = 80

	// listRowHeight 是列表那一行的高。**它是定死的**：封面 48 + 上下内边距 8×2。
	//
	// ⚠️ 它不是「量出来的」而是「定下来的」：歌名只占一行（见文件头），这一行
	// 的内容就是那 48 的封面撑着，量出来也是 64——写死是为了让这个数**保证**是
	// 64，而不是「碰巧量出来是 64」（歌名那一列要是哪天多了一行，量出来的就变了）。
	listRowHeight = 64

	// listTitleLines 是列表那一行歌名最多几行。**1**——上游那一行是 `truncate`
	// （多了就省略号），而网格卡片那一格是两行（`titleLines`）。
	listTitleLines = 1

	// listTitleMaxW 是**歌名那一列最多占多宽**（票 #244 / UX 报告 §2 的 E1）。
	//
	// # 为什么歌名要有一个上限
	//
	// 那一列是 `Grow(1)`——它吃掉了整行剩下的宽，于是**一个 630 像素的歌名一路
	// 长到离「播放量」那一列只差 31 像素**（实测：歌名 x=337..969，播放量
	// x=1000）。于是：
	//
	//   - 省略号要等歌名长到 700 多像素才出现（列边界离得太远）；
	//   - 这一行里「歌名 / 播放量 / 时长」被拉开 900 多像素——「一行一首歌」那个
	//     紧凑的信息单元被拉散了。
	//
	// 420 是量出来的：默认窗口（1200）下那一列是 520~630，收到 420 之后歌名和
	// 右边那几列之间**始终留着一段看得见的空**，而 420 仍然放得下二十来个汉字
	//（歌名字号 16）。
	//
	// ⚠️ **上限加在「字」上，不加在「列」上**：框架的 flex **没有收缩**，而行里
	// 歌名的宽度是**按内容算的**——给列加一个上限的话，长歌名那一行的歌名列会比
	// 短歌名那一行窄，于是右边那几列**逐行错开**（而画面上只是「表头歪了一点」）。
	// 字有上限、列照旧吃满剩下的宽，两边就都对得上。
	//
	// ⚠️ **UP 名用同一个数**：它和歌名在同一列里，不给它上限的话一行很长的
	// UP 名照样会一路顶到右边那几列跟前（它字号更小，跑得更远）。
	listTitleMaxW = 420

	// titleLines 是**网格卡片**那一格歌名最多几行。2 是 Biu 的 `line-clamp-2`。
	//
	// ⚠️ **别拿它当列表那一行的行数**：那两处是两件事。网格那一格是方的，歌名
	// 折两行不影响别的；列表那一行折两行，整张列表的行高就跟着跳（文件头那一段）。
	titleLines = 2

	// textGap 是歌名和 UP 名之间的距离。Biu 是 `mt-1`（4）。
	textGap = space.Px4
)

// 「紧凑」那一档的尺寸。和上面那一组**分开写**而不是给上面那几个加上「按档位取
// 值」的参数：普通那一行是十几条测试量着的对象，给它套一层按档位分支的取值之后，
// 改坏紧凑那档可能连累普通那档的断言，而症状（行高差了七八像素）在屏幕上像「排版
// 有点挤」——极难往回追。
//
// ⚠️ **紧凑档没有封面**（上游就是：`music-list-item/index.tsx:85-88` 的紧凑那一
// 支只画歌名，`w-8` 的封面整块不在）。这一档省下来的高度里最大的一块就是它。
const (
	// compactPadY / compactPadX 是紧凑档的内边距。上下 8 → 4，一行省 8 像素。
	compactPadY = space.Px4
	compactPadX = space.Px6

	// compactIndexWidth 是紧凑档序号那一列的宽。
	compactIndexWidth = 32

	// compactDurationWidth 是紧凑档时长那一列的宽。Biu 那边用 `tabular-nums`，
	// 宽度定住上下两行才对得齐。
	compactDurationWidth = 40

	// compactPlayCountWidth / compactPubTimeWidth 是紧凑档那两列的宽。
	//
	// ⚠️ **播放量那一列和普通档一样是 64**：`PlayCountText` 最长能产出
	// 「10000.0万」（99,999,999 按万折、一位小数进位之后），font 12 下约
	// 60 宽。56 会把最后那一位裁掉，读数变成「10000....」——评审量到过。
	// 日期那一档最长是「2026-10-21」，72 够。
	compactPlayCountWidth = 64
	compactPubTimeWidth   = 72

	// compactTitleLines 是紧凑档歌名最多几行。**1**——这一档和普通那一档现在歌名
	// 一样都是一行（普通那一档改成一行是这一票的事，见文件头），这一档省下来的
	// 高度是封面（整块不画）和内边距。
	compactTitleLines = 1

	// compactRowHeight 是紧凑档那一行的高。**定死的**（同 `listRowHeight`）：
	// 这一行里没有封面撑着，歌名和 UP 名那两块加起来才 33，剩下的靠这个数钉住。
	// 40 和改之前一样——这一档省的是封面和内边距，不是行高。
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
//
// ⚠️ **页内头部算不算在「滚过的像素」里**：算——它是内容的一部分，滚过去就是滚过
// 了（见 lead.go）。它有多高是**估的**（`Lead.Height`），差几十像素的后果只是那枚
// 回顶按钮早一点或晚一点出现。
func (l *List) ScrollOffset() float32 {
	first, _ := l.state.Visible()
	lead := leadRows(l.lead)
	if first < lead {
		// 页内头部还在画面上：还没滚过它。
		return 0
	}
	return leadHeight(l.lead) + float32(first-lead)*l.rowHeight()
}

// rowColors 是这一行文字该用的两个颜色。
//
// # ⚠️ **「选中」不再是一档颜色**（票 #197）
//
// 原来这里是两档：选中的行给主色底上的反色字，正在放的行给主色当字。2026-10-22
// 拍板的是「列表选中行的实心主色底：去掉」——上游压根没有这一层，点一行不刷绿。
// 于是选中的行**和没选中的行长得一样**（见 `rowCover`：框架那一层实心主色底被
// 这一行自己刷的底色盖掉了），文字也就没有理由再分一档。
//
// 剩下的唯一一档主色是「正在放」那一行：它跟着声音走，不跟着光标走。滚歌单的
// 时候光标停在上头、正在放的往往在屏幕中间——不分开的话用户得靠播放条才知道放
// 的是哪首。
//
// ⚠️ **「正在放」和「选中」同时成立时，「正在放」赢**：主色底已经没有了，两件事
// 里只有一件还画得出来，而它才是用户随时要认的那一件（正在放的那首在别的页面上
// 也要认得出来）。
func rowColors(t *ui.Theme, playing bool) (title, owner ui.Color) {
	if playing {
		// 主色当**字**：浅色下要一个与底色对比度 ≥4.5:1 的变体（默认那个绿
		// 压在白底上只有 1.9:1），深色下原样。算它的理由见 `widgets/ink`。
		//
		// ⚠️ 别写回 `t.Accent`：深色下看不出差别，浅色下那一行就是「正在放的
		// 那首歌读不出来」——而它恰恰是最该一眼认出的那一行。
		return ink.AccentOn(t), t.TextMuted
	}
	return t.Text, t.TextMuted
}

// rowCover / rowFocusRing / FocusVisible 都在 `selection.go` 那一层（票 #197
// 把「选中」这一层整个翻了过来，理由写在那儿）。

// viewCard 画一行：Biu 的 music-list-item 在 MyGo 里的样子。
//
// ⚠️ **选中那一行的实心主色底是这一行自己盖掉的**（`rowCover`）：框架的 `ui.List`
// 会给选中的行铺一整块主色底（`ui/table.go` 的 node.Selected），票 #197 拍板去掉
// 它，而那一层在库里改不动——所以这一行自己刷一层底色盖上去。
//
// ⚠️ **文字颜色不再跟着选中与否变**（`rowColors`）：主色底没了，「主色底上的反色
// 字」也就没有意义了；留着它的话，选中的行在浅色下会变成白底上的白字——直接
// 看不见。
func (l *List) viewCard(r ui.ListRow) {
	if l.view == ViewCompact {
		l.viewCompactCard(r)
		return
	}

	// ⚠️ **i 是「歌的下标」**：框架给的是**列表的行号**，而页内头部占着第 0 行
	// （见 lead.go）。
	c, i := r.Context, r.Index-leadRows(l.lead)
	t := c.Theme()
	m := l.songs[i]
	playing := l.isPlaying(m)

	titleColor, ownerColor := rowColors(t, playing)

	// ⚠️ **hover 必须在 Children 之前问**：`Hovered` 报的是**这一帧**指针在不在
	// 上面，而「按下去有反应」这件事本身要等 Children 建它。MyGo 自己的例子
	// 也是这个写法（`ui/ui_test.go` 的 TestHoverWhilePressed）。
	//
	// ⚠️ 局部变量叫 `onRow` 而不是 `hover`：`hover` 是**包名**，在这里把它当变量
	// 用的话，这一行以下所有用到那个包的地方都得绕开（编译器会让 `hover.RowCover`
	// 报「hover 是个 float32」）。
	//
	// ⚠️ **行高写死 64**（见文件头）：歌名一行，这一行由 48 的封面撑着。
	row := ui.Row(c).AlignSelf(ui.Stretch).Gap(rowGap).Padding(cardPadY, cardPadX).
		Height(listRowHeight).
		// 右键菜单挂在**这一行**上：MyGo 取的是最里层那个带菜单的元素
		//（`ui/menu.go` 的 menuTarget），挂到列表外面的话整列共用一份菜单。
		ContextMenu(func(m *ui.Menu) { l.buildMenu(m, i) })
	onRow := row.Hovered()

	// 选中那一行：盖掉框架的实心主色底，键盘走到它上面时给它一圈焦点环
	//（理由见 `selection.go`）。
	//
	// ⚠️ **环的条件是「列表有键盘焦点」且「上一次输入是键盘」**：鼠标点过之后
	// 方向键挪了游标，那一行也得有指示（框架那个 `focusVisible` 方向键不会设回来，
	// 靠它那一路是哑的——理由见 `selection.go` 的 KeyboardMode）。
	row = SelectedRow(row, t, r.Selected(), onRow,
		l.keys.On() && Focused(c, &l.state))

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
		//
		// ⚠️ **歌名和 UP 名各有一个宽度上限**（`listTitleMaxW`）：这一列是
		// `Grow(1)`，吃掉了整行剩下的宽，而字没有上限的话一个 630 像素的歌名
		// 会一路长到离右边那几列只差 31 像素（理由见那个常量）。
		// 显示的字是清洗过的（`showOf`）：去掉视频宣传标签，歌手 / 专辑名挪到副标题，
		// 现场版在副标题前加一枚「现场」小标签。
		sh := ShowOf(m)
		ui.Column(c).Grow(1).AlignItems(ui.Start).Gap(textGap).Children(func() {
			ui.Text(c, sh.Title).FontSize(scale.Font(c, scale.ListTitle)).LineHeight(leading.Title).
				TextColor(titleColor).MaxLines(listTitleLines).Ellipsis("…").MaxWidth(listTitleMaxW)
			if sh.Owner != "" || sh.Live {
				OwnerLine(c, sh, scale.Font(c, scale.ListOwner), leading.Owner, ownerColor, listTitleMaxW, false)
			}
		})

		// ④ 播放量 / ⑤ 投稿时间：这一页有才画（见 `Columns`）。两列都
		// **留着位置**——没有这个数的那一行空着，但列宽不动，右边那几列
		// 才不会逐行错开。
		//
		// ⚠️ **问 `l.Columns()` 而不是 `l.cols`**：窄窗口下它会先把「投稿
		// 时间」收掉（见 `minTitleWidth`）。表头读的也是它，两边一致。
		cols := l.Columns()
		l.viewPlayCount(c, i, t, cols, playCountWidth, 12)
		l.viewPubTime(c, i, t, cols, pubTimeWidth, 12)

		// ⑥ 时长。
		//
		// ⚠️ **没有时长也留着那么宽的空位**：行尾那枚「⋯」的位置跟着这一列走，
		// 而表头那一列是永远在的（表头只说这一页真有的列）。少留的话「⋯」在有
		// 时长的行上靠右、在没有的靠左——而表头上那个空位对着的是一个固定位置。
		ui.Box(c).Width(durationWidth).Children(func() {
			// ⚠️ **时长是记好的那一份**（`l.durText`）：现算的话每帧每行一次
			// `fmt.Sprintf`，一屏十来行就是十来次（票 #313）。
			if s := l.durText.At(i, m.Duration, formatDuration); s != "" {
				ui.Text(c, s).Width(durationWidth).TextAlign(ui.End).
					FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).MaxLines(1)
			}
		})

		// ⑦ 行尾那枚「⋯」。点开的是**这一行的菜单**，和右键同一份
		//（`operation.go` 的文件头）。
		ui.Box(c).Width(OperationColWidth).Children(func() {
			// ⚠️ **无障碍名字是记好的那一份**（`l.opText`）：现算的话每帧每行
			// 一次字符串拼接（票 #313）。
			Operation(c, l.opText.At(i, m.Title, OperationLabel), titleColor,
				func(m *ui.Menu) { l.buildMenu(m, i) })
		})
	})
}

// viewCompactCard 画「紧凑」那一档的一行。
//
// ## 和普通那一行差在哪
//
// 三处，**一处都不碰语义**：上下内边距 8 → 4、行高 64 → 40、**封面整块不画**
// （上游的紧凑那一支只画歌名，见 `music-list-item/index.tsx:85-88`）。选中 / 正在放 /
// 右键菜单 / 勾选框 / 行尾那枚「⋯」，这些**一个不少**——紧凑是「同一种行排得更密」，
// 不是「另一种行」。少一样就变成两个控件，而不是同一个控件的两档，用户会以为是两个
// 功能。
//
// ⚠️ **封面没有，所以也没有它上面那圈 hover 播放反馈**（`hover.RowCover`）。上游
// 紧凑那一行同样没有：双击和右键菜单照样能播。
//
// ⚠️ **字号只降了歌名那一处**（16 → 13）。UP 名和时长还是 12：它们本来就比正文小，
// 再降一档在 12pt 的基础上会糊，而这一档省的是高度不是宽度。
func (l *List) viewCompactCard(r ui.ListRow) {
	// ⚠️ **i 是「歌的下标」**：框架给的是**列表的行号**，而页内头部占着第 0 行
	// （见 lead.go）。
	c, i := r.Context, r.Index-leadRows(l.lead)
	t := c.Theme()
	m := l.songs[i]
	playing := l.isPlaying(m)

	titleColor, ownerColor := rowColors(t, playing)

	row := ui.Row(c).AlignSelf(ui.Stretch).Gap(rowGap).Padding(compactPadY, compactPadX).
		Height(compactRowHeight).
		// 右键菜单挂在**这一行**上，和普通那一行同一个理由（见 View 里的注释）。
		ContextMenu(func(m *ui.Menu) { l.buildMenu(m, i) })

	// 选中那一行：盖掉框架的实心主色底，键盘走到它上面时给它一圈焦点环
	//（理由同 `viewCard` 里那一句）。
	row = SelectedRow(row, t, r.Selected(), row.Hovered(),
		l.keys.On() && Focused(c, &l.state))

	row.Children(func() {
		l.viewCheckbox(c, i, m.Title)
		l.viewIndex(c, i, playing, titleColor, m.Title, compactIndexWidth, 11)

		sh := ShowOf(m)
		ui.Column(c).Grow(1).AlignItems(ui.Start).Gap(space.Px2).Children(func() {
			ui.Text(c, sh.Title).FontSize(scale.Font(c, scale.Size13)).LineHeight(leading.Compact).
				TextColor(titleColor).MaxLines(compactTitleLines).Ellipsis("…").MaxWidth(listTitleMaxW)
			if sh.Owner != "" || sh.Live {
				OwnerLine(c, sh, scale.Font(c, scale.Size12), leading.Owner, ownerColor, listTitleMaxW, false)
			}
		})

		// 播放量 / 投稿时间 / 时长：没有数也留着位置，理由同普通那一行
		//（见那里）。三列的宽度是这一档自己那一组（见 `compact*Width`）。
		cols := l.Columns()
		l.viewPlayCount(c, i, t, cols, compactPlayCountWidth, 12)
		l.viewPubTime(c, i, t, cols, compactPubTimeWidth, 12)
		ui.Box(c).Width(compactDurationWidth).Children(func() {
			// 理由同上面那一处（`l.durText`）。
			if s := l.durText.At(i, m.Duration, formatDuration); s != "" {
				ui.Text(c, s).Width(compactDurationWidth).TextAlign(ui.End).
					FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).MaxLines(1)
			}
		})

		// 行尾那枚「⋯」，理由同普通那一行。
		ui.Box(c).Width(OperationColWidth).Children(func() {
			// ⚠️ **无障碍名字是记好的那一份**（`l.opText`）：现算的话每帧每行
			// 一次字符串拼接（票 #313）。
			Operation(c, l.opText.At(i, m.Title, OperationLabel), titleColor,
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
// ⚠️ **color 由调用方给**（就是这一行歌名的那个颜色）：波形是**主色**画的，所以它得
// 跟着这一行的歌名走——「正在放」那一行的歌名是主色（`rowColors`），波形就画在主色
// 那一格里。
//
// （选中的行不再有主色底了——票 #197 去掉的那一层。所以这一格的颜色只跟「正在放」
// 有关，跟「选中」无关。）
//
// ⚠️ **居中对齐**（上游是 `min-w-8 text-center`，`music-list-item/index.tsx:82`）：
// 序号从 1 到几千，位数一变右对齐的左边缘就整列参差；居中之后几位数都落在同一根
// 竖线上。
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
	ui.Text(c, indexText(i)).Width(width).TextAlign(ui.Center).
		FontSize(scale.Font(c, font)).TextColor(t.TextMuted).MaxLines(1)
}

// viewPlayCount 画播放量那一列。**这一页没装这一列时一个元素都不建**——
// 建一个空盒子的话，那些页的右边几列会白白让出一列的宽。
//
// 装了而这一行的 `View` 是零值时，格子**留着、字是空的**：那一页上有的行有
// 播放量、有的没有（收藏夹里的音轨就是），字缺一格而列宽不动，上下才对得齐。
func (l *List) viewPlayCount(c *ui.Context, i int, t *ui.Theme, cols Columns, width, font float32) {
	if !cols.PlayCount {
		return
	}
	ui.Box(c).Width(width).Children(func() {
		// ⚠️ **播放量是记好的那一份**（`l.countText`）：理由同上面那两处。
		if s := l.countText.At(i, l.songs[i].View, PlayCountText); s != "" {
			ui.Text(c, s).Width(width).TextAlign(ui.End).
				FontSize(scale.Font(c, font)).TextColor(t.TextMuted).MaxLines(1)
		}
	})
}

// viewPubTime 画时间那一列。规矩同 `viewPlayCount`：这一页没装就不建，
// 装了而这一条没有时间就留空。
//
// ⚠️ 字的样子在 `PubTimeText` 里定（`今天 / 昨天 / N天前 / MM-DD /
// YYYY-MM-DD`），`now` 来自 `Columns.Now`（界面上是外壳那颗时钟）。
func (l *List) viewPubTime(c *ui.Context, i int, t *ui.Theme, cols Columns, width, font float32) {
	if !cols.PubTime {
		return
	}
	ui.Box(c).Width(width).Children(func() {
		if s := PubTimeText(l.songs[i].PubTime, l.now()); s != "" {
			ui.Text(c, s).Width(width).TextAlign(ui.End).
				FontSize(scale.Font(c, font)).TextColor(t.TextMuted).MaxLines(1)
		}
	})
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
