// 焦点交接：把键盘焦点交给内容区里的列表（票 #245 的 E4）。
//
// # 为什么要做这一层
//
// 键盘用户 Tab 到侧栏之后就**卡住了**：方向键走不进内容区的列表，而列表本身
// 是能键盘走的（点一行之后连按 ↓，焦点环落在第几行）。断的是「进内容区」这一
// 段——「键盘能不能走完主线」在侧栏和内容区之间少了一环。
//
// # 为什么这一层在包里，而不是每一页各写一遍
//
// 列表**自己知道**自己的焦点句柄（框架的 `ui.ListState.Handle`，框架在 build
// 列表的时候把它绑在列表元素上，`mygo/ui/list.go` 的
// `wrapElement(owner).Bind(&s.Handle)`），而界面层拿不到「此刻画的是哪一个」
// ——搜索页三个 Tab 各一份、收藏夹页一份、首页三个 Tab 各一份。
//
// 每一页各写一遍「我这一页的方向键目标是谁」的话，漏一页就是那一页的方向键
// 进不去内容区：症状是「有的页面能走、有的页面不能」，而用户看不出来那是他
// 走错了页面。
//
// 所以：**列表每帧把自己登记上去**（`Actions.Focus`），界面层只管收。
package list

import (
	"github.com/egoist/mygo/ui"
)

// Focusable 是「能把键盘焦点接过去」的列表 / 网格。
//
// 界面层用它做「侧栏按 ↓ → 焦点进内容区」：内容区里当前那一个列表实现了它，
// 焦点就交给它。
type Focusable interface {
	// TakeFocus 把键盘焦点接到这个列表上，并把光标放到第一行。
	//
	// ⚠️ **下一帧才生效**：框架的焦点请求是在一帧的末尾统一兑现的
	// （`mygo/ui/runtime.go` 的 `applyFocusRequests`）。
	//
	// ⚠️ **不收 `*ui.Context`**：框架的 `Handle.Focus` 不传窗口时用的是「这个
	// 句柄唯一绑定着的那个窗口」，而一个句柄只属于一个窗口（一个 `ListState`
	// 被两个窗口显示，框架自己会 panic）。
	TakeFocus()
}

// handleFocus 是「一个框架的焦点句柄」的 Focusable 写法。
//
// 界面层自己画的列表（不是这个包里的）用它：那一类的焦点句柄是
// `ui.ListState.Handle`，而它们和这个包里的列表用的是同一个框架机制。
type handleFocus struct {
	h *ui.Handle
}

// TakeFocus 把焦点交给这个句柄绑着的那个控件。
func (f handleFocus) TakeFocus() {
	if f.h == nil {
		return
	}
	f.h.Focus()
}

// HandleFocus 把一个框架的焦点句柄包成 Focusable。
func HandleFocus(h *ui.Handle) Focusable { return handleFocus{h: h} }
