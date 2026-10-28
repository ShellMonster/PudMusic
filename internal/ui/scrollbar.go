// 滚动条的悬停档：指针压在滚动条上时深一点，提示「这根能拖」。
//
// # 框架那儿缺的是「第二档颜色」
//
// 框架的 `scrollbars`（`mygo/ui/paint.go:975-1020`）只用 `Theme.Scrollbar` 一个
// 颜色，只在**指针落在容器上**（或正在拖）时才画。所以「出现」这件事框架已经
// 做了，缺的是「出现之后指针放到滚动条上再深一档」——上游那是
// `--os-handle-bg-hover`（`biu/src/app.css:58-59`）。
//
// 主题是每帧开头装一次的，所以这一档只能在**构建这一帧的 view 时**量、在
// **下一帧**生效（`Shell.scrollbarHot` 的注释）。一帧的延迟在指针移动时不会
// 被看见：指针一动框架就排了一帧。
//
// # 为什么不在绘制层做
//
// `Painter` 上没有 `SetTheme`，而绘制是深度优先的：在某个容器的 `DrawOver` 里
// 改主题，排在前面的滚动条已经画完了，后面的才用新色——一次滚动两个颜色。
// view 那一层是唯一能在「这一帧所有元素建出来之前」定下主题的地方。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scrollbar"
	"github.com/egoist/mygo/ui"
)

// scrollbarWidth 是滚动条的宽，照上游那一个数（`--os-size: 10px`）。
//
// 放在主题里而不是某个控件上：框架的 `scrollBars` 从 `theme.scrollbarWidth()`
// 读它（`mygo/ui/paint.go:1006`），一处改了所有滚动容器一起变。
const scrollbarWidth = scrollbar.Width

// trackScrollbar 量 el 的滚动条这一帧有没有被指针压着，并把它原样返回。
//
// 用法是把构造那一句包起来：
//
//	ui.Scroll(c).Grow(1).TrackScroll(&s.scroll).
//	    Children(...) // ← 这一句的返回值喂给 trackScrollbar
//
// ⚠️ **在 Children 之后调**：`Bounds()` 要的是容器上一帧的盒子，而盒子的宽
// 在 Children 之前就定了——两处都行，但放在链式调用的最后最不容易漏。
func (s *Shell) trackScrollbar(el ui.Element) ui.Element {
	if scrollbar.Hot(el) {
		s.scrollbarHotNow = true
	}
	return el
}

// noteScrollbar 收下控件层量到的结果（`widgets/list` 的 `List.Hot` /
// `Grid.Hot`：它们的容器在包里面，页面拿不到那个 `ui.Element`）。
func (s *Shell) noteScrollbar(hot bool) {
	if hot {
		s.scrollbarHotNow = true
	}
}
