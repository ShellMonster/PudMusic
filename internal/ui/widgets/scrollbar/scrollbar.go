// Package scrollbar 量「指针压没压在滚动条上」。
//
// # 为什么要量
//
// 上游的滚动条是 `overlayscrollbars` 的默认样式：10 像素宽，指针放上去
// `--os-handle-bg-hover` 比默认那一档深一点（`biu/src/app.css:55-61`），提示
// 「这根能拖」。MyGo 的滚动条只有 `Theme.Scrollbar` **一个**颜色，没有悬停档
// （`mygo/ui/paint.go:970-1012` 的 `scrollbars`），所以这一层只能我们自己接：
// 在 view 里量出「指针在滚动条上」，由界面层在下一帧把主题那个颜色换深一档
// （见 `internal/ui/scrollbar.go`）。
//
// # 为什么单独一个包
//
// 界面层（`internal/ui` 的各页）和控件层（`widgets/list` 的列表与网格）都要
// 量，而界面层在控件层上面——反过来 import 是个环。和 `widgets/flat`、
// `widgets/ink` 是同一个解法。
package scrollbar

import "github.com/egoist/mygo/ui"

// Width 是滚动条的宽，照上游 `--os-size: 10px`（`biu/src/app.css:56`）。
//
// 框架默认 6，由 `Theme.ScrollbarWidth` 改（见 `internal/ui/theme.go`）。
const Width = 10

// HotZone 是「算压在滚动条上」的那一条竖带的宽。
//
// 等于滚动条本身 + 框架在它左边留的 6 像素（`mygo/ui/paint.go:995-999` 的
// `vTrack`：`X = 右边 - width - 6`，宽 `width + 6`）。用整条轨道而不是精确到
// 那个 thumb：上游的 `:hover` 是 handle 上的，但轨道上任何一处都离 handle
// 不远，而用户要的提示是「能拖」——多出来的那几像素只会让提示早一点出现。
const HotZone = Width + 6

// Hot 报指针现在是不是压在 el 的竖直滚动条那一带。
//
// ⚠️ **el 必须是那个滚动容器本身**（`ui.List` / `ui.Scroll` / `ui.Table` 建
// 出来的那个元素），不是里面的内容：相对坐标和盒子都是按它算的。
//
// ⚠️ `Bounds()` 读的是**上一帧**的盒子（框架的约定，和 `lyrics.go` 那一处
// 一样）。容器的宽不会在指针落上去的那一帧变，所以没有影响。
//
// 元素没有状态（`hasState` 为 false）或者还没画过时返回 false：那种时候
// `Bounds()` 是零，而 `x >= -16` 会把「指针在哪儿」都算成真。
func Hot(el ui.Element) bool {
	x, _, over := el.PointerPosition()
	if !over {
		return false
	}
	w := el.Bounds().W
	return w > 0 && x >= w-HotZone
}
