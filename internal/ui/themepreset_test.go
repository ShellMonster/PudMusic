package ui

import (
	"math"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

var allPresets = []app.ThemePreset{app.ThemePresetWarmPaper, app.ThemePresetDuskAmber, app.ThemePresetSoftPeach}

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

// TestClassicPresetChangesNothing：经典方案和没有这个功能时拼出来的主题一字不差
// （那份「没有方案」的主题就是 classic）。
func TestClassicPresetChangesNothing(t *testing.T) {
	for _, dark := range []bool{false, true} {
		s := app.DefaultSettings("")
		s.ThemePreset = app.ThemePresetClassic
		got := BuildTheme(s, dark)
		s.ThemePreset = app.ThemePreset("不存在的方案")
		if *BuildTheme(s, dark) != *got {
			t.Errorf("dark=%v：不认识的方案改动了主题（该和经典一样不套方案）", dark)
		}
		if got.Accent != mygoui.Hex(app.DefaultPrimaryColor) {
			t.Errorf("dark=%v：经典的强调色不是 Biu 绿：%v", dark, got.Accent)
		}
	}
}

// TestPresetOverridesUserColorsAndRadius：选了方案就压过主色 / 底色 / 圆角。
func TestPresetOverridesUserColorsAndRadius(t *testing.T) {
	for _, name := range allPresets {
		for _, dark := range []bool{false, true} {
			s := app.DefaultSettings("")
			s.ThemePreset = name
			s.PrimaryColor = "#00ff00"
			s.BackgroundColor = "#123456"
			s.BorderRadius = 20
			s.ThemeMode = app.ThemeModeLight
			if dark {
				s.ThemeMode = app.ThemeModeDark
			}
			th := BuildTheme(s, false)
			sk, shape, _ := presetSkinFor(name, dark)
			if th.Dark != dark {
				t.Fatalf("%s dark=%v：深浅不对", name, dark)
			}
			if th.Background != mygoui.Hex(sk.bg) || th.Accent != mygoui.Hex(sk.accent) || th.Radius != shape.radius {
				t.Errorf("%s dark=%v：方案没压过用户设置：bg=%v accent=%v radius=%v", name, dark, th.Background, th.Accent, th.Radius)
			}
		}
	}
}

// TestPresetContrast：规范表里的对比度是承诺，实算不得低于它。
func TestPresetContrast(t *testing.T) {
	for _, name := range allPresets {
		for _, dark := range []bool{false, true} {
			sk, _, _ := presetSkinFor(name, dark)
			bg, text1, text2 := mygoui.Hex(sk.bg), mygoui.Hex(sk.text1), mygoui.Hex(sk.text2)
			acc, on := mygoui.Hex(sk.accent), mygoui.Hex(sk.onAccent)
			checks := []struct {
				what string
				got  float64
			}{
				{"正文/底", presetRatio(text1, bg)},
				{"副文字/底", presetRatio(text2, bg)},
				{"强调色/底（当文字用）", presetRatio(acc, bg)},
				{"强调色上的字", presetRatio(on, acc)},
			}
			for _, c := range checks {
				if c.got < 4.5 {
					t.Errorf("%s dark=%v %s 只有 %.2f:1，不到 4.5", name, dark, c.what, c.got)
				}
			}
		}
	}
}

// TestPresetIsInTheThemeCacheKey：换方案下一帧就得换主题（缓存键里要有方案）。
func TestPresetIsInTheThemeCacheKey(t *testing.T) {
	var c themeCache
	s := app.DefaultSettings("")
	s.ThemePreset = app.ThemePresetClassic
	a := c.of(s, false, mygoui.Preferences{})
	s.ThemePreset = app.ThemePresetDuskAmber
	b := c.of(s, false, mygoui.Preferences{})
	if a == b || a.Accent == b.Accent {
		t.Fatal("换了方案缓存还在给旧主题")
	}
	if c.of(s, false, mygoui.Preferences{}) != b {
		t.Error("方案没变，缓存应该复用同一份")
	}
}

// TestPickingAPresetOnTheSettingsPage：设置页点一档方案 → 设置里变了、主题下一帧换了；
// 再去拖圆角滑块，方案被清回经典（改了就得有反应，不能被方案压住）。
func TestPickingAPresetOnTheSettingsPage(t *testing.T) {
	start := app.DefaultSettings("")
	start.ThemePreset = app.ThemePresetWarmPaper
	tt, s, a, _ := shellOnSettings(t, start)

	if got := a.SettingsSnapshot().ThemePreset; got != app.ThemePresetWarmPaper {
		t.Fatalf("起点方案是 %q，want 暖纸", got)
	}
	clickSetting(t, tt, s, "暮光")
	tt.Frame()
	if got := a.SettingsSnapshot().ThemePreset; got != app.ThemePresetDuskAmber {
		t.Fatalf("点「暮光」之后方案是 %q", got)
	}
	got := s.theme.of(a.SettingsSnapshot(), false, mygoui.Preferences{})
	sk, _, _ := presetSkinFor(app.ThemePresetDuskAmber, got.Dark)
	if got.Accent != mygoui.Hex(sk.accent) {
		t.Errorf("方案选了，主题强调色还是 %v", got.Accent)
	}

	r, ok := tt.Find("全局圆角")
	if !ok {
		t.Fatalf("设置页上没有圆角滑块，文字有 %q", tt.Texts())
	}
	x, y := r.X+r.W*0.85, r.Y+r.H/2
	tt.Press(x, y)
	tt.Frame()
	tt.Release(x, y)
	tt.Frame()
	if got := a.SettingsSnapshot().ThemePreset; got != app.ThemePresetClassic {
		t.Errorf("拖了圆角之后方案还是 %q，应当回到经典", got)
	}
}
