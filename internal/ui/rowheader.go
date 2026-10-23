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
//   - 首页：编号 / 标题 / 播放量 / 时长（`list.Columns` 只开了播放量——上游首页
//     也是 `hidePubTime`）；
//   - 搜索的歌曲 / 视频、UP 空间投稿：编号 / 标题 / 播放量 / 投稿时间 / 时长；
//   - 搜索的用户：编号 / 标题（两样都没有）；
//   - 收藏夹 / 合集 / 系列：编号 / 标题 / 播放量 / 收藏时间或投稿时间 / 时长
//     （`list.Columns` 的 `PubTimeTitle`，见 `collectionpage.go`）；
//   - 本地音乐：编号 / 标题 / 时长（两样都没有）；
//   - 稍后再看：编号 / 标题 / 播放量 / 加入时间 / 投稿时间 / 时长（下面那一档；
//     窄窗口下两个时间列按 `laterTimeCols` 依次让位给标题和行尾的「⋯」）；
//   - 历史记录：编号 / 标题 / 播放进度 / 观看时间（下面那一档）。
//
// ⚠️ **「加入时间」和「投稿时间」是两个时间**（稍后再看那一页两列都画）：
// 前者是这一页的日期筛选筛的那个 `add_at`，后者是上游画的那个 `pubdate`
// （`later.go` 文件头写了为什么两个都留）。
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
// ⚠️ **封面不进表头**（DESIGN.md：表头的「标题」从封面左缘起）：行里是「序号 / 封面 /
// 标题」，表头是「序号 / 标题」。「标题」那一列 Grow(1) 吃掉左边少掉的宽，右边那几列
// 照样和行里对齐（理由见 `widgets/list/header.go` 的 `CardHeaderSpec`）。
func laterHeaderSpec(mt listMetrics, key string, showPubDate, showAddTime bool) list.HeaderSpec {
	cols := []list.HeaderCol{
		{Title: "#", Width: mt.index, Center: true},
		{Title: "标题"},
		{Title: "播放量", Width: mt.count, End: true},
	}
	// 两个时间列都由 `laterTimeCols` 决定画不画（行那一处读的是同一个
	// 判据）：窄窗口先收「投稿时间」，连基线都放不下时再收「加入时间」。
	if showAddTime {
		cols = append(cols, list.HeaderCol{Title: "加入时间", Width: mt.date, End: true})
	}
	// 投稿时间：上游这一页画的是 `pubdate`（票 #142）。它和「加入时间」
	// 是两个时间——那一个是筛选器筛的 `add_at`，这一列才是「哪天发的」。
	if showPubDate {
		cols = append(cols, list.HeaderCol{Title: "投稿时间", Width: mt.pubDate, End: true})
	}
	cols = append(cols, list.HeaderCol{Title: "时长", Width: mt.clock, End: true})
	return list.HeaderSpec{
		Key:  key,
		Cols: cols,
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
			{Title: "#", Width: mt.index, Center: true},
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
//	    viewLibraryHeader(c, p.rows, func(mt listMetrics, key string) list.HeaderSpec {
//	        pubDate, addTime := laterTimeCols(p.rows)
//	        return laterHeaderSpec(mt, key, pubDate, addTime)
//	    }, "later:header")
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
	ui.Text(c, rowNumber(i)).Width(width).TextAlign(ui.Center).
		FontSize(scale.Font(c, scale.Size12)).TextColor(c.Theme().TextMuted).MaxLines(1)
}
