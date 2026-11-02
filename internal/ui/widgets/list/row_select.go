// 「行标题那一列可选中，拖选复制整行」这一族（票 #703 的 A）。
//
// # 为什么是**容器级** `Selectable`
//
// 用户想复制的是**一整行**（歌名 + UP 名 + 时长），不是「歌名那几个字」。
// 框架的容器级 `Selectable` 正是干这个的：「它的 Text 后代共用一个选择，
// 复制时用换行连着」（`mygo/ui/api_gen.go` 的 `Selectable`）。
//
// ⚠️ **逐行各自 `Selectable` 不行**：那样作用域是每行一个，拖到下一行时上一行
// 会掉出去（用户得复制三次）。
//
// # 🔴 **两层各管一摊，不能合成一层**
//
// 框架把几样东西揉在同一个元素上，`Selectable` 一加就全要重新对位（下面每条
// 都有实测）。
//
//	外层（`HandleInput`）	接「单击选中 / 双击播放」（`rowTextInput`），
//		下一帧还焦点（`takeFocusIfIdle`）
//	内层（`Selectable` + `ContextMenu` + `Bind` + `CopyData`）	作用域 = 内层，
//		复制整行；右键菜单也挂这层（见下）
//
//	行（flagClickable + ContextMenu）
//	└─ 外层列（HandleInput）
//	   └─ 内层列（Selectable + ContextMenu + CopyData）
//	      └─ 歌名 / UP 名（Text）
//
// 🔴 **`HandleInput` 和 `Selectable` 放同一层是错的**（实测过，不是猜的）：
// 框架找作用域是从字往上走，遇到祖先带 `inputFn` 就**停**
// （`mygo/ui/textselection.go` 的 `prepareSelectable`）——两层合一的话它撞上
// 的第一个祖先就是自己这层，作用域塌掉，**拖选根本建立不起来选区**
// （`SelectedTexts` 永远是空，复制出空）。
//
//	手势	按下被框架的 `pressTextSelection` 接走之后，`rt.pressed` 落在
//		**那几段字**上，行的 `listRowValueInput` / 格子的 `OnClick` 收不到
//		`Clicked()` / `DoubleClicked()`——于是「单击选中 / 双击播放」两种
//		手势得自己接回来（`rowTextInput`）。
//
//	菜单	右键的 `menuTarget`（`mygo/ui/menu.go`）从命中链里往外找**第一个**
//		带 `flagContextMenu|flagEditable|flagSelectable` 的元素——内层带着
//		`flagSelectable`，它必赢。⇒ **把 `ContextMenu` 挂到内层**：框架的
//		`textMenu`（`mygo/ui/menu.go`）对「Selectable 容器带菜单」有现成的
//		让路——文字上右键开的是**容器的菜单**（文档原话：ContextMenu
//		"replaces … on the Selectable container whose text was clicked"）。
//		不挂的话，右键标题弹的是框架的「拷贝 / 全选」编辑菜单（实测）。
//
//	焦点	按下在**文字上**时框架把 `rt.focused` 强制设成那段字，⌘C 两条路
//		（`keyDownPhysical` / `editEvent`）都走 `focusHandler()`；焦点在字上、
//		又没选中字时，框架把 copy 吃掉且不写剪贴板——**列表永远收不到**。
//		⇒ 下一帧把焦点还回去（`takeFocusIfIdle`）；拖选不受影响（拖完有选区，
//		不还，⌘C 走 `copyData` 复制整行）。⌘C 本身由列表 / 网格那一层的输入
//		回调接（`copy_title.go` / `grid.go` 的 `gridInput`）。
//
// ⚠️ **行 / 格子的 `ContextMenu` 保留**：右键落在**文字之外**的地方（封面、
// 空白）时，命中链里没有这一列，`menuTarget` 找到的是行 / 格子——两处是同一
// 份菜单闭包，语义一致。
//
// # ⚠️ **判据在「按下」这一头，不在 `Clicked()` 上**：框架的 `pointerUp` 只给
// **被按下那个元素**的 `clicks++`，而 `pressTextSelection` 把 `rt.pressed`
// 设成了**那几段字**——行的 `listRowValueInput` 永远收不到，`Clicked()` 也就
// 永远不响。
//
// # ⚠️ **`Clicks >= 2` 而不是 `== 2`**：连按三下的时候框架报 3
// （`mygo/ui/input.go` 的 `countPress`），而用户的意思显然还是「双击」。
package list

