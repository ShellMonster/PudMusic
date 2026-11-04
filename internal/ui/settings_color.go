// 设置里的颜色串 ↔ ui.Color：满屏播放器的自定义底色那一格（fullplayer_settings.go）在用。
package ui

import "github.com/egoist/mygo/ui"

// colorOfHex 把设置里那个字符串变成一个颜色，认不出就退回不透明的黑。
//
// 解析用的是 `theme.go` 里那一份，**不是** `ui.Hex`：后者对不合法的输入直接
// panic（`ui/color.go:33`），而这里是设置文件里的用户输入，为一个画错的颜色把
// 整个进程带走不划算。
func colorOfHex(s string) ui.Color {
	if col, ok := parseHexColor(s); ok {
		return col
	}
	return ui.Color{A: 255}
}

// toHex 把一个颜色写成设置里存的那个串。
//
// 🔴 **不透明的时候才写 6 位；半透明写 8 位**（`#rrggbbaa`）。只写 RGB 的话，取色板
// 上那条 **Opacity** 滑块调出来的透明度会被**悄悄丢掉**：用户拖到一半，界面上是半透明
// 的，存下去却是全不透明，下一帧又跳回去（实测：拖到中点保存下来还是 `#ef4444`，
// `BuildTheme().Accent.A` 是 255 而不是 ~128）。
//
// 8 位写法设置那边认得：`internal/app/settings.go` 的 `hexColor` 收 3/4/6/8 位，
// `theme.go` 的 `parseHexColor` 也解 8 位——所以这不是新格式，是那个格式本来就有、
// 一直没人写。
func toHex(col ui.Color) string {
	const hex = "0123456789abcdef"
	out := "#" + string([]byte{
		hex[col.R>>4], hex[col.R&0xf],
		hex[col.G>>4], hex[col.G&0xf],
		hex[col.B>>4], hex[col.B&0xf],
	})
	// 全不透明时仍然写 6 位：Biu 的默认色是 `#1ed760` 这种 6 位的，多两位会让
	// 「和 Biu 存的那份一模一样」这件事变得比必要的更难。
	if col.A == 255 {
		return out
	}
	return out + string([]byte{hex[col.A>>4], hex[col.A&0xf]})
}
