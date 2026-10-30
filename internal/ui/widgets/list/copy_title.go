// 「⌘C 复制选中那一行的歌名」这一族（票 #685）。
//
// # 为什么是 ⌘C，而不是给行标题加 `Selectable`
//
// 🔴 **实测过**（票面记着，`TestClickingStillSelectsAndDoubleClickingStillPlays`
// 再钉一遍）：行标题一旦 `Selectable`，点这段文字就**不再选中那一行**——框架
// 把这一次按下当成了选字的起点（`mygo/ui/input.go` 的 `pointerDown` 先问
// `pressTextSelection`，而它认的就是 `flagSelectable`），`Selected` 停在 -1。
// 而「单击选中、双击播放」是这一列最要紧的两种手势。
//
// ⇒ 走键盘：**列表拿焦点 + 有一行被选中**时，⌘C / 「编辑 › 拷贝」复制那一行的
// 歌名。两条入口（`InputKeyDown` 与 `InputCommand`）都在这里接。
//
// # 为什么不会和「拖选文字」抢
//
// 框架只在**焦点那个元素**上派发这两条（`mygo/ui/input.go` 的
// `keyDownPhysical` 与 `editEvent` 都走 `focusHandler()`）。用户拖选了标题 /
// 评论昵称之后，焦点落在**那段文字**上（`pressTextSelection` 把
// `rt.focused` 设成段落），列表这一层的回调**根本收不到**这条命令——所以
// 「剪贴板被文本选择占用」这件事天然轮不到这里判。
//
// ⚠️ **`acts.Copy` 为 nil 时什么都不做**（返回 false，让事件照旧往下走）：
// 单独造这个包的测试没有界面层，那种情况下「复制」这件事压根不存在——不能
// 因为没接上就抢走 ⌘C。
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
//  2. **再认 ⌘C**：有选中一行就复制那一行的歌名。
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
	m, ok := l.Selected()
	if !ok {
		return false
	}
	l.acts.Copy(ShowOf(m).Title)
	notifyCopied(l.acts)
	return true
}
