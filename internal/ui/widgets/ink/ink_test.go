package ink

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// 这一组量的是**对比度**，不是「颜色变没变」。
//
// 默认绿 `#1ed760` 压在浅色底上只有 1.9:1，而 WCAG 正文 AA 要 4.5:1——这就是
// 这一票要修的那件事。所以判据取 `Ratio` 算出来的数，而 `Ratio` 自己先用
// 已知值验一遍（见 TestRatioMatchesTheWCAGPair）。

// TestRatioMatchesTheWCAGPair 先用 WCAG 里公布的一对已知值验 Ratio 本身。
//
// 黑白是 21:1；`#777777` 压白是 4.48:1（WCAG 文档里那个著名的「差一点」的例子）。
// 两条都要对，否则下面那些「≥4.5」的断言是拿一把没刻度的尺量出来的。
func TestRatioMatchesTheWCAGPair(t *testing.T) {
	if got := Ratio(ui.Color{R: 255, G: 255, B: 255, A: 255}, ui.Color{A: 255}); math.Abs(got-21) > 0.05 {
		t.Errorf("黑白对比度 = %.3f，want 21", got)
	}
	gray := ui.Color{R: 0x77, G: 0x77, B: 0x77, A: 255}
	if got := Ratio(gray, ui.Color{R: 255, G: 255, B: 255, A: 255}); math.Abs(got-4.48) > 0.02 {
		t.Errorf("#777 压白 = %.3f，want 4.48（WCAG 那个著名例子）", got)
	}
}

// TestTheAccentAsTextIsReadableInLightThemes 是这一票的核心判据。
//
// # 默认绿
//
// 报告里实测：主色压纯白 = 1.92:1、压浅色底 = 1.75:1，而 WCAG 正文 AA 要 4.5:1。
// `AccentOn` 必须从**用户选的那个主色**算出一个达标的变体。
//
// # 自定义主色
//
// 主色是用户可设的（设置页「主色」那一格），所以这里换几个色再算一遍——写死
// 一个绿的话，这一条会红。用的是几个亮度、色相都不同的色：深红、亮黄、中蓝、
// 灰。挑的这几个里有两个（绿、黄）在浅色底上本来就读不出来。
func TestTheAccentAsTextIsReadableInLightThemes(t *testing.T) {
	accents := []string{
		"#1ed760", // Biu 的绿（默认）
		"#ff0000", // 深红
		"#ffe600", // 亮黄
		"#2563eb", // 中蓝
		"#808080", // 灰
		"#00ffff", // 青
	}
	for _, hex := range accents {
		light := ui.LightTheme()
		light.Accent = ui.Hex(hex)
		got := AccentOn(light)
		if r := Ratio(got, light.Background); r < 4.5 {
			t.Errorf("主色 %s 当字色时与底色的对比度 = %.2f:1（< 4.5），算出来的颜色是 %v",
				hex, r, got)
		}
	}
}

// TestTheDarkAccentIsLeftAlone 钉住「深色下不变」。
//
// 深色下那个绿压深灰是 9.2:1，已经够；把它也往白里混的话，主色就不再是用户挑的
// 那个色了（票面明写「深色下不变」）。这里连**同一个值**都钉住，不只是「够亮」。
func TestTheDarkAccentIsLeftAlone(t *testing.T) {
	dark := ui.DarkTheme()
	dark.Accent = ui.Hex("#1ed760")
	if got, want := AccentOn(dark), dark.Accent; got != want {
		t.Errorf("深色下主色当字色被改成了 %v，want 原样 %v", got, want)
	}
}

// TestTheVariantKeepsTheHueWhereItCan 验变体不是「一律换成黑/白」。
//
// 用户挑了一个能在浅底上读出来的主色（比如深蓝）时，原样就该够——这时候返回的
// 必须**就是那个色**，不能被无差别地压成黑。这一条防的是「所有主色都走一遍混合」
// 那种偷懒实现（它会让设置里挑的颜色和界面上看到的对不上）。
func TestTheVariantKeepsTheHueWhereItCan(t *testing.T) {
	light := ui.LightTheme()
	light.Accent = ui.Hex("#1d4ed8") // 深蓝，压白本来就 > 4.5:1
	if got, want := AccentOn(light), light.Accent; got != want {
		t.Errorf("已经达标的主色被改成了 %v，want 原样 %v", got, want)
	}
}

// TestTheDisabledInkSurvivesTheFrameworkFade 是禁用态那一条。
//
// MyGo 把禁用元素整个乘 50% 不透明度（`mygo/ui/paint.go:154-156`）。所以判据要
// **先合一次 50%**、再算对比度：算的是屏幕上那个灰，不是 ink 里存的那个色。
//
// 深色下那个「选中」的主题按钮原来是主色绿：绿乘完 0.5 压在选中底上只剩
// 2.6:1——用户以为三个选项消失了。`Disabled` 换的是一个扛得住 0.5 的灰。
func TestTheDisabledInkSurvivesTheFrameworkFade(t *testing.T) {
	for name, t2 := range map[string]*ui.Theme{
		"浅色": ui.LightTheme(),
		"深色": ui.DarkTheme(),
	} {
		got := Disabled(t2)
		shown := got.Alpha(0.5).Over(t2.Background)
		if r := Ratio(shown, t2.Background); r < 3 {
			t.Errorf("%s：禁用字 %v 乘完框架的 50%% 之后与底色 %v 的对比度 = %.2f:1（< 3）",
				name, got, t2.Background, r)
		}
	}
}

// TestEnsureNeverReturnsSomethingWorse 验 Ensure 在已经达标时不改色、在不达标时
// 真的往对的方向走。
func TestEnsureNeverWorse(t *testing.T) {
	white := ui.Color{R: 255, G: 255, B: 255, A: 255}
	blue := ui.Hex("#0000ff")
	if got := Ensure(blue, white, 4.5); got != blue {
		t.Errorf("已经达标的蓝色被改成了 %v", got)
	}
	green := ui.Hex("#1ed760")
	got := Ensure(green, white, 4.5)
	if Ratio(got, white) < 4.5 {
		t.Fatalf("Ensure 之后还是不达标：%v → %.2f", got, Ratio(got, white))
	}
	if Luminance(got) >= Luminance(green) {
		t.Errorf("浅底上应该往黑混，结果 %v 不比原来的绿暗", got)
	}
}
