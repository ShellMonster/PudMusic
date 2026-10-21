package ui

import (
	"image"
	"image/color"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scrollbar"
	"github.com/egoist/mygo/ui"
	mygoui "github.com/egoist/mygo/ui"
)

// 这一份量的是票 #139 那几件「系统偏好和读得出来」的事。全部走真框架、真像素：
// 这些行为改的恰恰是**画出来的颜色**，读结构体只能验一半。
//
// ⚠️ 机器的负载会让像素飘吗：这几条都只画一帧、不依赖时间，指针移到哪儿是确定的。

// deepestIn 在一块矩形里找**离底色最远**的像素。
//
// 字有抗锯齿，边缘是一圈半透明的过渡色；字色本身在笔画中心那一块，也就是离底色
// 最远的那个。拿它当「字色」去算对比度，比拿平均色更接近用户实际读到的那一层。
func deepestIn(img *image.RGBA, r mygoui.Rect, bg color.RGBA) color.RGBA {
	best, bestD := bg, -1
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			if !(image.Point{x, y}).In(img.Rect) {
				continue
			}
			c := img.RGBAAt(x, y)
			if d := colorDist(c, bg); d > bestD {
				best, bestD = c, d
			}
		}
	}
	return best
}

// colorDist 是两个像素的通道差之和（只拿来找「墨迹最深的那一点」）。
func colorDist(a, b color.RGBA) int {
	d := func(x, y uint8) int { return abs(int(x) - int(y)) }
	return d(a.R, b.R) + d(a.G, b.G) + d(a.B, b.B)
}

// contrastOf 是两个不透明像素的 WCAG 对比度。
func contrastOf(a, b color.RGBA) float64 {
	toUI := func(c color.RGBA) ui.Color {
		return ui.Color{R: c.R, G: c.G, B: c.B, A: 255}
	}
	return ink.Ratio(toUI(a), toUI(b))
}

// TestHighContrastDarkensBordersAndSecondaryText 验系统「增加对比度」接上了。
//
// 框架的默认主题会 follow 这一档（`mygo/ui/preferences.go:72-77`），而我们是从
// `LightTheme()` / `DarkTheme()` 起步的——**绕开了那一层**。不接的话，用户在系统
// 设置里打开「增加对比度」，界面上一个像素都不变。
//
// 两条判据：
//   - 结构体：Alternate 之后 Border / TextMuted 变了，Focus 不透明；
//   - 画面：边框和次要文字都**更深**（浅色下）。
func TestHighContrastDarkensBordersAndSecondaryText(t *testing.T) {
	s := app.DefaultSettings("")
	if got, want := buildTheme(s, false, ui.Preferences{HighContrast: true}).Border, BuildTheme(s, false).Border; got == want {
		t.Errorf("高对比度下 Border 还是 %v——偏好没有被接上", got)
	}
	if got, want := buildTheme(s, false, ui.Preferences{HighContrast: true}).TextMuted, BuildTheme(s, false).TextMuted; got == want {
		t.Errorf("高对比度下 TextMuted 还是 %v——偏好没有被接上", got)
	}
	if got := buildTheme(s, false, ui.Preferences{HighContrast: true}).Focus; got.A != 255 {
		t.Errorf("高对比度下焦点环的 alpha 是 %d，want 255（框架那一档是不透明）", got.A)
	}

	view := func(c *mygoui.Context) {
		ApplyTheme(c, s, false)
		t := c.Theme()
		ui.Column(c).Padding(20).Gap(12).Children(func() {
			ui.Box(c).Width(80).Height(30).Border(1, t.Border)
			ui.Text(c, "次要文字").FontSize(14).TextColor(t.TextMuted)
		})
	}
	tt := mygoui.NewTester(view, 300, 200)
	tt.Frame()
	offBorder := deepestIn(tt.Image(), mygoui.Rect{X: 18, Y: 18, W: 84, H: 34},
		tt.Image().RGBAAt(2, 2))
	offText := deepestIn(tt.Image(), mygoui.Rect{X: 20, Y: 56, W: 200, H: 30},
		tt.Image().RGBAAt(2, 2))

	tt.SetPreferences(ui.Preferences{HighContrast: true})
	tt.Frame()
	onBorder := deepestIn(tt.Image(), mygoui.Rect{X: 18, Y: 18, W: 84, H: 34},
		tt.Image().RGBAAt(2, 2))
	onText := deepestIn(tt.Image(), mygoui.Rect{X: 20, Y: 56, W: 200, H: 30},
		tt.Image().RGBAAt(2, 2))

	if luminanceRGBA(onBorder) >= luminanceRGBA(offBorder) {
		t.Errorf("高对比度打开之后边框没有变深：%v → %v", offBorder, onBorder)
	}
	if luminanceRGBA(onText) >= luminanceRGBA(offText) {
		t.Errorf("高对比度打开之后次要文字没有变深：%v → %v", offText, onText)
	}
}

