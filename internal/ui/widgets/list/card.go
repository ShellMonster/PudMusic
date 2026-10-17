// 音乐卡片：一行长什么样。
//
// 结构照 Biu 的 `src/components/music-list-item/index.tsx`（那一行的七列网格
// 里取这里用得上的几列）：序号、封面、标题、UP 名、时长。封面用已经建好的
// `widgets/imgcache`，它管空地址 / 载入中 / 就绪 / 失败四态，这里不重复画
// 占位（重复画就会出现两个占位，或者一个图上面盖着一个框）。
//
// ⚠️ 有一件事是这张卡片存在的技术理由：**行高不能写死**。歌名一行摆不下时
// 折到第二行（Biu 的 `line-clamp-2`），于是歌名长的行比歌名短的行高，整张
// 列表的行高就不一样了。这正是变高行的真实场景——`ui.List` 会按实际量出来的
// 高度校正它对未量到的行的估计（`mygo/ui/list.go` 的 listHeights）。
//
// 如果这里图省事写 `Height(rowHeight)` 把行高钉死，变高行的那条路就没人走了：
// 几千行里只要有一行内容比钉死的高，它的内容会被裁掉或者溢出到下一行上，
// 而且这种错在几屏之内看不出来。宁可多量一次。
package list

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
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

	// titleLines 是歌名最多几行。2 是 Biu 的 `line-clamp-2`。
	titleLines = 2

	// textGap 是歌名和 UP 名之间的距离。Biu 是 `mt-1`（4）。
	textGap = 4

	// titleLeading 是歌名的行高倍数。1.5 是 Tailwind 的默认行高，也是 Biu 那边
	// `text-base` 的行高。
	//
	// ⚠️ 这一条不只是好看，它决定两行歌名的那一块**比 48 的封面高多少**：
	// MyGo 的默认行高约 1.18，两行歌名 + UP 名的文字块是 51，比封面只高 3，
	// 两行的歌名挤在封面的边上（实测行高 67 对 64）；用 1.5 之后文字块是 60，
	// 实测行高 76 对 64——差得开才像两行，而不是「挤在一起的两行」。
	titleLeading = 1.5

	// ownerLeading 是 UP 名的行高倍数。1 是 Biu 那一行的 `leading-none`。
	ownerLeading = 1
)

// viewCard 画一行：Biu 的 music-list-item 在 MyGo 里的样子。
//
// ⚠️ 选中那一行的底色和字色由 `ui.List` 自己刷（主色底 + 主色上的反色字，
// `ui/table.go` 的 node.Selected），这里**不要**再给文字设固定颜色——一设就
// 盖掉它，选中行会变成主色底上的深色字，深色主题下直接看不见。所以正文色是
// 跟着选中与否给的。
func (l *List) viewCard(r ui.ListRow) {
	c, i := r.Context, r.Index
	t := c.Theme()
	m := l.songs[i]

	titleColor, ownerColor := t.Text, t.TextMuted
	if r.Selected() {
		// 主色底上的字。UP 名那一行压到 0.75，主色底太亮时它比正文色更
		// 站得住，也还是「次要信息」的样子。
		titleColor, ownerColor = t.AccentText, t.AccentText.Alpha(0.75)
	}

	ui.Row(c).AlignSelf(ui.Stretch).Gap(cardGap).Padding(cardPadY, cardPadX).
		// 右键菜单挂在**这一行**上：MyGo 取的是最里层那个带菜单的元素
		//（`ui/menu.go` 的 menuTarget），挂到列表外面的话整列共用一份菜单。
		ContextMenu(func(m *ui.Menu) { l.buildMenu(m, i) }).
		Children(func() {
			// ⓪ 多选态：这一行最左边的勾选框。没装多选就一格都不画
			//（见 SetSelection）。
			l.viewCheckbox(c, i, m.Title)

			// ① 序号。定宽 + 靠右（贴着封面），1 和 1000 在同一条竖线上，
			// Biu 的 `min-w-8 text-center` 是同一个效果。
			ui.Text(c, indexText(i)).Width(indexWidth).TextAlign(ui.End).
				FontSize(12).TextColor(t.TextMuted).MaxLines(1)

			// ② 封面。四态由 imgcache.View 画（空地址画空框，不画「失败」）。
			imgcache.View(c, l.cache, m.Cover, coverSize, coverSize)

			// ③ 标题 + UP 名。这一列 Grow(1)，把剩下的宽度都吃掉。
			//
			// ⚠️ 别给这两块设固定高度（Height/SingleLine）：那样整张列表的行高
			// 就是常数，变高行没得走（见文件开头）。
			ui.Column(c).Grow(1).AlignItems(ui.Start).Gap(textGap).Children(func() {
				ui.Text(c, m.Title).FontSize(14).LineHeight(titleLeading).
					TextColor(titleColor).MaxLines(titleLines).Ellipsis("…")
				if m.Owner != "" {
					ui.Text(c, m.Owner).FontSize(12).LineHeight(ownerLeading).
						TextColor(ownerColor).MaxLines(1).Ellipsis("…")
				}
			})

			// ④ 时长。没有就不画——画一列空白比不画更难看。
			if s := formatDuration(m.Duration); s != "" {
				ui.Text(c, s).Width(durationWidth).TextAlign(ui.End).
					FontSize(12).TextColor(t.TextMuted).MaxLines(1)
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
func (l *List) viewCheckbox(c *ui.Context, i int, title string) {
	if l.sel2.On == nil {
		return
	}
	checked := l.sel2.On(i)
	box := ui.Checkbox(c, &checked, "")
	if box.Changed() && l.sel2.Toggle != nil {
		l.sel2.Toggle(i)
	}
	box.Label("选择 " + title)
}
