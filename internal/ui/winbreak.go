// 原型的断点和流式字号都是按**窗口宽**算的（`@media (max-width: …)`、`clamp(…vw…)`，COMMON.md
// 「断点和流式字号一律按窗口宽度计算」）。页边距、标题字号、网格列数用 main 上统一的那一套：
// `space.PageHorizontalForWindow`、`scale.PageTitleFluid` / `scale.Fluid`、`homeLayoutFor(winW).cols`。
// 这里只留它们没有覆盖的两个：窗口宽取值、收藏夹大标题的字号、网格间距。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// windowW 是这一帧的窗口宽（不是内容区宽）。
func windowW(c *ui.Context) float32 {
	w, _ := c.Size()
	return w
}

// winGridGap 是卡片网格的格间距：24；窗口 ≤ 760 为 16（`.card-grid` 的媒体查询）。
func winGridGap(w float32) float32 {
	if w <= 760 {
		return 16
	}
	return 24
}

// colTitleSize 是 `.col-title` 的字号：`clamp(36px, 4.4vw, 64px)`（1200 宽时 52.8）。
func colTitleSize(w float32) float32 { return scale.Fluid(36, 4.4, scale.HeroTitle, w) }
