// Package ink 算「这个颜色当字色读不读得出来」。
//
// # 为什么单独一个包
//
// 主色当字色这件事，界面层（`internal/ui` 的稍后再看 / 历史记录 / 动态卡片）
// 和控件层（`widgets/list` 的列表行与网格卡片）都要做，而界面层在控件层上面
// ——反过来 import 是个环。这和 `widgets/flat`、`widgets/iconset` 是同一个解法。
//
// # 判据是 WCAG 的对比度，不是「看着差不多」
//
// 主色默认是 Biu 的绿 `#1ed760`。它当**底色**时很好（上面的深色字 9:1），
// 但当**字色**压在浅色底上只有 1.9:1，而 WCAG 正文 AA 要 4.5:1。深色下同一个绿
// 压在深灰上是 9.2:1——所以浅色和深色不是「两边不一样」，是浅色那一半读不出来。
//
// # 主色是用户可设的
//
// 设置页能改主色，所以这一包里**没有一个写死的绿**：每一档都从 `t.Accent`
// 现算。验「不能写死」的测试（`theme_contrast_test.go`）会把主色换成一个
// 别的颜色再量一遍。
package ink

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// black 和 white 是混合的两个端点。
var (
	black = ui.Color{A: 255}
	white = ui.Color{R: 255, G: 255, B: 255, A: 255}
)

