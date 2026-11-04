// 原型的断点和流式字号都是按**窗口宽**算的（`@media (max-width: …)`、`clamp(…vw…)`，COMMON.md
// 「断点和流式字号一律按窗口宽度计算」）。页边距、标题字号、网格列数用 main 上统一的那一套：
// `space.PageHorizontalForWindow` / `space.GridGapForWindow`、`scale.*Fluid`、`pud.GridCols(winW)`。
// 这里只留窗口宽的取值。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// windowW 是这一帧的窗口宽（不是内容区宽）。
func windowW(c *ui.Context) float32 {
	w, _ := c.Size()
	return w
}

// pageBodyW 是原型 `.page { max-width: 1280px; margin: 0 auto }` 里页面的宽：内容区（窗口宽 − 侧栏）
// 封顶 1280（含左右内边距）。量不出内容区时取 1280。页面外壳用它：外层 `AlignItems(ui.Center)`，
// 里层 `Width(s.pageBodyW(c))`。
func (s *Shell) pageBodyW(c *ui.Context) float32 {
	cw := s.spaceContentWidth(c)
	if cw <= 0 || cw > space.PageMaxW {
		return space.PageMaxW
	}
	return cw
}
