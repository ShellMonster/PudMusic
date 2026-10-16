// 主题：把设置里的四件事（跟随系统/浅色/深色、主色、背景色、圆角）
// 变成 MyGo 每一帧真正用的那份主题。
//
// 🔴 铁律：Apply 必须在**每个窗口的每一帧开头**调一次。
//
// MyGo 的 ui/context.go:119 在每帧开头都执行
// `c.theme = c.rt.defaultTheme()`——把主题重置成跟随系统外观的那份。
// 上一帧 SetTheme 进去的东西**被静默丢掉**：不报错、不崩溃、不打日志，
// 就是主题悄悄跳回系统色。所以「在切换按钮的回调里 SetTheme 一次」这种
// 写法看起来能用，切完那一帧是对的，下一帧就没了。官方文档没写这个坑，
// `apply()` 下面那条测试就是为了钉住它。
//
// 接进 shell 的位置：`Shell.applyFrameTheme`，viewMain 和 viewCrash 的第一行
// 各调它一次。别的地方不许直接调 `c.SetTheme`——漏一处就有一帧用系统色。
package ui

import (
	"strconv"
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// ApplyTheme 在一帧的开头装上当前设置对应的主题。
//
// systemDark 是「系统现在是不是深色」。传 `c.Theme().Dark` 即可——那是在
// ApplyTheme 之前读的、MyGo 刚重置好的那份跟随系统的主题，正好是系统外观。
// **必须先读再设**，反过来的话读到的是自己上一帧设的值。
func ApplyTheme(c *ui.Context, s app.Settings, systemDark bool) {
	c.SetTheme(BuildTheme(s, systemDark))
}

// applyFrameTheme 是每帧开头那一次 ApplyTheme，界面层唯一的入口。
//
// ⚠️ **先读 `c.Theme().Dark` 再设**：读到的是 MyGo 刚在帧开头重置好的、跟随
// 系统的那份，也就是「系统现在是不是深色」。反过来的话读到的是我们上一帧
// 自己设的档位，「跟随系统」在系统浅色时会画出深色——深浅整个反过来，而且
// 不报错，只是界面和系统对不上。
//
// 正因为它必须读「MyGo 刚重置好的那一份」，**一帧里只能调一次**：第二次
// 读到的已经是我们自己刚设进去的那份，重复调会把「跟随系统」整个搞反。
// 于是认「这一帧装过了」的办法是比指针——`context.reset` 每个 build pass 都
// 把 `c.theme` 换回框架那份（`&rt.passTheme`，地址不变），我们 `SetTheme`
// 进去的是 `BuildTheme` 新建的那一份。两个指针不一样，就是还没装过。
//
// 这样页面 view 就敢在开头照惯例再调一次（SetTheme 只影响它之后创建的控件，
// 页面 view 忘了装主题就会用系统色画出来，跟旁边的界面对不上），而且调几次
// 都不会把主题搞反。
func (s *Shell) applyFrameTheme(c *ui.Context) {
	if c.Theme() == s.themed {
		return
	}
	systemDark := c.Theme().Dark
	ApplyTheme(c, s.app.SettingsSnapshot(), systemDark)
	s.themed = c.Theme()
}

// BuildTheme 按设置拼出这一帧要用的主题。
//
// 从 `ui.LightTheme()` / `ui.DarkTheme()` 起步而不是从零拼结构体：那两份
// 是 MyGo 自己的默认值，跟着框架升级一起变；自己写一个 `&ui.Theme{...}`
// 的话，MyGo 新加一个字段我们不会知道，控件就会用零值画。
func BuildTheme(s app.Settings, systemDark bool) *ui.Theme {
	dark := systemDark
	switch s.ThemeMode {
	case app.ThemeModeLight:
		dark = false
	case app.ThemeModeDark:
		dark = true
	}

	var t *ui.Theme
	if dark {
		t = ui.DarkTheme()
	} else {
		t = ui.LightTheme()
	}

	t.Radius = radiusOf(s.BorderRadius)
	if c, ok := parseHexColor(s.BackgroundColor); ok {
		t.Background = c
	}
	if c, ok := parseHexColor(s.PrimaryColor); ok {
		applyAccent(t, c)
	}

	// 🔴 Inverse / InverseText 留零的话，MyGo 的 inverse() 会退回
	// 「fill=Text、text=Background」——深色下就是**一张白底卡片**，
	// tooltip 和 toast 直接在深色界面里闪一下。默认值不是我们要的，
	// 所以显式给：深色下用比背景略亮的 Surface，浅色下用深色卡片。
	if dark {
		t.Inverse, t.InverseText = t.Surface, t.Text
	} else {
		t.Inverse, t.InverseText = t.Text, t.Background
	}
	return t
}

// radiusOf 是全局圆角，夹在 0~24。
//
// 上限 24 是 Biu 设置页滑块的右端点，也是圆角再大控件就开始变形的点。
// 上限在这里兜一道，不只靠设置文件那边夹——BuildTheme 也被测试和将来的
// 预览直接调用，那边给的是手搓的结构体。
func radiusOf(v float64) float32 {
	switch {
	case v <= 0:
		return 0
	case v >= app.MaxBorderRadius:
		return float32(app.MaxBorderRadius)
	}
	return float32(v)
}

// applyAccent 把主色铺到整份主题的强调色那一组上。
//
// 做法和 MyGo 自己的 `Theme.follow` 一致（ui/preferences.go:50-78）：主色
// 之外那几个色都是从它推出来的。自己另推一套，两处就会给出不一样的 hover 色。
func applyAccent(t *ui.Theme, a ui.Color) {
	black := ui.Color{A: 255}
	white := ui.Color{R: 255, G: 255, B: 255, A: 255}

	t.Accent = a
	if t.Dark {
		// 深色底上主色偏暗，hover 要往亮走才对得出来。
		t.AccentHover, t.AccentPressed = a.Mix(white, 0.2), a.Mix(black, 0.15)
		t.Selection, t.Focus = a.Alpha(0.4), a.Mix(white, 0.2).Alpha(0.6)
	} else {
		t.AccentHover, t.AccentPressed = a.Mix(black, 0.12), a.Mix(black, 0.25)
		t.Selection, t.Focus = a.Alpha(0.25), a.Alpha(0.55)
	}

	// 主色上的字：白底太亮看不清就反过来用深色。Biu 的绿 (#1ed760)
	// 正好是要走这一支的。
	t.AccentText = white
	if luminance(a) > 165 {
		t.AccentText = ui.Color{R: 24, G: 24, B: 27, A: 255}
	}
}

// luminance 是 sRGB 相对亮度，和 ui.Color 里那个未导出的 gray() 同算法。
//
// 这里重写一份是因为它没导出。本包不能靠反射去戳 ui 的内部状态。
func luminance(c ui.Color) float32 {
	return 0.2126*float32(c.R) + 0.7152*float32(c.G) + 0.0722*float32(c.B)
}

// parseHexColor 解析 #rgb / #rgba / #rrggbb / #rrggbbaa。
//
// 不用 `ui.Hex`：它对不合法的输入**直接 panic**（ui/color.go:33-38）。
// 颜色串是设置文件里的用户输入，为了一个画错的颜色把整个进程带走不划算，
// 这里返回 ok=false 让调用方退回默认值。
func parseHexColor(s string) (ui.Color, bool) {
	h, ok := strings.CutPrefix(strings.TrimSpace(s), "#")
	if !ok {
		return ui.Color{}, false
	}

	// 先拆成 0~15 的单个 hex 位（nibble），再按需要拼。
	// ⚠️ 这一步不能省：直接两位一组 ParseUint 拿到的是**字节**，
	// 再拿它 `v[0]<<4|v[1]` 就成了字节左移 4 位——#1ed760 会变成
	// R=0x1e<<4|0xd7 = 0xf7，一整片红。两种拼法混用是这个坑的全部。
	d, ok := hexDigits(h)
	if !ok {
		return ui.Color{}, false
	}
	switch len(d) {
	case 3, 4: // #rgb / #rgba：每一位翻倍
		c := ui.Color{
			R: d[0]*16 + d[0],
			G: d[1]*16 + d[1],
			B: d[2]*16 + d[2],
			A: 255,
		}
		if len(d) == 4 {
			c.A = d[3]*16 + d[3]
		}
		return c, true
	case 6, 8: // #rrggbb / #rrggbbaa
		c := ui.Color{
			R: d[0]<<4 | d[1],
			G: d[2]<<4 | d[3],
			B: d[4]<<4 | d[5],
			A: 255,
		}
		if len(d) == 8 {
			c.A = d[6]<<4 | d[7]
		}
		return c, true
	}
	return ui.Color{}, false
}

// hexDigits 把 hex 串拆成单个 hex 位（0~15）。位数不对或者有非 hex 字符就 false。
func hexDigits(h string) ([]uint8, bool) {
	if len(h) != 3 && len(h) != 4 && len(h) != 6 && len(h) != 8 {
		return nil, false
	}
	d := make([]uint8, len(h))
	for i := range h {
		n, err := strconv.ParseUint(h[i:i+1], 16, 8)
		if err != nil {
			return nil, false
		}
		d[i] = uint8(n)
	}
	return d, true
}
