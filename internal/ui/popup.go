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
//
// ⚠️ **出口有两档**（浮层那一档 / 卡片那一档），但**两档都带影**——漏影是那
// 一条要防的，档位不是。头像下拉走卡片那一档（见 `popupPanelCard`）。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shadow"
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

// popupPanelCard 给一个**卡片级**浮层面板上外观：底色 surface1、圆角 radiusLg、
// 阴影 shadow.Modal。
//
// # 它和 `popupPanel` 是**两档料**，不是两个出口
//
//	浮层那一档（`popupPanel`）  底 = 页面底、圆角 8、影 = shadow.Popover
//	卡片那一档（这一档）        底 = surface1、圆角 radiusLg、影 = shadow.Modal
//
// 卡片那一档是**设计规范 §零** 定的：登录页那张卡、弹窗面板（`dialogPanel`）
// 都是这一档，而**头像下拉是同一块料**——它是一张浮在内容上面的卡片，不是
// 「贴着一层底的小浮层」。
//
// ⚠️ **底色和圆角取自 `Shell.cardBackground` / `Shell.cardRadius`**（方案里的
// surface1 / radiusLg；经典是控件层和 1.5 倍圆角）——它们是卡片这一档的**唯一
// 出口**，别在调用处自己算一个。
//
// ⚠️ **影是 `shadow.Modal` 那一档**（值只写在 `widgets/shadow`）。
func popupPanelCard(s *Shell, t *ui.Theme, panel ui.Element) ui.Element {
	return shadow.Modal(
		panel.Radius(s.cardRadius(t)).
			Background(s.cardBackground(t)).
			Border(1, t.Border),
		t)
}
