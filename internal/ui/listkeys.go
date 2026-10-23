// 界面层自己画的那几张列表的键盘接线（票 #245 的 E4）。
//
// # 为什么它们要单独有一套
//
// `widgets/list` 那个包里的 `List` 自带三样东西：**游标**、**焦点环**、以及
// 「↑ 在顶上一行时把焦点交回侧栏」。而这几页（我的关注 / 用户动态 / UP 主空间）
// 的内容区是**裸的 `ui.List`**——卡片是自己画的，走不了那个包。
//
// 少了的那两样，症状各是：
//
//   - **没有游标**（`ui.ListState.Selected` 没装）→ 框架那一档就没有键盘游标，
//     方向键在内容区里**只滚动、不选行**。
//   - **没有「↑ 回侧栏」那一档** → 键盘用户**被留在内容区里**，回不去。
//
// # 为什么是一份共享的实现
//
// 三页的列表画法各不相同（网格 / 卡片 / 行），但**接线是同一件**：装游标、
// 听输入、记「现在是键盘在管」、顶上一行时交回焦点。同一件事写三遍的话，
// 漏一处就是那一页回不来——而症状是「有的页面回得来、有的回不来」。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// listKeys 是「一张自己画的列表」的那套键盘接线。
type listKeys struct {
	// sel 是游标：装进 `ui.ListState.Selected`，框架的列表靠它挪游标。
	//
	// ⚠️ **-1 是「还没选中任何一行」**——和 `widgets/list` 的 `List` 同一个
	// 约定（零值不是「没选中」：没选中是 -1，而零值是 0，也就是第一行）。
	// 第一帧由 `wire` 把它摆成 -1（结构体的零值是 0，那会让**第一行永远
	// 看起来是选中的**）。
	sel int

	// started 是「`wire` 已经跑过」。
	started bool

	// keys 记「上一次输入是键盘还是指针」，并接住「↑ 到顶时交回侧栏」。
	keys list.KeyboardMode

	// st 是这一份列表的框架状态（页面状态里那个）。
	//
	// ⚠️ **由 `wire` 记下来**：`TakeFocus` 要用它的焦点句柄，而那时列表已经
	// 建过至少一帧了。
	st *ui.ListState
}

// wire 把这一套接到一张列表上。**每一帧** build 时调一次。
//
// out 是「↑ 在顶上一行时该做什么」——界面层传的是「把焦点交回侧栏」。
func (k *listKeys) wire(c *ui.Context, st *ui.ListState, out func()) {
	if !k.started {
		k.started = true
		// 「还没选中任何一行」（理由见 `sel` 那一条）。
		k.sel = -1
	}
	k.st = st
	// 游标：框架靠它挪游标，也靠它判「这一行选中没有」。
	//
	// ⚠️ **不装就没有键盘游标**：框架的 `buildList` 只在 `s.cursor() != nil`
	// 时才把列表标成可聚焦（`mygo/ui/list.go`），而 `cursor()` 读的就是
	// `Selected`。
	st.Selected = &k.sel
	// 「↑ 到顶时交回侧栏」那一档（票 #245 的 E4）。
	k.keys.SetUpAtTop(out)
	// 游标挪了没有？挪了而且不是指针挪的，就是键盘挪的（焦点环靠它，理由见
	// `widgets/list/selection.go` 的 KeyboardMode）。
	//
	// ⚠️ **要在画行之前问**：焦点环是画在行上的，这一句晚了那一帧就少一圈环。
	k.keys.Moved(k.sel)
}

// TakeFocus 把键盘焦点接到这张列表上，并把游标摆到第一行。
//
// ⚠️ **和 `widgets/list` 的 `List.TakeFocus` 是同一件事**：走进来就在第一首上，
// 而不是「还得再按一下方向键才开始挪」。
func (k *listKeys) TakeFocus() {
	k.sel = 0
	if k.st != nil {
		k.st.Handle.Focus()
	}
}

// ring 报「现在该不该给选中的那一行画焦点环」。
//
// ⚠️ **条件是「列表有键盘焦点」且「上一次输入是键盘」**：鼠标点过之后方向键
// 挪了游标，那一行也得有指示（框架那个 `focusVisible` 方向键不会设回来，靠它
// 那一路是哑的——理由见 `widgets/list/selection.go` 的 KeyboardMode）。
func (k *listKeys) ring(c *ui.Context) bool {
	if k.st == nil || !k.keys.On() {
		return false
	}
	return list.Focused(c, k.st)
}

// selected 报「第 i 行是不是选中的那一行」。
func (k *listKeys) selected(i int) bool { return i == k.sel }

// selectedCell 报「第 i 个（**行里的**）是不是落在选中的那一行上」。
//
// ⚠️ **网格那一档的行和格不是同一个数**：框架的游标数的是**行**（`ui.List` 的
// 那一行），而网格里用户点的是**格**。所以「这一格选中没有」要拿它的**行号**
// 去比——`i/cols`。
func (k *listKeys) selectedCell(i, cols int) bool {
	if cols <= 0 {
		return false
	}
	return i/cols == k.sel
}
