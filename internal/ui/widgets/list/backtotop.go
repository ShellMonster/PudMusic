// 「回到顶部」那一枚。
//
// # ⚠️ **那四个数照上游抄，不要重新量**
//
// Biu `src/components/scroll-container/index.tsx` 的那一枚是
// `className="bg-default/50 fixed right-10 bottom-[120px] z-20 shadow-xl shadow-black/30"`，
// 出现的条件是 `setShowBackToTop((viewport?.scrollTop ?? 0) > 400)`。于是：
//
//	触发阈值 400（像素）	距右 40（`right-10` 是 2.5rem）
//	距下 120（`bottom-[120px]`，躲开播放条）	圆角全圆（`radius="full"`）
//
// **重新量一遍只会得到另一个数**，而用户是在同一个应用里从一页走到另一页的——
// 上一页的按钮在右边 40，下一页在右边 44，那就是「两个页面不是一套东西」。
package list

import (
	"github.com/ShellMonster/PudMusic/internal/ui/iconset"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shadow"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// backTopPx 是滚过多少像素才出现回顶按钮。
//
// **400 是死数，照抄** Biu `src/components/scroll-container/index.tsx`。
const backTopPx = 400

// 回顶按钮摆在哪儿。Biu 是 `fixed right-10 bottom-[120px]`：右边 40（`right-10`
// 是 2.5rem），离底 120（躲开播放条）。我们照抄这两个数。
// 原型 `.btt`：离右 32、离内容区底 24（内容区底就是播放条上沿，播放条 84），44×44 的圆，
// surface 底、1 像素 line-strong 描边、正文色 18 的 `up` 图标、`--shadow-pop`；悬停 surface-2；进场 PopIn。
const (
	backTopRight  = 32
	backTopBottom = tok.BarH + 24

	backTopSize  = 44
	backTopGlyph = 18
)

// BackToTopIcon 是回顶按钮的图标文件（原型 `up`）。
const BackToTopIcon = "Pud/up.svg"

var backToTopIcon = iconset.Must(BackToTopIcon)

// BackToTop 画「回到顶部」那一枚：滚过 400 像素才出现。
//
// offset 是**这一屏滚过了多少像素**——每一页给的是**当前画着的那一个列表**的那
// 一份（`ui.ScrollState` 的 Y，或者行数乘行高）。给错了的话，按钮会在一个没滚过
// 的页面上冒出来。
//
// top 是「回到顶上」要做的事：各页各接（列表那一档是 `List.ScrollTo`，分页那一层
// 是 `ScrollTop`，滚动容器那一档是把 `ScrollState.Y` 归零）。
//
// ⚠️ **这一份是唯一的实现**：这一枚原先在两个地方各写了一份（`widgets/list` 的
// 分页那一层，和界面层那两页自己的分页列表），两份的数一样、代码不一样——
// 改一份就有一页对不上。
func BackToTop(c *ui.Context, offset float32, top func()) {
	if offset <= backTopPx {
		return
	}
	p := tok.Of(c.Theme())
	ui.Overlay(c, func() {
		btn := ui.ButtonBase(c).Key("back-to-top").Label("返回顶部").
			Size(backTopSize, backTopSize).Radius(backTopSize/2).
			AlignItems(ui.Center).Justify(ui.Center).
			Border(1, p.LineStrong).Transition(motion.PopIn)
		bg := p.Surface
		if btn.Hovered() {
			bg = p.Surface2
		}
		btn = btn.Background(bg).Absolute().Right(backTopRight).Bottom(backTopBottom)
		btn.Children(func() {
			ui.Icon(c, backToTopIcon).FontSize(backTopGlyph).TextColor(p.Text)
		})
		btn = shadow.Pop(btn, c.Theme())
		if btn.Clicked() {
			top()
		}
	})
}
