// 列表页顶上那一行表头——**组件是 `widgets/list` 里那一个**（`list.Header`），这里
// 只负责把**这一页自己那些行的列**交给它。
//
// # 为什么不是一个函数七个页面各调一次，而是一份规格
//
// 上游 `music-list-item/header.tsx` 是**七个页面在用同一个**组件，列定义放在常量文件
// 里。这一版是照它做的：表头画在哪、怎么对齐，全在 `list.Header` 一处；每一页给一份
// `HeaderSpec`，而**规格里的宽度取自 `metricsOf`——也就是那些行自己用的那一份尺寸**
// （`librarypage.go`）。另抄一份宽度的话，改了行忘了改表头，表头就在那几像素之差上慢
// 慢错开。
//
// # ⚠️ 表头只说这一页**真有**的列
//
// 上游那一行是六列（编号 / 标题 / UP / 播放量 / 投稿时间 / 时长），因为上游的每一行都
// 有那几列。**摆一列这一页没有内容的表头比不摆更糟**，而它糟得很安静：字都在、位置也
// 在，只是下面永远是空的，用户看不出那是没接还是自己记错了。所以：
//
//   - 首页 / 搜索 / 收藏夹 / UP 空间 / 本地音乐：走 `widgets/list` 的行，只有编号 / 标题 /
//     时长三列（`list.CardHeaderSpec`）；
//   - 稍后再看：编号 / 标题 / 播放量 / 加入时间 / 时长（下面那一档）；
//   - 历史记录：编号 / 标题 / 播放进度 / 观看时间（下面那一档）。
//
// 「投稿时间」我们走的是「加入时间 / 观看时间」——那是这一页真有的那个时间，和它的
// 筛选器筛的是同一个（`later.go` 文件头写了为什么）。
//
// # ⚠️ 网格那一档不摆表头
//
// 一格是一首歌，表头上那几列和格子对不上号。判据都在 `widgets/list/header.go` 的那三
// 个 `Header` 方法里，页面这边只判一次 `isGrid`。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// laterHeaderSpec 是「稍后再看」那一页的表头。
//
// ⚠️ **封面那一列的空位夹在编号和标题中间**：行里序号和标题中间隔着 48 的封面，表头不
// 给它留位置，右边那几列就整整差 56（这一页原来那份表头就差在这里——它把「标题」摆在序
// 号后面）。
func laterHeaderSpec(mt listMetrics, key string) list.HeaderSpec {
	return list.HeaderSpec{
		Key: key,
		Cols: []list.HeaderCol{
			{Title: "#", Width: mt.index, End: true},
			{Width: mt.cover}, // 封面：行里有、这一列没字
			{Title: "标题"},
			{Title: "播放量", Width: mt.count, End: true},
			{Title: "加入时间", Width: mt.date, End: true},
			{Title: "时长", Width: mt.clock, End: true},
		},
		PadY: mt.padY, PadX: mt.padX, Gap: mt.gap,
	}
}

// historyHeaderSpec 是「历史记录」那一页的表头。
//
// 和稍后再看差的是右边那两列：历史记录的接口不给播放量，给的是「看了多少」和「什么时候
// 看的」（`libraryRow.View` / `Progress` / `ViewedAt`）。
func historyHeaderSpec(mt listMetrics, key string) list.HeaderSpec {
	return list.HeaderSpec{
		Key: key,
		Cols: []list.HeaderCol{
			{Title: "#", Width: mt.index, End: true},
			{Width: mt.cover}, // 封面：行里有、这一列没字
			{Title: "标题"},
			{Title: "播放进度", Width: mt.progress, End: true},
			{Title: "观看时间", Width: mt.viewedAt, End: true},
		},
		PadY: mt.padY, PadX: mt.padX, Gap: mt.gap,
	}
}

// viewLibraryHeader 画这两页共用的那一行表头。
//
// ⚠️ **网格那一档什么都不画**（理由见文件头）。两页都照这一句调：
//
//	if !p.rows.isGrid() {
//	    viewLibraryHeader(c, p.rows, laterHeaderSpec, "later:header")
//	}
//
// 规格是一个**值**而不是一个页面方法：两页的列差在右边那两列，把它们并进一个函数里就
// 变成一串 if-else，而表头是最容易被改坏、又最难看出来的部件。
func viewLibraryHeader(c *ui.Context, rows *pagedRows,
	spec func(listMetrics, string) list.HeaderSpec, key string) {
	if rows.isGrid() {
		return
	}
	list.Header(c, spec(metricsOf(rows.Layout()), key))
}

// viewRowIndex 是「稍后再看」「历史记录」这两页那一行最左边：平时是序号，**正在放的那
// 一行换成一段跳动的波形**。
//
// 和 `widgets/list` 的 `viewIndex` 是同一件事，只有列宽取自各自那一行。
//
// ⚠️ **key 用的是 `m.ID` 不是歌名**：铁律四那条——行的身份必须是稳定且唯一的那个。歌名
// 会重复（同一首歌在两个收藏夹里各一行），而用歌名当 key 时两行共用一份波形状态。
//
// ⚠️ **color 是调用方给的**（就是这一行歌名的那个颜色）：波形摆的那一格在选中时是**主
// 色底**，主色底上画一条主色波形等于没画。
func viewRowIndex(c *ui.Context, i int, playing bool, color ui.Color, id, title string, width float32) {
	if playing {
		hover.Wave(c, "wave:"+id, width, color, title)
		return
	}
	ui.Text(c, rowNumber(i)).Width(width).TextAlign(ui.End).
		FontSize(scale.Font(c, 12)).TextColor(c.Theme().TextMuted).MaxLines(1)
}