// luminanceRGBA 是 0~255 的亮度（只拿来比「更深」）。
func luminanceRGBA(c color.RGBA) float64 {
	return 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
}

// TestTextScaleEnlargesText 验系统「文本大小」接上了。
//
// 框架只缩放 `Theme.FontSize`（默认字号），而我们的界面绝大多数显式写了
// `FontSize(12)` 这类固定 DIP——所以每个显式字号都过了 `scale.Font`。
//
// 判据：同一个字号，TextScale 1.4 时**画出来的高度**更大。只读 `scale.Font`
// 的返回值不够：那个数有没有真到框架的排版那一步，只有画面说了算。
func TestTextScaleEnlargesText(t *testing.T) {
	measure := func(prefs ui.Preferences) (float32, float32) {
		tt := mygoui.NewTester(func(c *mygoui.Context) {
			ApplyTheme(c, app.DefaultSettings(""), false)
			ui.Text(c, "标题").FontSize(scale.Font(c, 20))
		}, 300, 200)
		tt.SetPreferences(prefs)
		tt.Frame()
		r, ok := tt.Find("标题")
		if !ok {
			t.Fatalf("画面上没有标题，文字有 %q", tt.Texts())
		}
		return r.W, r.H
	}
	_, h1 := measure(ui.Preferences{TextScale: 1})
	_, h2 := measure(ui.Preferences{TextScale: 1.4})
	// ⚠️ 只量高度：`ui.Text` 在根上默认占满整行宽（300），宽度不随字号变。
	if h2 <= h1 {
		t.Errorf("TextScale 1.4 之后字没有变高：%.1f → %.1f", h1, h2)
	}
}

// TestACustomBackgroundCarriesTheControls 验「用户改底色，控件的底色和描边跟着走」。
//
// 不跟的话，白底上的按钮还是浅灰、深底上的还是深灰——两个主题的「层次」在自定义
// 底色上就断了。判据同时看结构体和画面：结构体对了而没画上去，用户一样看不见。
func TestACustomBackgroundCarriesTheControls(t *testing.T) {
	s := app.DefaultSettings("")
	s.BackgroundColor = "#101014"
	th := BuildTheme(s, false)
	def := ui.LightTheme()
	if th.Surface == def.Surface {
		t.Errorf("自定义底色之后 Surface 还是默认的 %v", th.Surface)
	}
	if th.Border == def.Border {
		t.Errorf("自定义底色之后 Border 还是默认的 %v", th.Border)
	}

	tt := mygoui.NewTester(func(c *mygoui.Context) {
		ApplyTheme(c, s, false)
		ui.Button(c, "按钮").Width(80)
	}, 200, 100)
	tt.Frame()
	// 按钮的左上角往里去一点：那个点是它的底色（避开 1px 描边和圆角）。
	got := tt.Image().RGBAAt(10, 20)
	want := th.Surface
	if got.R != want.R || got.G != want.G || got.B != want.B {
		t.Errorf("画面上按钮的底色是 %v，want 跟着背景推出来的 %v", got, want)
	}
}

