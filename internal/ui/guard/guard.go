// Package guard 是 view 的 panic 兜底：给一个画法包一层 recover。
//
// 它单独一个包，是因为**两层都要用它**：界面层给整棵树包（`internal/ui` 的
// Guard），控件层给自己包（`internal/ui/widgets/list` 的 PagedList）。放进
// `internal/ui` 的话控件层就得反过来 import 界面层——那是个环，阶段 5 一接
// 内容页就炸。所以这里只认 MyGo 的 ui 包，谁都能 import 它。
//
// 为什么非包不可：MyGo 的 Dispatch（app.go:576）和事件循环的 drain 都没有
// recover，view 里 panic 会一路穿到 Cocoa 事件循环上去**直接杀死进程**，连
// 「这一页坏了」的机会都没有。Biu 原来靠 React 的 ErrorBoundary 兜住，这里用
// recover 兜住同一个位置的效果。
package guard

import (
	"github.com/egoist/mygo/ui"
)

// Wrap 给 draw 包一层 recover，崩了交给 onPanic。
//
// onPanic 拿到崩掉的东西（以及栈，要自己打日志）：界面用它换一张错误页，或者
// 像分页列表那样记一笔要下一帧。
//
// ⚠️ recover 是在 build pass **中间**接住的，那一帧已经建了一半。所以想要
// 完整的下一帧，onPanic 里得**再要一帧**（实测：MyGo 的 build 会在下一遍
// 从头重建，不主动要就停在那半张上）。
func Wrap(draw func(*ui.Context), onPanic func(any)) func(*ui.Context) {
	return func(c *ui.Context) {
		defer func() {
			if r := recover(); r != nil {
				onPanic(r)
			}
		}()
		draw(c)
	}
}
