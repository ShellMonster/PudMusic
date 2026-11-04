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
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/hover"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
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
		return tok.RowCompactH
	}
	return tok.RowH
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
//
// # 🔴 **失效那一档（票 #624）**
//
// invalid 是「这一条已经失效」（稿件被 UP 主删了、或者换成了别的链接）。
// 它拿**次要色**——和「正在放」那一档不一样：
//
//	正在放	主色当字（最该一眼认出的那一行）
//	失效	次要色（这一行放不了了，但**不是**用户正在听的那首）
//
// ⚠️ **「正在放」优先**（票 #624 的评审第 4 条）：这两档**能同时成立**——
// 「正在放」是用户随时要认的那一行，它要是被灰掉，用户就认不出自己正在听
// 的是哪首了。
//
// ⚠️ 而失效那一档也**不会因此失效**：一条**真放上了**的歌不会中途变成失效
// （失效是 B 站在收藏夹里给的事实，而队列里的歌来自取流那一刻）。
func rowColors(t *ui.Theme, playing, invalid bool) (title, owner ui.Color) {
	if playing {
		// 主色当**字**：浅色下要一个与底色对比度 ≥4.5:1 的变体（默认那个绿
		// 压在白底上只有 1.9:1），深色下原样。算它的理由见 `widgets/ink`。
		//
		// ⚠️ 别写回 `t.Accent`：深色下看不出差别，浅色下那一行就是「正在放的
		// 那首歌读不出来」——而它恰恰是最该一眼认出的那一行。
		return ink.AccentOn(t), t.TextMuted
	}
	if invalid {
		return t.TextMuted, t.TextMuted
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
	l.viewPudRow(r)
}

// isPlaying 报这一首是不是正在放的那首。判据和理由见 shared.go 的同名函数。
func (l *List) isPlaying(m Music) bool { return isPlaying(l.acts, m.Item) }

// rankTopFontBoost 是榜单前三名的序号比普通序号大几号：名次是榜单这一页的主角，
// 前三名用主色加粗放大，其余名次与普通序号一样是淡色。
const rankTopFontBoost = 4
