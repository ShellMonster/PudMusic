// 列表页顶上那一行表头。**七个页面共用这一个组件**。
//
// 上游是 `components/music-list-item/header.tsx`：一行 `grid-cols-[auto_1fr_100px_110px_80px_auto`，
// 列定义放在常量文件里，**七个页面在用同一个**。这一版是照它做的——七个页面各写一份表头
// 的话，改了列宽只改掉一边，而那种错在界面上只是「这一页的表头歪了一点」。
//
// # ⚠️ 表头**只说这一页真有的列**
//
// 上游那一行是六列（编号 / 标题 / UP / 播放量 / 投稿时间 / 时长），因为上游的每一行都有
// 那几列。我们不是每一页都有：`list.Music` 上只有 ID / 歌名 / 封面 / UP 名 / 时长，播放
// 量只在「稍后再看」那一页有（`internal/ui/library_rows.go` 的 `libraryRow.View`），而
// 「投稿时间」我们走的是「加入时间 / 观看时间」。
//
// **摆一列这一页没有内容的表头，比不摆表头更糟**，而它糟得很安静：字都在、位置也在，只
// 是「播放量」那一列下面是空的——用户看不出那是没接，还是自己记错了。所以每一页把**它
// 自己那些行的列**交给 `Header`，而**组件只有这一个**。
//
// # ⚠️ 列宽**从行的尺寸里取**，不另抄一份
//
// 表头另抄一份宽度的话，改了行忘了改表头，表头就在那几像素之差上慢慢错开（`later.go` 和
// `history.go` 原来那两份就是：它们把「标题」摆在序号后面，而行里序号和标题中间还隔着一
// 个 48 的封面，于是右边那几列整整差 56）。见 `MetricsOf`。
//
// # 表头**不在滚动区里**
//
// 它是列表**外面**的一个兄弟（页面的 `Children` 里先 `Header(c)` 再 `list.View(c)`），所
// 以列表上下滚它不动；横向没有滚动区（Biu 那一行的列宽是定死的，`1fr` 那一列吃掉剩下的
// 宽），于是「滚到右边列名还在」这一条天然成立。
//
// # ⚠️ **网格那一档不画表头**
//
// 一格是一首歌，表头上那几列和格子对不上号，而对不上的表头在界面上不像坏了（`later.go`
// 里那一处先想到过这件事）。判据在下面那三个 `Header` 方法里，只有一处。
package list

import (
	"github.com/egoist/mygo/ui"
)

// headerFont 是表头上那几个字的字号。12 是 `later.go` / `history.go` 原来那两份用的数，
// 也是 Biu 那一行表头的数（`text-xs`）。
const headerFont = 12

// CardMetrics 是一行（列表 / 紧凑两档）里的那些尺寸。**表头照它摆。**
//
// ⚠️ 这一份是 `card.go` 里那几组常量的**读法**，不是另抄的数：紧凑档的封面、间距、序号
// 宽度都取自同一批常量（见 `card.go` 顶上那两组）。所以改 `card.go` 里的数，表头跟着
// 走；反过来说表头永远不会和行差出那几像素。
type CardMetrics struct {
	PadY, PadX, Gap float32
	// Index 是序号那一列的宽，Cover 是封面的边长，Clock 是时长那一列的宽。
	Index, Cover, Clock float32
	// Compact 报这一份是不是紧凑档。
	Compact bool
}

// MetricsOf 是「这一档该用哪一份尺寸」。
func MetricsOf(v View) CardMetrics {
	if v == ViewCompact {
		return CardMetrics{
			PadY: compactPadY, PadX: compactPadX, Gap: compactGap,
			Index: compactIndexWidth, Cover: compactCoverSize, Clock: compactDurationWidth,
			Compact: true,
		}
	}
	return CardMetrics{
		PadY: cardPadY, PadX: cardPadX, Gap: cardGap,
		Index: indexWidth, Cover: coverSize, Clock: durationWidth,
	}
}

// CheckColWidth 是多选态那个勾选框占多宽。
//
// ⚠️ **和 `viewCheckbox` 里那个数是同一件事**：那边给勾选框写死了这一个宽（空标签的
// `ui.Checkbox` 就是 `t.Space(4)` 见方），这边是表头给它留的位置。少写这一处的话，多选
// 态一切换右边几列整整差 16。
func CheckColWidth(c *ui.Context) float32 { return c.Theme().Space(4) }

// LeadCol 是行最左边那个**没有字**的东西——多选态的勾选框。表头照着留出同样宽的空位，
// 它后面的列才和行里后面那几列对得上。
//
// ⚠️ **封面不走这里**：封面的位置在**序号之后**（行里是「序号 / 封面 / 标题 / 时长」），
// 而这里摆的是**最左边**那一块。把它当 Lead 摆出去，「编号」那一列就落到了封面上
// ——而那种错在画面上只是「表头的编号歪了一点」，一眼看不出是排错了顺序（无头渲染存图
// 时才看出来）。
type LeadCol struct {
	// Check 是多选态那个勾选框。宽度按主题算，见 CheckColWidth——不要写死一个数，那
	// 样主题的间距一变就又对不上了。
	Check bool
}

