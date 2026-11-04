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
	"github.com/ShellMonster/PudMusic/internal/ui/iconset"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/leading"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// Icon 取一枚原型图标（`iconset/icons/Pud/<name>.svg`，名字照原型 `ICONS` 的键：
// "play"、"heart"、"more"……）。
func Icon(name string) *ui.SVG { return iconset.Must("Pud/" + name + ".svg") }

// glyph 画一枚图标：size 边长，col 颜色。
func glyph(c *ui.Context, name string, size float32, col ui.Color) ui.Element {
	return ui.Icon(c, Icon(name)).FontSize(size).TextColor(col)
}

// text 是一行正文字：字号过系统缩放，行高 `leading.Body`（1.55）。
func text(c *ui.Context, s string, size float32, col ui.Color) ui.Element {
	return ui.Text(c, s).FontSize(scale.Font(c, size)).TextColor(col).LineHeight(leading.Body)
}

// pal 是这一帧的令牌。
func pal(c *ui.Context) tok.Palette { return tok.Of(c.Theme()) }

// Cover 是「在这个方框里画一张封面」：调用方用 imgcache 那套画图，组件只给框。
// 传 nil 就只画 surface-2 的占位底（原型 `.cover` 的 background）。
type Cover func(c *ui.Context, w, h float32)
