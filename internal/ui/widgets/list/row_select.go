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
// # 🔴 **三件事必须分开：作用域 / 手势 / 焦点**
//
// 框架在一个元素上揉了三样东西，于是「给行标题加 `Selectable`」会连带弄坏
// 三种手势（下面逐条记，理由都在实测里）。
//
//	作用域	框架找它是从字往上走，遇到祖先带 `inputFn` 就**停下**
//		（`mygo/ui/textselection.go` 的 `prepareSelectable`）——所以
//		**可选中那一列必须和带 `inputFn` 的那一层分开**：
//
//		行（flagClickable）
//		└─ 那一列（Selectable）	← 作用域是它
//		   └─ 歌名 / UP 名（Text）
//
//	手势	按下被框架的 `pressTextSelection` 接走之后，行的
//		`listRowValueInput` 收不到 `Clicked()`——于是「单击选中 / 双击播放」
//		两种手势得自己接回来（`rowTextInput`）。
//
//	焦点	拖选之后框架把焦点落在**那段字**上（`rt.focused`），而
//		`keyDownPhysical` / `editEvent` 只把按键派给**焦点那个元素**
//		（`focusHandler()`）——列表这一层于是收不到 ⌘C。
//
// ⇒ **两条路各走一层**：
//
//	那一列（`Selectable` + `CopyData`）	作用域 = 那一列，复制整行
//	那一列外面那一层（`HandleInput`）	接「单击选中 / 双击播放」
//
// 🔴 **反过来（`HandleInput` 挂在那一列上）是错的**，实测有三条后果：
//
//  1. **右键菜单没了**：框架的 `menuTarget`（`mygo/ui/menu.go`）从命中链里
//     往上找**第一个**带 `flagContextMenu|flagEditable|flagSelectable` 的元素
//     ——那一列带着 `flagSelectable`，于是它赢了，菜单变成框架的
//     「拷贝 / 全选」，行上那份 Biu 菜单再也开不出来。
//  2. **焦点环挂在鼠标点过的那一行上**：`pressTextSelection` 把
//     `rt.focused` 设成**那段字**，而 `Focused(c, &l.state)` 问的是列表
//     那个句柄——焦点不在列表上，列表这一层就收不到 ⌘C，兜底复制没了。
//  3. **拖选之后按 ⌘C 复制出空的**：焦点在那段字上时框架走的是
//     `textSelectionCommand`，而那一段字的 `textScope` 才是那个作用域；
//     焦点在列表上时框架**不**复制。
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

// rowTextInput 是**行标题那一列外面那一层**的输入回调（票 #703 的 A）。
//
// # 它接回来的是**一个**手势
//
//	按下（单击）	那一行被选中
//
// ⚠️ **双击不在这儿接**：框架的 `listRowValueInput`（`mygo/ui/value_input.go`）
// 在 `n.DoubleClicked()` 时会 `f.owner.st.markSubmitted()`，而列表那一层挂了
// `OnSubmit(func() { l.rowPlay(l.selSong()) })`——**框架自己就放那一首**。
// 两处都放的话，一次双击会放**两遍**（实测：收到两次 `PlayNow`）。
// ⇒ 所以这里只接「单击选中」，双击交给框架那一路。
//
// ⚠️ **返回 false，不抢这一下**：文字选择（拖选的起点、双击选词）照旧发生。
// 两件事同时发生不冲突——用户双击一个词，词被选中、那一首开始播。
//
// ⚠️ **只认主键按下**：拖动 / 滚动 / 键盘一概不碰。
//
// # 🔴 **必须把这一下也转给列表那一层**
//
// 框架把一次按下交给**命中链上最里层那个带 `inputFn` 的元素**
// （`mygo/ui/handler.go` 的 `handler`）——而这一列**在行里面**，所以按在它上面
// 的那一下**到不了列表那一层的 `HandleInput`**（`l.input`）。
//
// ⇒ 而列表那一层的 `l.input` 干的第一件事是 **`l.keys.Input(ev)`**
// （`copy_title.go`）——`KeyboardMode` 靠它记「现在是键盘在管还是指针在管」，
// **焦点环就靠那个记号**（`selection.go` 的 `On()`）。
//
// ⇒ 不转过去的话：指针点一下之后 `KeyboardMode` 仍然以为「键盘在管」⇒
// **那一行挂上了焦点环**——而用户用鼠标点过之后不该有环（实测：
// `TestClickingARowLeavesNoFocusRing` 红）。
//
// ⚠️ **转过去的是「这一下」，不是「结果」**：列表那一层自己会判它要不要吃
// （⌘C 那条），而**这一层永远返回 false**（不抢）。
func rowTextInput(selectRow func(), forward func(ui.InputEvent) bool, ev ui.InputEvent) bool {
	// ⚠️ **先转给列表那一层**：它要听见这一下（`KeyboardMode` 靠它）。
	if forward != nil {
		forward(ev)
	}
	if ev.Kind != ui.InputPointerDown || ev.Button != 0 {
		return false
	}
	if selectRow != nil {
		selectRow()
	}
	return false
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
func (l *List) rowSelHandle(i int) *ui.Handle {
	if i < 0 {
		return &l.rowSel[0]
	}
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
	if i < 0 {
		return &g.rowSel[0]
	}
	for len(g.rowSel) <= i {
		g.rowSel = append(g.rowSel, ui.Handle{})
	}
	return &g.rowSel[i]
}

// rowSelection 读**当帧**那一段文字选中（票 #703 的 A，列表那一档）。
//
// ⚠️ **为什么挨个问**：框架全局只有一段选中（`mygo/ui/textselection.go` 的
// `rt.selection`，一个 scope 一个），而「拥有它的那一列」是哪一个，框架没告诉
// anybody——所以挨个问一遍「选中在你那儿吗」（`SelectedTexts` 判的是 scope，
// 不是内容，`mygo/ui/textinput.go` 的 `SelectedTexts`）。
//
// ⚠️ **每帧读一次**：选中会自己变（选中变了、段落被重建了、那一段没了）。
//
// ⚠️ **读不到就是没选中**：那一列这一帧没被建出来（虚拟化滚走了）时
// `Context.Resolve` 给一个空元素，读出来是空的——那是对的：那段字已经不在
// 屏幕上了。
func (l *List) rowSelection(c *ui.Context) {
	l.rowSelParts = nil
	l.rowSelText = ""
	for i := range l.rowSel {
		e := c.Resolve(l.rowSel[i])
		if !e.Valid() {
			continue
		}
		if parts := e.SelectedTexts(); len(parts) > 0 {
			l.rowSelParts = parts
			l.rowSelText = l.rowSelTextOf(i)
			return
		}
	}
}

// rowSelection 读**当帧**那一段文字选中（票 #703 的 A，网格那一档）。
//
// ⚠️ **为什么挨个问**：理由同 `List` 那一档（框架全局只有一段选中）。
func (g *Grid) rowSelection(c *ui.Context) {
	g.rowSelParts = nil
	g.rowSelText = ""
	for i := range g.rowSel {
		e := c.Resolve(g.rowSel[i])
		if !e.Valid() {
			continue
		}
		if parts := e.SelectedTexts(); len(parts) > 0 {
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