// HeaderCol 是表头里的一列。
type HeaderCol struct {
	// Title 是列名。**空串是「行里有、这一列没字」**（封面那一列）——它在表头里占着和
	// 封面一样宽的空位，为的是它后面的列和行里后面那几列对得上。
	Title string
	// Width 是这一列多宽。**0 是「标题」那一列**——它 Grow(1) 吃掉剩下的宽。
	Width float32
	// End 报这一列是不是右对齐。数字列（编号 / 播放量 / 时长 / 日期）要右对齐，不然上下
	// 两行的数字左边缘对不齐、竖着扫不下来。
	End bool
}

// HeaderSpec 是这一页表头的样子。每一页给一份，**组件只有 `Header` 那一个**。
type HeaderSpec struct {
	// Key 是这一行表头的身份。同一个父下面给两份一样的 Key 会被框架当错（无头 Tester
	// 当场 panic 并报出代码行号）。
	Key string
	// Lead / Cols 的顺序必须和行里一样，见文件头那一段。
	Lead []LeadCol
	Cols []HeaderCol
	// PadY / PadX / Gap 取自 `MetricsOf`（或那一页自己的行尺寸），**不要另抄**。
	PadY, PadX, Gap float32
}

// CardHeaderSpec 是这个列表（`List` / `PagedList` / `SwitchView` 那一套）此刻该摆的表
// 头：编号 / 标题 / 时长，外加**封面那一列的空位**。check 是此刻在多选态（最左边要给它
// 让出勾选框那一列）。
//
// ⚠️ **列的顺序必须和行里一模一样**：行里是「勾选框 / 编号 / 封面 / 标题 / 时长」
// （`card.go` 的 `viewCard`），所以封面那个空位夹在编号和标题中间。
//
// ⚠️ **本页的行没有播放量和投稿时间两列**，所以表头也没有——理由见文件头那一段。
func CardHeaderSpec(v View, check bool, key string) HeaderSpec {
	m := MetricsOf(v)
	spec := HeaderSpec{
		Key: key,
		Cols: []HeaderCol{
			{Title: "#", Width: m.Index, End: true},
			{Width: m.Cover}, // 封面：行里有、这一列没字
			{Title: "标题"},
			{Title: "时长", Width: m.Clock, End: true},
		},
		PadY: m.PadY, PadX: m.PadX, Gap: m.Gap,
	}
	if check {
		spec.Lead = append(spec.Lead, LeadCol{Check: true})
	}
	return spec
}

// Header 画那一行表头。**七个页面共用它**，每一页给一份 `HeaderSpec`。
func Header(c *ui.Context, spec HeaderSpec) {
	t := c.Theme()
	ui.Row(c).Key(spec.Key).FillWidth().Gap(spec.Gap).Padding(spec.PadY, spec.PadX).
		Children(func() {
			// ⚠️ 下面这些**只占位不画字**的东西（勾选框那一列、封面那一列）存在的全部
			// 理由就是让后面的列对得上。给它们塞一个空字符串会让 Tester 的 `Texts()` 里多出
			// 一堆空串，而「表头对不对得上」这件事是靠 `Find` 拿每一列的盒子量出来的。
			for range spec.Lead {
				ui.Box(c).Width(CheckColWidth(c)).Height(0)
			}
			for _, col := range spec.Cols {
				if col.Title == "" {
					ui.Box(c).Width(col.Width).Height(0)
					continue
				}
				e := ui.Text(c, col.Title).FontSize(headerFont).
					TextColor(t.TextMuted).MaxLines(1)
				if col.Width > 0 {
					e = e.Width(col.Width)
				} else {
					e = e.Grow(1)
				}
				if col.End {
					e = e.TextAlign(ui.End)
				}
			}
		})
}

// Header 画这个列表顶上那一行表头。
//
// ⚠️ **这一层不管「网格不摆表头」**：`List.SetView` 把 `ViewCard` 改写成 `ViewList`
// （一个裸的 `List` 永远不画网格，`switch.go` 文件头写明了为什么），所以这一层根本
// 看不到网格那一档。那个判断在 `SwitchView.Header` 里——只有它知道此刻画的是哪一个。
//
// 每一页在 `Children` 里**先调它再调 `View`**，表头就落在列表外面。
func (l *List) Header(c *ui.Context, key string) {
	Header(c, CardHeaderSpec(l.view, l.HasSelection(), key))
}

// Header 同上，走这一层是因为页面拿到的常常是 `PagedList` 而不是 `List`。
func (p *PagedList) Header(c *ui.Context, key string) { p.surf.Header(c, key) }

// Header 同上。首页那几个列表页、收藏夹、UP 空间和本地音乐拿的都是这一对。
//
// ⚠️ **网格那一档什么都不画**（理由见文件头）——这一层是唯一知道此刻画的是哪一半的
// 地方，所以判断放在这里，页面那边一个 `if` 都不用写。
func (s *SwitchView) Header(c *ui.Context, key string) {
	if s.view == ViewCard {
		return
	}
	s.list.Header(c, key)
}
