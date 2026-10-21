// Package scale 把系统「文本大小」那一档套到写死的字号上。
//
// # 为什么每个字号都要过它
//
// 框架的 `Theme.FontSize` 会跟着 `Preferences.TextScale` 变
// （`mygo/ui/preferences.go:78-80`），但那只是**没显式设字号**的文字的默认值。
// 我们的界面里绝大多数 `ui.Text` / `ui.Button` 都显式写了 `FontSize(12)` 这类
// 固定 DIP——不乘的话，用户在系统设置里把文本调到 125%，界面上一个像素都不变
// （票 #139 的原话）。
//
// 所以全应用的显式字号都写成 `FontSize(scale.Font(c, 12))`。`scale` 为 1 时
// **原样返回**，所以默认那一档一个字节都不变。
//
// # 什么不过它
//
// **图标**（`ui.Icon(...).FontSize(iconSm)`）和 Spinner：上游的图标尺寸是固定
// 像素（`size={20}`），不跟系统字号缩放走。把它们也乘上去的话，系统字号调大
// 之后图标比字还大，整行的高度会跟着变。
package scale

import "github.com/egoist/mygo/ui"

// Font 返回 n 在系统文本缩放之后的大小。
//
// 读的是 `Context.Preferences()`（每帧缓存，不贵）。1 和 0 都按「不缩放」：
// 0 在框架那边是「没说」，落到 `preferences()` 里也会变成 1。
func Font(c *ui.Context, n float32) float32 {
	s := c.Preferences().TextScale
	if s <= 0 || s == 1 {
		return n
	}
	return n * s
}
