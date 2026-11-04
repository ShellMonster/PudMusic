// 原型的断点和流式字号都是按**窗口宽**算的（`@media (max-width: …)`、`clamp(…vw…)`，COMMON.md
// 「断点和流式字号一律按窗口宽度计算」）。T0 的 `space.PageHorizontal` / `pud.GridCols` /
// `scale.PageTitleFor` 写的是内容区宽，口径不对，收尾清理票会统一改；在那之前 T3 的页面用
// 这里这几个按原型 CSS 直接算的版本。
package ui

import "github.com/egoist/mygo/ui"

// windowW 是这一帧的窗口宽（不是内容区宽）。
func windowW(c *ui.Context) float32 {
	w, _ := c.Size()
	return w
}

// winPad 是 `.page` 的左右内边距：> 1024 为 40；≤ 1024 为 32；≤ 760 为 16。
func winPad(w float32) float32 {
	switch {
	case w <= 760:
		return 16
	case w <= 1024:
		return 32
	}
	return 40
}

// winGridCols 是 `.card-grid` 的列数：> 1280 为 6；≤ 1280 为 5；≤ 1180 为 4；≤ 760 为 2。
func winGridCols(w float32) int {
	switch {
	case w <= 760:
		return 2
	case w <= 1180:
		return 4
	case w <= 1280:
		return 5
	}
	return 6
}

// winGridGap 是卡片网格的格间距：24；≤ 760 为 16。
func winGridGap(w float32) float32 {
	if w <= 760 {
		return 16
	}
	return 24
}

// pageTitleSize 是 `.h-page` 的字号：`clamp(32px, 3.4vw, 48px)`（1200 宽时 40.8）。
func pageTitleSize(w float32) float32 { return min(max(32, 0.034*w), 48) }

// colTitleSize 是 `.col-title` 的字号：`clamp(36px, 4.4vw, 64px)`（1200 宽时 52.8）。
func colTitleSize(w float32) float32 { return min(max(36, 0.044*w), 64) }
