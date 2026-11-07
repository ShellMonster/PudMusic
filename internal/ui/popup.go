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
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shadow"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// popupRadius 是浮层面板的圆角：**固定的 12**，不跟 `t.Radius` 走（原型 `.pop` 的
// `border-radius: var(--r-12)`，index.html:588；`tok.R12`）。
//
// 浮层是浮在别的内容上面的一层，圆角跟着下面内容块走的话两层会「长在一起」。
const popupRadius = tok.R12

// popupPanel 给一个浮层面板上原型 `.pop` 的外观（index.html:587-589）：`surface` 底、
// 1 像素 `line` 描边、圆角 12、最小宽 200、`--shadow-pop`、进场 `pop-in`（`motion.PopIn`）。
// 和 `pud.PopPanel` / `pud.PopStyle` 同一套样子；这一处是给框架 `SelectBase` / `PopoverBase`
// 递进来的面板用的（只有主题可用的地方）。
//
// 参数和框架 `stylePanel` 一样（没有 Padding：各处的间距不一样，调用方自己给）。
// **别在调用处再抄一遍这几个字段**——漏一个就是下一处「有一处无」。
//
// ⚠️ **影的值从 `widgets/shadow` 取**（`shadow.Popover`，2026-11 起就是 `shadow.Pop` 那一档
// `--shadow-pop`）：那一圈影的数不许写在调用处（DESIGN.md §四·五「值只写一处」）。
func popupPanel(t *ui.Theme, panel ui.Element) ui.Element {
	p := tok.Of(t)
	return shadow.Popover(
		panel.Radius(popupRadius).MinWidth(tok.PopMinW).
			Background(p.Surface).Border(1, p.Line).Transition(motion.PopIn),
		t)
}

// popupPanelCard 给一个**卡片级**浮层面板上外观：底色 surface1、圆角 radiusLg、
// 阴影 shadow.Modal。
//
// # 它和 `popupPanel` 是**两档料**，不是两个出口
//
//	浮层那一档（`popupPanel`）  原型 `.pop`：底 = surface、圆角 12、影 = shadow.Popover（= Pop）
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