import (
	"strings"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// rowTextInput 是**可选中那一列**的输入回调（票 #703 的 A）。
//
// # 它接回来的是**两个**手势，焦点还要还回去
//
//	按下（单击）	那一行被选中，焦点下一帧交回列表 / 网格
//	双击（连按）	播放那一首
//
// ⚠️ **这两个都得自己接**：按下被框架的 `pressTextSelection` 接走之后，
// `rt.pressed` 落在**那几段字**上——行 / 格子的 `Clicked()` /
// `DoubleClicked()`（`mygo/ui/value_input.go`、`OnClick`）全都收不到。
//
// ⚠️ **双击在这里吃掉（返回 true）**：吃掉之后框架的文字选择不再跑——
// 双击的意图是「播放」，顺带把一个词选中反而是噪音。单击**不吃**（返回
// false）：文字选择（拖选的起点）照旧发生，两件事不冲突。
//
// ⚠️ **`Clicks >= 2` 而不是 `== 2`**：连按三下的时候框架报 3
// （`mygo/ui/input.go` 的 `countPress`），而用户的意思显然还是「双击」。
//
// # 🔴 **焦点必须还回去**
//
// 框架把一次按下交给**命中链上最里层那个带 `inputFn` 的元素**，而按下在
// **文字上**时 `pressTextSelection` 会把 `rt.focused` 强制设成**那几段字**——
// 焦点从此不在列表上，⌘C 两条路（`keyDownPhysical` / `editEvent`）都走
// `focusHandler()`，到不了列表 / 网格那一层；而且焦点在字上、没选中字的时候，
// 框架把 copy **吃掉且不写剪贴板**（`mygo/ui/textselection.go` 的
// `textSelectionCommand`：`!selected → break`，最后 `return true`）——
// **列表永远收不到**（实测：`TestCmdCCopiesTheCleanedTitle` 复制出空）。
//
// ⇒ 所以 `selectRow` 里要调 `Handle.Focus()`（列表 / 网格那一层的）。它是
// **下一帧生效**（`mygo/ui/ref.go`：`requested = true` + 要一帧）：单击之后
// 焦点回到列表，⌘C 走回列表那一层。**拖选不受影响**：拖选结束时框架把焦点
// 落在字上（用户在选字），⌘C 走 `copyData` 复制整行——两种语义各自正确。
//
// # 🔴 **必须把这一下也转给列表那一层**
//
// 列表 / 网格那一层的输入回调（`l.input` / `g.gridInput`）干的第一件事是
// **`keys.Input(ev)`**——`KeyboardMode` 靠它记「现在是键盘在管还是指针在管」，
// **焦点环就靠那个记号**（`selection.go` 的 `On()`）。不转过去的话：指针点
// 一下之后 `KeyboardMode` 仍然以为「键盘在管」⇒ **那一行挂上了焦点环**——
// 而用户用鼠标点过之后不该有环（实测：`TestClickingARowLeavesNoFocusRing`）。
//
// ⚠️ **转过去的返回值要透传**：⌘C 到这一层时（焦点在这一列上），列表那一层
// 复制完返回 true——这一层不吃掉它的话，框架会再走一遍窗口级的命令查找。
func rowTextInput(selectRow, play func(), forward func(ui.InputEvent) bool, ev ui.InputEvent) bool {
	// ⚠️ **先转给列表那一层**：它要听见这一下（`KeyboardMode` 靠它）。
	handled := false
	if forward != nil {
		handled = forward(ev)
	}
	if ev.Kind != ui.InputPointerDown || ev.Button != 0 {
		return handled
	}
	// ⚠️ **单击和双击都先选中**：双击的第一下已经选中过，第二下再设一次是
	// 幂等的；而双击后焦点同样该回到列表 / 网格（「焦点要不要还」走同一条
	// 下一帧的判据，见 `takeFocusIfIdle`）。
	if selectRow != nil {
		selectRow()
	}
	if ev.Clicks >= 2 {
		if play != nil {
			play()
		}
		return true
	}
	return handled
}

// rowCopyData 是**行标题那一列**的 `CopyData` 回调（票 #703 的 A）。
//
// # 它复制的是**整行**，不是「用户拖到了哪几段」
//
// 票面 A 项要的是「一次复制**标题 + UP 名 + 时长**一整段」——用户拖的是歌名，
// 要的是**那一行**。所以这一列自己拼那一行，不看用户拖到了哪几段。
//
// ⚠️ **而它必须挂在**那个 `Selectable` 的容器**上**：框架找作用域时判的是
// `p.kind == kindBox && p.flags&flagSelectable != 0`（`prepareSelectable`），
// 而 `CopyData` 是**按作用域**找的（`textSelectionCommand` 的
// `sc := rt.states[s.textScope]`）——挂在别的元素上不生效（同样的道理见
// `internal/ui/comments_body.go` 的 `bodyBox`）。
//
// ⚠️ **parts 为空时框架不会调它**（`textSelectionCommand` 里 `!selected` 就
// `break`）——所以这里不用管「一个字都没选中」的情形。
func rowCopyData(sh Shown, duration string) func([]ui.SelectedText) transfer.Data {
	return func([]ui.SelectedText) transfer.Data {
		return transfer.TextData(rowTextOf(sh, duration))
	}
}

// rowTextOf 是**那一行复制出去的那一段字**（歌名 + UP 名 + 时长，用换行连着）。
//
// ⚠️ **这是「整行」，不是「用户拖到了哪几段」**：票面 A 项要的是「一次复制
// **标题 + UP 名 + 时长**一整段」——用户拖的是歌名，要的是那一行。
func rowTextOf(sh Shown, duration string) string {
	var out strings.Builder
	out.WriteString(sh.Title)
	if sh.Owner != "" {
		out.WriteByte('\n')
		out.WriteString(sh.Owner)
	}
	if duration != "" {
		out.WriteByte('\n')
		out.WriteString(duration)
	}
	return out.String()
}

// rowSelHandle 报第 i 行标题那一列的句柄（票 #703 的 A）。
//
// ⚠️ **每一行一个**：框架的 `Bind` 同一个句柄在一次 build 里绑两个元素会 panic
// （`mygo/ui/ref.go` 的 `Bind`：「Handle bound to two controls in one build」），
// 而每一行都要有自己的一个。
//
// ⚠️ **它按歌的下标编号**：调用方给的是「歌的下标」（不是列表的行号）——
// 页内头部占着第 0 行（见 lead.go），而头部那一行没有可选中的文字列。
// 负的下标到不了这里（`viewRow` 先拦了 lead/footer），到了就是调用方的错。
func (l *List) rowSelHandle(i int) *ui.Handle {
	for len(l.rowSel) <= i {
		l.rowSel = append(l.rowSel, ui.Handle{})
	}
	return &l.rowSel[i]
}

// rowSelHandle 报第 i 格标题那一列的句柄（票 #703 的 A，网格那一档）。
//
// ⚠️ **每一格一个**：理由同 `List` 那一个（框架的 `Bind` 同一个句柄绑两个
// 元素会 panic）。
func (g *Grid) rowSelHandle(i int) *ui.Handle {
	for len(g.rowSel) <= i {
		g.rowSel = append(g.rowSel, ui.Handle{})
	}
	return &g.rowSel[i]
}

// rowWiring 记**第 i 行（格）那一列**挂的四样回调（票 #703 的 A）。
//
// # 🔴 **为什么记着，不每帧现建**
//
// 那四样（`HandleInput` / `ContextMenu` / `CopyData`，加上里面包着的
// selectRow / play）全是**闭包**——每帧每行现建的话，一屏十几行就是几十次
// 分配，首页那一档的逐帧分配预算直接爆掉（实测：`framebudget_test.go` 的
// 「首页列表」档 80 次 / 2564 字节，上限 60 次 / 1800）。⇒ 照 `durText` /
// `ownerTip`（票 #313）的同一招：**记好的那一份**，命中就不再建。
//
// ⚠️ **闭包体内全部现算**（`l.songs[i]` / `leadRows(l.lead)`）：歌换了、
// 重排了，闭包读到的都是**当态**——所以这份记号**不用失效**（和 `rowSel`
// 一个策略）。
//
// ⚠️ **`copy` 是低频路径**（用户真按了 ⌘C / 右键复制才跑）：里面现拼一次
// 字符串没有关系，不为它建缓存。
type rowWiring struct {
	input func(ui.InputEvent) bool
	menu  func(*ui.Menu)
	copy  func([]ui.SelectedText) transfer.Data
}

// newRowWiring 建**列表第 i 行**的那一组（票 #703 的 A）。
//
// ⚠️ **`selectRow` 里把焦点交回列表**（`Handle.Focus`，下一帧生效）：按在
// 文字上时框架把焦点落在字上，不还回去的话 ⌘C 到不了列表那一层（实测复制出
// 空）。**拖选也还**：拖选建立的选区与焦点无关（框架的拖选跟着 `rt.pressed`
// 走），而 ⌘C 由列表那一层接（`copy_title.go` 的兜底）——复制出去的同样是
// **整行**，而且走的是**受占号保护的那条写剪贴板的通道**（`internal/ui` 的
// `copyClip`；框架的 `copyData` 直写系统剪贴板，会被在途的封面复制盖掉——
// 票 #685 的对抗性审查结论）。
func newRowWiring(l *List, i int) *rowWiring {
	w := &rowWiring{}
	w.input = func(ev ui.InputEvent) bool {
		return rowTextInput(
			func() {
				l.sel = i + leadRows(l.lead)
				l.state.Handle.Focus()
			},
			func() { l.rowPlay(i) },
			l.input,
			ev)
	}
	w.menu = func(m *ui.Menu) { l.buildMenu(m, i) }
	w.copy = func([]ui.SelectedText) transfer.Data {
		m := l.songs[i]
		return transfer.TextData(rowTextOf(ShowOf(m), formatDuration(m.Duration)))
	}
	return w
}

// wiringAt 报**列表第 i 行**记好的那一组（票 #703 的 A）。
func (l *List) wiringAt(i int) *rowWiring {
	for len(l.wiring) <= i {
		l.wiring = append(l.wiring, nil)
	}
	if l.wiring[i] == nil {
		l.wiring[i] = newRowWiring(l, i)
	}
	return l.wiring[i]
}

// newCellWiring 建**网格第 i 格**的那一组（票 #703 的 A）。
//
// ⚠️ **`selectRow` 是原 `OnClick` 那一套**（选中 + 把焦点交给网格，票 #453；
// 理由同上：按在文字上时框架把焦点落在字上，格子的 `OnClick` 收不到 release）。
func newCellWiring(g *Grid, i int) *rowWiring {
	w := &rowWiring{}
	w.input = func(ev ui.InputEvent) bool {
		return rowTextInput(
			func() {
				g.sel = i
				g.state.Handle.Focus()
			},
			func() { g.rowPlay(i) },
			g.gridInput,
			ev)
	}
	w.menu = func(m *ui.Menu) { buildSongMenu(m, g.songs, g.acts, i, g.playFrom) }
	w.copy = func([]ui.SelectedText) transfer.Data {
		m := g.songs[i]
		return transfer.TextData(rowTextOf(ShowOf(m), formatDuration(m.Duration)))
	}
	return w
}

// wiringAt 报**网格第 i 格**记好的那一组（票 #703 的 A）。
func (g *Grid) wiringAt(i int) *rowWiring {
	for len(g.wiring) <= i {
		g.wiring = append(g.wiring, nil)
	}
	if g.wiring[i] == nil {
		g.wiring[i] = newCellWiring(g, i)
	}
	return g.wiring[i]
}

// selectedRowText 报这一段选中**真的选中了字没有**。
//
// ⚠️ **`SelectedTexts` 非空不代表选中了字**：它判的是「这一列有没有选择」
// （`mygo/ui/textinput.go` 的 `SelectedTexts`），而单击（不拖）也会留一个
// anchor = caret 的空选择——拿它当「用户拖选了」的话，⌘C 的歌名兜底会被
// 一段空文本短路掉（实测：复制出空）。
func selectedRowText(parts []ui.SelectedText) string {
	var out strings.Builder
	for _, p := range parts {
		if p.Text == "" {
			continue
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(p.Text)
	}
	return out.String()
}

// rowSelection 读**当帧**那一段文字选中（票 #703 的 A，列表那一档）。
//
// ⚠️ **为什么挨个问**：框架全局只有一段选中（`mygo/ui/textselection.go` 的
// `rt.selection`，一个 scope 一个），而「拥有它的那一列」是哪一个，框架没告诉
// anybody——所以挨个问一遍「选中在你那儿吗」。
//
// 🔴 **只问画面上的那几行**（对抗性审查抓的）：`rowSel` 只增不减（和
// `rowSelTextOf` 一个策略），挨个问**全部**的话，几千行的库页面上就是每帧
// 几千次 `Resolve`——随数据规模而不是随视口涨。⇒ 用 `state.Visible()` 拿
// 顶行 / 底行的**列表行号**（含页内头部那几行，减掉 `leadRows` 才是歌的
// 下标），只扫视口。
//
// ⚠️ **判的是「选中了字」，不是「有选择」**（见 `selectedRowText`）。
//
// ⚠️ **每帧读一次**：选中会自己变（选中变了、段落被重建了、那一段没了）。
//
// ⚠️ **读不到就是没选中**：那一列这一帧没被建出来（虚拟化滚走了）时
// `Context.Resolve` 给一个空元素，读出来是空的——那是对的：那段字已经不在
// 屏幕上了。
func (l *List) rowSelection(c *ui.Context) {
	l.rowSelParts = nil
	l.rowSelText = ""
	first, last := l.state.Visible()
	lead := leadRows(l.lead)
	from := max(first-lead, 0)
	to := min(last-lead, len(l.rowSel)-1)
	for i := from; i <= to; i++ {
		e := c.Resolve(l.rowSel[i])
		if !e.Valid() {
			continue
		}
		parts := e.SelectedTexts()
		if selectedRowText(parts) != "" {
			l.rowSelParts = parts
			l.rowSelText = l.rowSelTextOf(i)
			return
		}
	}
}

// rowSelection 读**当帧**那一段文字选中（票 #703 的 A，网格那一档）。
//
// ⚠️ **为什么挨个问 / 只问视口**：理由同 `List` 那一档（框架全局只有一段
// 选中；`rowSel` 只增不减）。
//
// ⚠️ **网格一行几格**：`Visible` 给的是**网格的行号**（一格一行时才恰好等于
// 歌的下标），乘上列数才是这一行装着的**格下标**区间——和 `ScrollIntoView`
// 那个换算（`scrollCursorIntoView`）是同一个算法。
func (g *Grid) rowSelection(c *ui.Context) {
	g.rowSelParts = nil
	g.rowSelText = ""
	first, last := g.state.Visible()
	lead := leadRows(g.lead)
	cols := g.gridCols()
	from := max((first-lead)*cols, 0)
	to := min((last-lead+1)*cols-1, len(g.rowSel)-1)
	for i := from; i <= to; i++ {
		e := c.Resolve(g.rowSel[i])
		if !e.Valid() {
			continue
		}
		parts := e.SelectedTexts()
		if selectedRowText(parts) != "" {
			g.rowSelParts = parts
			g.rowSelText = g.rowSelTextOf(i)
			return
		}
	}
}

// rowSelTextOf 是**列表第 i 行**复制出去的那一段字（票 #703 的 A）。
//
// ⚠️ **i 是「歌的下标」**（不是列表的行号）：页内头部占着第 0 行
// （见 lead.go），而 `rowSel` 那一族句柄也是按歌的下标编号的。
func (l *List) rowSelTextOf(i int) string {
	if i < 0 || i >= len(l.songs) {
		return ""
	}
	m := l.songs[i]
	return rowTextOf(ShowOf(m), l.durText.At(i, m.Duration, formatDuration))
}

// rowSelTextOf 是**网格第 i 格**复制出去的那一段字（票 #703 的 A）。
//
// ⚠️ **i 是「格的下标」**，而格子里装的就是那一首（见 `viewCell`）。
func (g *Grid) rowSelTextOf(i int) string {
	if i < 0 || i >= len(g.songs) {
		return ""
	}
	m := g.songs[i]
	return rowTextOf(ShowOf(m), g.durText.At(i, m.Duration, formatDuration))
}
