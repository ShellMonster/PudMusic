// 浮层（下拉面板、联想面板、歌词旁的旋钮面板）的统一外观。
//
// # 为什么要有这一处
//
// 框架的 `stylePanel`（`mygo/ui/widgets.go:623`）给浮层的是
// 「Radius+2 / Background / Border / Shadow(0,6,20,0,黑18%)」。我们曾经用
// `SelectBase` / `PopoverBase` 自己拼面板时，**每一处都只抄了前三个**——阴影
// 漏掉了。于是同一屏上，框架自己弹的浮层有影子，我们抄的那几个没有：两块面板
// 挨着的时候，一块浮在内容上、一块像贴上去的，而那种差别没人说得清是哪不对。
//
// 票面要的是「两处有一处无」统一；这里收成一个出口，以后再加浮层就不会漏。
// ⚠️ **自己拼面板的地方必须走这个出口**——`internal/ui/popup_test.go` 的
// `TestEveryHandMadePopupHasAShadow` 扫源码盯着这件事（#229）。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/shadow"
	"github.com/egoist/mygo/ui"
)

// popupRadius 是浮层面板的圆角：**固定的 8**，不跟 `t.Radius` 走。
//
// ⚠️ **8 来自 DESIGN.md 的 `radius.popover`，不是上游的 `rounded-medium`**：
// HeroUI v2 里 `rounded-medium` 是 **12**（`rounded-small` 8、`rounded-large`
// 14），`search/index.tsx:100` 的联想面板用的就是它。这一条按 DESIGN 的令牌
// 走——它把浮层单列成 8，而且明确「不在这里的项不要自己发明」。
//
// 我们原来抄的是框架 `stylePanel` 的 `t.Radius+2`——默认 8 时是 10，比 DESIGN
// 大 2；用户把全局圆角调到 24 时更是圆得不成样子。
const popupRadius = 8

// popupPanel 给一个浮层面板上外观：圆角、底、描边、阴影。
//
// 参数和框架 `stylePanel` 一样（没有 Margin/Padding：各处的间距不一样，调用方
// 自己给）。**别在调用处再抄一遍这几个字段**——漏一个就是下一处「有一处无」。
//
// ⚠️ **影的值从 `widgets/shadow` 取**（`shadow.Popover`）：那一圈影的数不许写在
// 调用处（DESIGN.md §四·五「值只写一处」），票 #239 把这一处收进去了。
func popupPanel(t *ui.Theme, panel ui.Element) ui.Element {
	return shadow.Popover(
		panel.Radius(popupRadius).
			Background(t.Background).Border(1, t.Border),
		t)
}
