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

// playingMark 是「这一行正在放」那个标记。Biu 那一行画的是一段跳动的波形，
// 这里写死成一句字：⚠️ **它不跳**——跳要一帧一变，而快速切歌时帧率本来就紧，
// 而帧率一掉「正在放」就跟着糊掉。认出来靠的是「只有一行是这个颜色 + 只有
// 这一行有这句字」，不靠动。
const playingMark = "正在播放"

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
	playing := l.isPlaying(m)

	titleColor, ownerColor := t.Text, t.TextMuted
	switch {
	case r.Selected():
		// 主色底上的字。UP 名那一行压到 0.75，主色底太亮时它比正文色更
		// 站得住，也还是「次要信息」的样子。
		titleColor, ownerColor = t.AccentText, t.AccentText.Alpha(0.75)
	case playing:
		// 「正在放」和「选中」是**两件事**：选中跟着光标走（点哪行亮哪行），
		// 正在放跟着声音走（声音从哪来亮哪行）。滚歌单的时候光标停在上头，
		// 正在放的往往在屏幕中间——不分开的话用户得靠播放条才知道放的是哪首。
		titleColor = t.Accent
	}

	// ⚠️ **hover 必须在 Children 之前问**：`Hovered` 报的是**这一帧**指针在不在
	// 上面，而「按下去出现这个按钮」这件事本身要等 Children 建它。MyGo 自己
	// 的例子也是这个写法（`ui/ui_test.go` 的 TestHoverWhilePressed）。
	row := ui.Row(c).AlignSelf(ui.Stretch).Gap(cardGap).Padding(cardPadY, cardPadX).
		// 右键菜单挂在**这一行**上：MyGo 取的是最里层那个带菜单的元素
		//（`ui/menu.go` 的 menuTarget），挂到列表外面的话整列共用一份菜单。
		ContextMenu(func(m *ui.Menu) { l.buildMenu(m, i) })
	hover := row.Hovered()

	row.Children(func() {
		// ⓪ 多选态：这一行最左边的勾选框。没装多选就一格都不画
		//（见 SetSelection）。
		l.viewCheckbox(c, i, m.Title)

		// ① 序号，或者（指针在这一行上时）那个「播放」按钮。见 viewIndex。
		l.viewIndex(c, i, hover && !playing)

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

		// ⑤ 正在放的那一行在末尾挂一句「正在播放」。
		//
		// ⚠️ 它认的是**歌**不是行（`isPlaying` 按判重标识，见 Actions.IsPlaying）：
		// 换歌之后它跟着换，而正在放的歌在别的一行时这一行就退回一个普通的
		// 时长。用行的序号认的话，「下一首播放」一插队它就会钉在错的那一行上。
		//
		// ⚠️ **无障碍标签带上了歌名**（读屏软件读的是标签，不是画出来的那几个
		// 字）：光说「正在播放」的话，用户听到的是列表里突然冒出来的一句没头没尾
		// 的话，他得自己猜是哪一首。顺带它也让这一行在 Tester 里可认——
		// 同一首歌排了两行时，两个标签各不相同（铁律四保证 ID 不同）。
		if playing {
			ui.Text(c, playingMark).Label(m.Title + " " + playingMark).
				FontSize(11).TextColor(t.Accent).MaxLines(1)
		}
	})
}

// isPlaying 报这一首是不是正在放的那首。判据和理由见 shared.go 的同名函数。
func (l *List) isPlaying(m Music) bool { return isPlaying(l.acts, m.Item) }

// viewIndex 是那一行最左边：平时是序号，指针移上来时是「播放」按钮。
//
// **按了就是从这一首往后接进队列并开始播**——和双击同一件事（见 `View` 的
// `OnSubmit`）。hover 出来的按钮跟双击说的不是一回事的话，用户第一次按就会
// 拿到一个自己没点过的动作。
//
// ⚠️ **正在放的那一行不摆它**：按下去是同一首从头重播（见 `App.PlayAt`
// 开头那一支，它开头就 return 了）。摆一个「按了等于没换歌」的按钮不如摆序号。
//
// 用序号那一列的 `indexWidth`，所以按钮出现时那一行不会横向跳一下。
//
// ⚠️ **内边距收到 4**：MyGo 的按钮左右各留 `Space(3.5)`（14），「播放」两个
// 汉字约 26——加起来 54，而 `indexWidth` 只有 40。第一版没收，画出来的是
// **10px 宽的一个「播」字**，而**无头 Tester 读的是控件的文本**：所有断言一律
// 是绿的，「播放」两个字在屏幕上只剩一个（`TestTheHoverButtonIsNotTruncated`
// 量的是画出来的那几个字，见那里）。
func (l *List) viewIndex(c *ui.Context, i int, hover bool) {
	if !hover {
		ui.Text(c, indexText(i)).Width(indexWidth).TextAlign(ui.End).
			FontSize(12).TextColor(c.Theme().TextMuted).MaxLines(1)
		return
	}
	if ui.Button(c, "播放").Width(indexWidth).Padding(1, 4).MaxLines(1).
		Tooltip("从这一首往后听").Clicked() {
		l.playFrom(i)
	}
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
