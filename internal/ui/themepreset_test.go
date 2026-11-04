package ui

import (
	"math"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	mygoui "github.com/egoist/mygo/ui"
)

// 「深夜唱片行」主题的用例（DESIGN.md §0′）：令牌的对比度、强调色进缓存键、
// 侧栏底色、弹窗与 Toast 的字。

func presetLum(c mygoui.Color) float64 {
	f := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c.R) + 0.7152*f(c.G) + 0.0722*f(c.B)
}

func presetRatio(a, b mygoui.Color) float64 {
	x, y := presetLum(a), presetLum(b)
	if x < y {
		x, y = y, x
	}
	return (x + 0.05) / (y + 0.05)
}

// TestTheTokensKeepTheirContrast：design-tokens.html 表 2-6 的对比度是承诺，四种
// 强调色 × 深浅两份实算都不得低于 4.5:1（浅色朱砂当字色那一档本来就不过，所以
// 主色只当底，当字用的是 AccentText）。
func TestTheTokensKeepTheirContrast(t *testing.T) {
	for _, a := range app.Accents {
		for _, dark := range []bool{false, true} {
			p := tok.Base(dark, string(a))
			for _, c := range []struct {
				what string
				got  float64
			}{
				{"正文/bg", presetRatio(p.Text, p.Bg)},
				{"次要/bg", presetRatio(p.Text2, p.Bg)},
				{"第三级/bg", presetRatio(p.Text3, p.Bg)},
				{"第三级/surface", presetRatio(p.Text3, p.Surface)},
				{"主色当字/bg", presetRatio(p.AccentText, p.Bg)},
				{"主色底上的字", presetRatio(p.AccentInk, p.Accent)},
				{"danger/bg", presetRatio(p.Err, p.Bg)},
			} {
				if c.got < 4.5 {
					t.Errorf("%s 深色=%v %s 只有 %.2f:1，不到 4.5", a, dark, c.what, c.got)
				}
			}
		}
	}
}

// TestTokOfMatchesTheBaseForEveryAccent：页面和控件拿的 `tok.Of(主题)` 和装主题用的
// `tok.Base` 是同一组值（`Of` 是从主色反查回强调色的）。
func TestTokOfMatchesTheBaseForEveryAccent(t *testing.T) {
	for _, a := range app.Accents {
		for _, dark := range []bool{false, true} {
			s := app.DefaultSettings("")
			s.Accent = a
			if got, want := tok.Of(BuildTheme(s, dark)), tok.Base(dark, string(a)); got != want {
				t.Errorf("%s 深色=%v：tok.Of = %+v\nwant %+v", a, dark, got, want)
			}
		}
	}
}

// TestTokOfDerivesTheCoverAccent：封面取色给的主色不在四种之内时，主色当字那一档
// 从底色现推到 ≥4.5:1，焦点环跟着主色。
func TestTokOfDerivesTheCoverAccent(t *testing.T) {
	for _, dark := range []bool{false, true} {
		th := themeWithAccent(app.DefaultSettings(""), "#ffe600", dark)
		p := tok.Of(th)
		if r := presetRatio(p.AccentText, p.Bg); r < 4.5 {
			t.Errorf("深色=%v：现推的主色字只有 %.2f:1", dark, r)
		}
		if p.Focus != th.Accent || p.AccentInk != th.AccentText {
			t.Errorf("深色=%v：焦点环 / 底上字没跟着封面色：%+v", dark, p)
		}
	}
}

// TestTheAccentIsInTheThemeCacheKey：换强调色下一帧就得换主题（缓存键里要有它）。
func TestTheAccentIsInTheThemeCacheKey(t *testing.T) {
	var c themeCache
	s := app.DefaultSettings("")
	a := c.of(s, false, mygoui.Preferences{})
	s.Accent = app.AccentAmber
	b := c.of(s, false, mygoui.Preferences{})
	if a == b || a.Accent == b.Accent {
		t.Fatal("换了强调色缓存还在给旧主题")
	}
	if c.of(s, false, mygoui.Preferences{}) != b {
		t.Error("强调色没变，缓存应该复用同一份")
	}
}

// TestTheSidebarTakesTheSideToken：侧栏底是 `--side`，和内容区的 bg 不同色。
func TestTheSidebarTakesTheSideToken(t *testing.T) {
	for _, dark := range []bool{false, true} {
		st := app.DefaultSettings("")
		st.ThemeMode = app.ThemeModeLight
		if dark {
			st.ThemeMode = app.ThemeModeDark
		}
		tt, _, _, _ := shellOnAppearance(t, st)
		p := tok.Base(dark, "cinnabar")
		if got := tt.Image().RGBAAt(3, 560); !sameColor(got, rgb(p.Side)) {
			t.Errorf("深色=%v：侧栏底是 %v，该是 --side %v", dark, got, p.Side)
		}
		if p.Side == p.Bg {
			t.Errorf("深色=%v：side 和 bg 同色，侧栏就分不出层次了", dark)
		}
	}
}

// TestTheSelectedSideItemIsSolidAndReadable：选中的侧栏项（T1 之前的样子）底是
// 实心主色底，字取 `t.AccentText`，压在主色底上 ≥ 3:1。
func TestTheSelectedSideItemIsSolidAndReadable(t *testing.T) {
	for _, a := range app.Accents {
		for _, dark := range []bool{false, true} {
			s := app.DefaultSettings("")
			s.Accent = a
			th := BuildTheme(s, dark)
			if got, want := flatSolidCurrentText(th), th.AccentText; got != want {
				t.Errorf("%s dark=%v：选中项的字 %v，该是 AccentText %v", a, dark, got, want)
			}
			if r := presetRatio(flatSolidCurrentText(th), th.Accent); r < 3 {
				t.Errorf("%s dark=%v：选中项的字压在主色底上只有 %.2f:1", a, dark, r)
			}
		}
	}
}

// TestToastAndConfirmTextIsReadableOnEverySemanticColor：语义色当底时（Toast、确认键），
// 字色在深浅两份下都 ≥ 3:1。
func TestToastAndConfirmTextIsReadableOnEverySemanticColor(t *testing.T) {
	for _, dark := range []bool{false, true} {
		th := BuildTheme(app.DefaultSettings(""), dark)
		for tone, bg := range map[string]mygoui.Color{"success": th.Success, "warning": th.Warning, "danger": th.Danger} {
			for _, pref := range []mygoui.Color{toastOnSemantic, toastOnDanger, confirmOnWarning, dialogOnDanger} {
				got := readableOn(bg, pref)
				if r := presetRatio(got, bg); r < 3 {
					t.Errorf("dark=%v %s：字压在底上只有 %.2f:1", dark, tone, r)
				}
			}
		}
	}
}

// TestThePlaybarShowsACleanTitle：播放条上的歌名是清洗过的，原始的视频标签不出现。
func TestThePlaybarShowsACleanTitle(t *testing.T) {
	st := barState()
	st.Title = "【4K修复】晴天 (官方MV)"
	tt, _ := playbarTester(t, st, 1200, 0, false)
	if !tt.HasText("晴天") {
		t.Fatalf("播放条上没有清洗后的歌名「晴天」，文字有 %q", tt.Texts())
	}
	for _, bad := range []string{"4K修复", "官方MV"} {
		if tt.HasText("【" + bad + "】晴天 (官方MV)") {
			t.Errorf("播放条上还画着没清洗的标题")
		}
	}
}

// cardColorOf 是卡片在这份设置下该是的底色：`--surface`。
func cardColorOf(_ app.Settings, th *mygoui.Theme) mygoui.Color {
	return tok.Of(th).Surface
}