// TestTheDisabledThemeButtonsStayReadable 是禁用态那一条的像素判据。
//
// 只读设置页里那三个主题按钮被框架整个乘 50% 不透明度。原来深色下选中那一档的
// 字是主色绿，乘完只剩 2.6:1——用户以为「这三个选项在深色模式下消失了」。
//
// 判据：每个按钮的**字**（离底色最远的像素）与它底色的对比度 ≥3:1。深色和浅色
// 各量一遍，三个按钮逐个量。
func TestTheDisabledThemeButtonsStayReadable(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt := openSettingsFor(t, dark, true)
			if !tt.HasText("设置只读") {
				t.Fatalf("没有进只读那一版，文字有 %q", tt.Texts())
			}
			for _, label := range []string{"跟随系统", "浅色", "深色"} {
				r, ok := tt.Find(label)
				if !ok {
					t.Fatalf("找不到「%s」，文字有 %q", label, tt.Texts())
				}
				// 按钮的底：取文字框左上角往外让一点。那一块要么是页面底色
				// （未选中的按钮透明），要么是选中那一档的淡底。
				bg := tt.Image().RGBAAt(int(r.X)-6, int(r.Y+r.H/2))
				text := deepestIn(tt.Image(), r, bg)
				if got := contrastOf(text, bg); got < 3 {
					t.Errorf("「%s」禁用之后的字 %v 压底色 %v 只有 %.2f:1（< 3）",
						label, text, bg, got)
				}
			}
		})
	}
}

// TestTheScrollbarIsTenPixelsAndDeepensOnHover 是滚动条那一条。
//
// 上游是 10 像素、指针压上去再深一档（`--os-handle-bg-hover`）。框架只有一个
// 滚动条颜色，也没有悬停档——所以这一票把宽度设成 10，并自己接了「指针在滚动条
// 那一带时换深一档」（`scrollbar.go` + `widgets/scrollbar`）。
func TestTheScrollbarIsTenPixelsAndDeepensOnHover(t *testing.T) {
	s := app.DefaultSettings("")
	if got := BuildTheme(s, false).ScrollbarWidth; got != scrollbar.Width {
		t.Fatalf("滚动条宽 %v，want %v（上游 --os-size: 10px）", got, scrollbar.Width)
	}
	if scrollbar.Width != 10 {
		t.Errorf("scrollbar.Width = %d，want 10", scrollbar.Width)
	}

	// 🔴 **走真实外壳**，不是自己模拟一段：`applyFrameTheme` 读 `s.scrollbarHot`
	// 那一句正是要验的东西，复制一份到测试里就永远抓不住它掉了（实测：把
	// theme.go 里那一段删掉，自模拟的版本照样绿）。设置页够长、有滚动条。
	b := newTestShell(t, at(900, 600), withSettings(s))
	b.s.navigate(settingsPath)
	tt := b.tt
	tt.Frame()
	tt.Move(400, 300) // 指针落在设置页上：框架这时才画滚动条
	tt.Frame()

	// 滚动条 thumb 在内容区（侧栏 200 宽）的右边：x 890 落在 10 像素那一档里，
	// y 100 在 thumb 上（内容很长，thumb 从顶上开始）。
	const barX, barY = 890, 100
	idle := tt.Image().RGBAAt(barX, barY)
	if idle.R == 255 && idle.G == 255 && idle.B == 255 {
		t.Fatalf("设置页右边没有滚动条（(%d,%d) 是底色）——这一条就没有东西可量",
			barX, barY)
	}

	tt.Move(barX, barY) // 压在滚动条那一带
	tt.Frame()          // 这一帧末尾 `trackScrollbar` 才记下 hot
	tt.Frame()          // 这一帧 `applyFrameTheme` 用上深色
	hot := tt.Image().RGBAAt(barX, barY)

	if maxDiff(hot, idle) < 20 {
		t.Errorf("指针压到滚动条上之后颜色只变了 %d 个色阶（%v → %v），"+
			"want 明显更深（上游 --os-handle-bg-hover）", maxDiff(hot, idle), idle, hot)
	}
	if luminanceRGBA(hot) > luminanceRGBA(idle) {
		t.Errorf("滚动条悬停之后反而变亮了：%v → %v", idle, hot)
	}
}