// Luminance 是 WCAG 2.x 的相对亮度。
//
// 和 `ui.Color` 里那个未导出的 `gray()` **不是一回事**：gray 是 sRGB 空间的
// 加权和（拿来挑反色够用），而 WCAG 要先转成线性光再加权，两者在中间调上差
// 得不少，而 AA 那条线正是拿它算的。
func Luminance(c ui.Color) float64 {
	lin := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

// Ratio 是 WCAG 的对比度：(L1+0.05)/(L2+0.05)，L1 取亮的那一份。
//
// ink 带 alpha 时先按 `Over` 合到 bg 上：屏幕上读到的是合出来的那个颜色，
// 不是 ink 里存的那三个通道。bg 自己也带 alpha 的话按它的 RGB 算——窗口底下
// 没有第二层底可依。
func Ratio(ink, bg ui.Color) float64 {
	if ink.A < 255 {
		ink = ink.Over(bg)
	}
	l1, l2 := Luminance(ink), Luminance(bg)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

// Ensure 把 ink 沿「离 bg 更远」的方向混，直到与 bg 的对比度至少是 ratio。
//
// 方向按**底色**的亮度挑（票面原话「按当前底色的亮度往黑或往白混」）：浅底
// 往黑走、深底往白走。已经达标就原样返回——颜色的身份（是绿、是红）尽量留着，
// 只在不够的时候才付出「变深 / 变浅」的代价。
func Ensure(ink, bg ui.Color, ratio float64) ui.Color {
	if Ratio(ink, bg) >= ratio {
		return opaque(ink, bg)
	}
	// 🔴 **方向按「哪一端能到」选，不是按底色亮不亮**：中间调底色（#808080 到
	// #AAAAAA）上，往白混到纯白也只有 3.95 / 2.85 / 2.32:1——离 4.5 差一截，
	// 而往黑混是 5.3 / 7.1 / 9.0:1，绰绰有余。早先那一版用 `Luminance(bg) < 0.5`
	// 判断（#808080 的线性亮度是 0.22，被判成「深底」），于是在这几个灰底上
	// 永远返回纯白、永远不达标。
	//
	// 数学上有一道保底：黑、白两端里总有一端能到 ≥4.58:1（两端相等时正好是
	// 4.58），所以 4.5 这个目标在**任何**底色上都可达。
	target := black
	if Ratio(white, bg) > Ratio(black, bg) {
		target = white
	}
	// 二分找**最小的**混合量：混合越多，颜色离主色越远。
	lo, hi := 0.0, 1.0
	for range 20 {
		mid := (lo + hi) / 2
		if Ratio(mixOpaque(ink, target, float32(mid), bg), bg) >= ratio {
			hi = mid
		} else {
			lo = mid
		}
	}
	return mixOpaque(ink, target, float32(hi), bg)
}

// AccentOn 是主色**当字色**时该用的那一档。
//
//   - 深色：原样。同一个绿压在深灰上是 9.2:1，而深色下主色偏暗时用户自己挑的
//     就是那个样子（票面明确「深色下不变」）。
//   - 浅色：从 `t.Accent` 现算一个与 `t.Background` 至少 4.5:1 的变体。主色是
//     用户可设的，所以这里**每次都是从他选的那个色算的**，没有写死的绿。
//
// ⚠️ 说的是「主色当**字**」。主色当**底**（实心按钮、选中行）是另一件事：那
// 一档用的是 `t.AccentText`，由 `applyAccent` 按主色亮度挑黑白。两件事的关系
// 写在 `internal/ui/flatbutton.go` 的 `flatCurrent` 里。
func AccentOn(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.Accent
	}
	// ⚠️ **按「更严的那个底」算**：列表行 / 网格格 hover 时底色由框架换成
	// `t.SurfaceHover`（浅色下比页面底色暗一档），同一条主色压在上面更淡
	//（实测按 Background 算出来的变体压 #e9e9ec 只有 3.75:1）。取两者里更暗的
	// 那个当判据——平时那一档只会更宽裕，hover 那一档正好达标。
	bg := t.Background
	if t.SurfaceHover.A == 255 && Luminance(t.SurfaceHover) < Luminance(bg) {
		bg = t.SurfaceHover
	}
	return Ensure(t.Accent, bg, 4.5)
}

// Disabled 是**禁用控件里的字**该用的灰。
//
// # 为什么不是「整体降透明度」
//
// MyGo 对禁用元素的做法是把它（连同子元素）按 50% 不透明度画一遍
// （`mygo/ui/paint.go:210-212` 的 `p.opacity *= 0.5`）。原来只读设置页那三个
// 主题按钮用的就是这个：深色下选中那一档的字是主色绿，绿乘完 0.5 压在
// `#3e3e42` 上只剩 **2.6:1**——用户看见的是「这三个选项在深色模式下消失了」，
// 而不是「现在是只读的」。
//
// 所以禁用时把字色本身换掉，让**乘完 0.5 之后**仍然 ≥3:1（WCAG 对非正文的
// 那一档）。这里是反着算的：先假设框架会乘 0.5，再找一个能扛住它的字色。
//
// ⚠️ **这依赖框架那个 0.5**。它在 `mygo/ui/paint.go` 里，不是我们的代码——
// 框架哪一版把这档改了，这条补偿就过头（字会偏亮/偏暗），而**不会报错**。
// `TestTheDisabledRowKeepsItsContrast` 量的是屏幕上的像素，能抓住。
//
// ⚠️ 这里只算**字**的颜色。按钮的底、描边仍然走框架的 0.5——那是「禁用」这件事
// 的一致手感，票面只要求字读得出来。
func Disabled(t *ui.Theme) ui.Color {
	const (
		fade = 0.5 // 框架禁用元素的透明度，见上面的注释
		want = 3.0
	)
	bg := t.Background
	shown := func(c ui.Color) ui.Color { return c.Alpha(float32(fade)).Over(bg) }
	// 方向同样按「合完 50% 之后哪一端更远」选（理由同 `Ensure`）。
	//
	// ⚠️ 中间调底色（#808080 一带）上 3:1 **数学上到不了**：框架的 50% 把字色
	// 和底色各拉近一半，两端最好的那一端在 #808080 上是 2.63:1（#999999 起才够
	// 3）。这一档能保证的是**取到两端里更好的那一个**，不是「任何底色都达标」。
	dir := black
	if Ratio(shown(white), bg) > Ratio(shown(black), bg) {
		dir = white
	}
	lo, hi := 0.0, 1.0
	for range 20 {
		mid := (lo + hi) / 2
		if Ratio(shown(mixOpaque(t.Text, dir, float32(mid), bg)), bg) >= want {
			hi = mid
		} else {
			lo = mid
		}
	}
	return mixOpaque(t.Text, dir, float32(hi), bg)
}

// opaque 把带 alpha 的颜色合到 bg 上，返回不透明的那一份。
//
// 返回不透明是刻意的：调用方拿它当字色画，底下是什么由 bg 说了算；留着一半
// alpha 的话，画的时候还要再合一次，二次合成的结果和这里算的不是同一个数。
func opaque(c, bg ui.Color) ui.Color {
	if c.A == 255 {
		return c
	}
	return c.Over(bg)
}

// mixOpaque 是 `ink.Mix(target, t)` 再合到 bg 上。
func mixOpaque(ink, target ui.Color, t float32, bg ui.Color) ui.Color {
	return opaque(ink.Mix(target, t), bg)
}
