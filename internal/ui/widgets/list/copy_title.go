// 「⌘C 复制选中那一行」这一族（票 #685；票 #703 的 A 改了它的分工）。
//
// # 它现在做的是**兜底**
//
// 行标题那一列现在**可选中**了（票 #703 的 A，见 `row_select.go`），而拖选之后
// 焦点落在**那段字**上——框架只把按键派给焦点那个元素（`mygo/ui/input.go` 的
// `keyDownPhysical` 与 `editEvent` 都走 `focusHandler()`），列表这一层收不到
// 那一段。
//
// ⚠️ **而这一列按下的时候把焦点交回了列表**（`rowTextInput`）——于是列表这一层
// **收得到** ⌘C 了，代价是**框架那一份复制不做了**（实测：焦点在列表上时
// 「编辑 › 拷贝」复制出来是空的）。
//
// ⇒ 所以**两份复制都由列表这一层做**，分工是：
//
//	有文字选中	复制**整行**（歌名 + UP 名 + 时长，理由见 `row_select.go`）
//	没有选中	复制选中那一行的**歌名**（这一族原来的作用）
//
// ⚠️ **为什么「没有选中」这一支还在**：用户用方向键选了行（焦点在列表上），
// 按 ⌘C 复制的是那一行的歌名——而拖选之后按 ⌘C 复制的是刚拖选的那一段。
// 两个入口各管各的（和页面里别的可选中区域一个规矩，见
// `internal/ui/selectable_copy_test.go` 的
// `TestCopyingASelectionStillBeatsTheSelectedRow`）。
//
// ⚠️ **只有真的复制了才返回 true**（返回 true = 这一下我吃掉了）：没选中行、
// 或者 `acts.Copy` 没接上时返回 false——列表不该在没东西可复制的时候把 ⌘C
// 吞掉（窗口那一层、别的控件还有可能要用它）。
//
// ⚠️ **复制的是「界面上显示的那个名字」**（`ShowOf` 清洗过的）：界面上写着
// 「晴天」而复制出来是「【4K修复】晴天」，用户会以为复制错了行。
package list

import (
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/egoist/mygo/ui"
)

// copyCommand 报这个事件是不是「复制」：按键那一路（⌘C / Ctrl+C）或者编辑菜单
// 那一路（`InputCommand` 的 `"copy"`）。
//
// ⚠️ **两条都要认**：macOS 上「编辑 › 拷贝」走的是菜单命令（
// `SurfaceCommand`，见 `mygo/ui/input.go` 的 `editEvent`），而键盘走的是按键；
// 只认一条的话另一条在真机上就是「按了没反应」。
func copyCommand(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputKeyDown:
		return ev.Mods == ui.Cmd && ev.Key == ui.KeyC
	case ui.InputCommand:
		return ev.Text == "copy"
	}
	return false
}

// notifyCopied 说一句「复制好了」。
//
// ⚠️ **静默的复制是最坏的那一种**：用户按了 ⌘C，界面上什么都没有，他不知道
// 是复制成功了还是这一次按键丢了。列表右键菜单那一项也是这么做的
// （`menu.go` 的 `copy-info`：「已复制歌曲信息」）。
func notifyCopied(acts Actions) {
	if acts.Notify != nil {
		acts.Notify("已复制歌名", notice.Success)
	}
}

// input 是列表这一层的输入回调（`Element.HandleInput`）。
//
// 它做两件事，顺序不能反：
//
//  1. **先让 `KeyboardMode` 听一听**——它记「上一次是键盘还是指针」，焦点环
//     靠它（`selection.go`）。它永远返回 false（只记不吃）。
//  2. **再认 ⌘C**：有文字选中就复制**整行**，没选中就复制选中那一行的歌名。
//
// ⚠️ **只有真的复制了才返回 true**（返回 true = 这一下我吃掉了）：没选中行、
// 或者 `acts.Copy` 没接上时返回 false——列表不该在没东西可复制的时候把 ⌘C
// 吞掉（窗口那一层、别的控件还有可能要用它）。
//
// ⚠️ **复制的是「界面上显示的那个名字」**（`ShowOf` 清洗过的）：界面上写着
// 「晴天」而复制出来是「【4K修复】晴天」，用户会以为复制错了行。
func (l *List) input(ev ui.InputEvent) bool {
	// ⚠️ **返回值不判**：`KeyboardMode.Input` 永远返回 false（它只记「键盘在管
	// 还是指针在管」，不吃事件）。
	l.keys.Input(ev)
	if !copyCommand(ev) || l.acts.Copy == nil {
		return false
	}
	// ⚠️ **先看「有没有选中一段字」**（票 #703 的 A）：拖选之后那才是用户要的。
	if len(l.rowSelParts) > 0 {
		l.acts.Copy(l.rowSelText)
		notifyCopied(l.acts)
		return true
	}
	m, ok := l.Selected()
	if !ok {
		return false
	}
	l.acts.Copy(ShowOf(m).Title)
	notifyCopied(l.acts)
	return true
}
