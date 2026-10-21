// 浮层（下拉面板、联想面板、歌词旁的旋钮面板）的统一外观。
//
// # 为什么要有这一处
//
// 框架的 `stylePanel`（`mygo/ui/widgets.go:623`）给浮层的是
// 「Radius+2 / Background / Border / Shadow(0,6,20,0,黑18%)」。我们用
// `SelectBase` / `PopoverBase` 自己拼面板时，**每一处都只抄了前三个**——阴影
// 漏掉了。于是同一屏上，框架自己弹的浮层有影子，我们抄的那几个没有：两块面板
// 挨着的时候，一块浮在内容上、一块像贴上去的，而那种差别没人说得清是哪不对。
//
// 票面要的是「两处有一处无」统一；这里收成一个出口，以后再加浮层就不会漏。
package ui

import "github.com/egoist/mygo/ui"

// popupRadius 是浮层面板的圆角：**固定的 8**，不跟 `t.Radius` 走。
//
// 上游那一套浮层是 HeroUI 的 `rounded-medium`（主题里这一档是 8；
// `search/index.tsx:100` 的联想面板就是它）。我们原来抄的是框架 `stylePanel` 的
// `t.Radius+2`——默认 8 时是 10，比上游大 2；用户把全局圆角调到 24 时更是圆得
// 不成样子。DESIGN 的令牌也把它单列成 `radius.popover: 8`，所以这里钉死，
// 不跟全局可调圆角走。
const popupRadius = 8

// popupPanel 给一个浮层面板上外观：圆角、底、描边、阴影。
//
// 参数和框架 `stylePanel` 一样（没有 Margin/Padding：各处的间距不一样，调用方
// 自己给）。**别在调用处再抄一遍这几个字段**——漏一个就是下一处「有一处无」。
func popupPanel(t *ui.Theme, panel ui.Element) ui.Element {
	return panel.Radius(popupRadius).
		Background(t.Background).Border(1, t.Border).
		Shadow(0, 6, 20, 0, popupShadow(t))
}

// popupShadow 是浮层那圈影子的颜色。
//
// 浅色下取框架 `stylePanel` 那个 18%；深色下要更浓（90/255 ≈ 35%）——同一个
// 18% 压在 `#18181b` 上看不出边界，面板和底下的内容糊成一片。上游 Biu 是
// `shadow-2xl`，深浅两套本来就是两个浓度。
func popupShadow(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.Color{A: 90}
	}
	return ui.Color{A: 46}
}
