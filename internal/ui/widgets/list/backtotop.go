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
	"github.com/ShellMonster/Biu_Go/internal/ui/iconset"
	"github.com/egoist/mygo/ui"
)

// backTopPx 是滚过多少像素才出现回顶按钮。
//
// **400 是死数，照抄** Biu `src/components/scroll-container/index.tsx`。
const backTopPx = 400

// 回顶按钮摆在哪儿。Biu 是 `fixed right-10 bottom-[120px]`：右边 40（`right-10`
// 是 2.5rem），离底 120（躲开播放条）。我们照抄这两个数。
const (
	backTopRight  = 40
	backTopBottom = 120
	// backTopRadius 是按钮的圆角：Biu 的 `radius="full"` 是药丸形，圆角给到
	// 比按钮高度大就等于它。
	backTopRadius = 999
)

// backToTopIcon 是回顶那一枚里的向上箭头（上游 `RiArrowUpLine`）。
//
// ⚠️ 它从 `iconset` 取而**不是**从界面层拿常量：这个包在界面层**下面**，
// 反过来 import 是个环（见 `iconset` 文件头）。同一个文件在界面层的常量叫
// `iconToTop`。
var backToTopIcon = iconset.Must("Arrows/arrow-up-line.svg")

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
	t := c.Theme()
	// Overlay 把按钮浮在整窗之上（不占布局），Biu 的 `fixed` 就是这个意思。
	// Radius 给一个大数就是 Biu 的 `radius="full"`（药丸形）。
	ui.Overlay(c, func() {
		//
		// 药丸形留着（Biu 的 `ScrollContainer` 那一枚就是 `rounded-full`），
		// 里面换成一枚向上箭头（上游 `RiArrowUpLine`）。`Label` 仍是「返回顶部」
		// ——图标对辅助技术是不可见的，而它同时也是无头 Tester 认得这一枚的
		// 唯一凭据。
		btn := ui.Button(c, "").Label("返回顶部")
		btn.Children(func() { ui.Icon(c, backToTopIcon).FontSize(16).TextColor(t.Text) })
		if btn.Absolute().
			Right(backTopRight).Bottom(backTopBottom).
			Radius(backTopRadius).Background(t.SurfaceHover).Clicked() {
			top()
		}
	})
}
