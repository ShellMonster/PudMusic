// Package pud 是「深夜唱片行」的公共组件（DESIGN.md §0′）：按原型 `index.html` 的
// 组件 CSS 逐项做的一套积木，T1–T7 的页面直接拿它拼。
//
// # 有哪些
//
//	按钮      Button(kind, size) / RoundButton / IconBtn / Chip / Kbd / Badge
//	标题      PageHead / SecHead / Tabs / typo.Eyebrow
//	列表      TrackHead / TrackRow / EqBars
//	卡片      CoverCard / ColCard / VideoCard / UCard / Avatar / GridCols / VideoGridCols
//	状态      Empty / EmptyArt / LoginPrompt / Skeleton
//	控件      Switch / Select / MiniSearch（进度条用 internal/ui 的 selfSlider，已按 .range 改过）
//	浮层      PopPanel / PopHeading / PopOpt / Scrim / DrawerPanel / DrawerHead /
//	          DrawerSub / DialogPanel / DialogBody / DialogFoot / Toast
//
// # 约定
//
//   - 颜色只从 `tok.Of(c.Theme())` 取，尺寸从 `tok` 的尺寸常量取，字体走 `typo`。
//   - 组件**只管长相和交互反馈**，不管数据：封面图由调用方传一个画法（`Cover`），
//     点击结果用返回值报（`Clicked` / 事件结构体），和框架控件一个用法。
//   - 每个可点的组件都要调用方给一个稳定的 key（动态列表的铁律四）。
//   - 进出场动画挂在 `motion` 的那几份 `Transition` 上，系统开了「减少动态效果」
//     时框架只淡入淡出。
//   - 字号都过 `scale.Font`（系统文本缩放），图标尺寸不过。
//
// 组件陈列见 `internal/ui/components_shot_test.go`（深浅各一张，和原型逐项对照）。
package pud

import (
	"sync"

	"github.com/ShellMonster/PudMusic/internal/ui/iconset"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/leading"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// Icon 取一枚原型图标（`iconset/icons/Pud/<name>.svg`，名字照原型 `ICONS` 的键：
// "play"、"heart"、"more"……）。
func Icon(name string) *ui.SVG {
	if v, ok := iconCache.Load(name); ok {
		return v.(*ui.SVG)
	}
	svg := iconset.Must("Pud/" + name + ".svg")
	iconCache.Store(name, svg)
	return svg
}

// iconCache 按名字缓存解析好的图标：`Icon` 每帧每枚键调一次，每次现拼路径字符串就是每帧多
// 十几次分配（满屏的每帧分配预算盯着这一处）。
var iconCache sync.Map

// glyph 画一枚图标：size 边长，col 颜色。
func glyph(c *ui.Context, name string, size float32, col ui.Color) ui.Element {
	return ui.Icon(c, Icon(name)).FontSize(size).TextColor(col)
}

// text 是一行正文字：字号过系统缩放，行高 `leading.Body`（1.55）。
func text(c *ui.Context, s string, size float32, col ui.Color) ui.Element {
	return ui.Text(c, s).FontSize(scale.Font(c, size)).TextColor(col).LineHeight(leading.Body)
}

// pal 是这一帧的令牌。全屏播放器里（`WithFull`）取固定暗色那一套。
func pal(c *ui.Context) tok.Palette {
	if fullMode {
		return tok.OfFull(c.Theme())
	}
	return tok.Of(c.Theme())
}

// fullMode 为真时所有组件按全屏播放器的固定暗色画（原型 `.full` 改写了整套 CSS 变量）。
// 只在主线程建树时读写，且只由 `WithFull` 改。
var fullMode bool

// WithFull 在 fn 里按全屏播放器的固定暗色那一档画，fn 结束（包括 panic 被外层 Guard 接住）
// 时用 defer 摆回进来之前的值。
//
// 以前是成对的 `SetFull(true)` / `SetFull(false)`：夹着的回调一旦 panic，false 摆不回去，
// 别的页面会按全屏的暗色画，只能靠每帧开头复位兜底。`defer` 把「摆回去」绑在作用域上，
// 不再需要任何一处兜底。
func WithFull(fn func()) {
	prev := fullMode
	fullMode = true
	defer func() { fullMode = prev }()
	fn()
}

// Full 报现在是不是在全屏播放器的那一档配色里。
func Full() bool { return fullMode }

// Cover 是「在这个方框里画一张封面」：调用方用 imgcache 那套画图，组件只给框。
// 传 nil 就只画 surface-2 的占位底（原型 `.cover` 的 background）。
//
// radius 是组件那个框的圆角（卡片 8、曲目行 4、头像半边长），**画图的那一层必须原样用上**：
// GPU 后端嵌套裁剪只有最内层切圆角（mygo `internal/gpu/gpu.go` 的 markEnclosing），外框的
// `Radius(r).Clip()` 只按矩形裁，图自己不带圆角在真机上就是直角（无头截图的 CPU 光栅把两层
// 覆盖率相乘，看不出来）。
type Cover func(c *ui.Context, w, h, radius float32)
