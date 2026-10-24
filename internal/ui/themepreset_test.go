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

// TestTheSidebarTakesThePresetsOwnColor：套了方案，侧栏底是方案里的 `sidebar`，
// 不是内容区的 Background；经典沿用 Background（侧栏和内容区同色，靠右缘那条线分界）。
func TestTheSidebarTakesThePresetsOwnColor(t *testing.T) {
	for _, tc := range []struct {
		preset app.ThemePreset
		dark   bool
	}{
		{app.ThemePresetWarmPaper, false}, {app.ThemePresetWarmPaper, true},
		{app.ThemePresetDuskAmber, true}, {app.ThemePresetSoftPeach, false},
	} {
		st := app.DefaultSettings("")
		st.ThemePreset = tc.preset
		st.ThemeMode = app.ThemeModeLight
		if tc.dark {
			st.ThemeMode = app.ThemeModeDark
		}
		tt, _, _, _ := shellOnSettings(t, st)
		sk, _, _ := presetSkinFor(tc.preset, tc.dark)
		want := rgb(mygoui.Hex(sk.sidebar))
		if got := tt.Image().RGBAAt(3, 560); !sameColor(got, want) {
			t.Errorf("%s dark=%v：侧栏底是 %v，该是方案的 sidebar %v", tc.preset, tc.dark, got, want)
		}
		if mygoui.Hex(sk.sidebar) == mygoui.Hex(sk.bg) {
			t.Errorf("%s dark=%v：sidebar 和 bg 同色，侧栏就分不出层次了", tc.preset, tc.dark)
		}
	}

	st := app.DefaultSettings("")
	st.ThemePreset = app.ThemePresetClassic
	st.ThemeMode = app.ThemeModeLight
	tt, _, _, _ := shellOnSettings(t, st)
	bg := rgb(BuildTheme(st, false).Background)
	if got := tt.Image().RGBAAt(3, 560); !sameColor(got, bg) {
		t.Errorf("经典：侧栏底是 %v，该沿用 Background %v", got, bg)
	}
}

// TestTheSelectedSideItemUsesTheAccentSoftBase：选中的侧栏项底是强调色淡底
// （`accentSoft`），字在这层底上仍 ≥ 4.5:1。
func TestTheSelectedSideItemUsesTheAccentSoftBase(t *testing.T) {
	for _, name := range allPresets {
		for _, dark := range []bool{false, true} {
			s := app.DefaultSettings("")
			s.ThemePreset = name
			s.ThemeMode = app.ThemeModeLight
			if dark {
				s.ThemeMode = app.ThemeModeDark
			}
			th := BuildTheme(s, false)
			if got, want := flatCurrentBg(th), accentSoft(th); got != want {
				t.Errorf("%s dark=%v：选中底 %v，该是 accentSoft %v", name, dark, got, want)
			}
			if r := presetRatio(flatCurrentText(th, true), flatCurrentBg(th)); r < 4.5 {
				t.Errorf("%s dark=%v：选中项的字压在淡底上只有 %.2f:1", name, dark, r)
			}
		}
	}
}

// TestThePresetCardsAreFourEqualCards：设置页「主题方案」是四张等宽的预览卡
// （暖纸 / 暮光 / 柔雾 / 经典），名字都画得出来、没被省略号吃掉。
func TestThePresetCardsAreFourEqualCards(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	var xs []float32
	for _, p := range themePresets {
		r := settingRect(t, tt, s, p.label)
		xs = append(xs, r.X+r.W/2)
	}
	if len(xs) != 4 {
		t.Fatalf("方案卡有 %d 张，want 4", len(xs))
	}
	step := xs[1] - xs[0]
	if step < 40 {
		t.Fatalf("相邻两张卡的中心只差 %.1f，卡片被挤扁了", step)
	}
	for i := 2; i < len(xs); i++ {
		if d := xs[i] - xs[i-1] - step; d > 1 || d < -1 {
			t.Errorf("第 %d 张和前一张的间距是 %.1f，其余是 %.1f——四张卡不等宽", i, xs[i]-xs[i-1], step)
		}
	}
}

// TestToastAndConfirmTextIsReadableOnEveryPresetColor：方案的语义色当底时（Toast、
// 确认键），字色在每套、每档深浅下都 ≥ 3:1；经典的惯用字色不被换掉。
func TestToastAndConfirmTextIsReadableOnEveryPresetColor(t *testing.T) {
	for _, name := range append(append([]app.ThemePreset{}, allPresets...), app.ThemePresetClassic) {
		for _, dark := range []bool{false, true} {
			s := app.DefaultSettings("")
			s.ThemePreset = name
			s.ThemeMode = app.ThemeModeLight
			if dark {
				s.ThemeMode = app.ThemeModeDark
			}
			th := BuildTheme(s, false)
			for tone, bg := range map[string]mygoui.Color{"success": th.Success, "warning": th.Warning, "danger": th.Danger} {
				for _, pref := range []mygoui.Color{toastOnSemantic, toastOnDanger, confirmOnWarning, dialogOnDanger} {
					got := readableOn(bg, pref)
					if r := presetRatio(got, bg); r < 3 {
						t.Errorf("%s dark=%v %s：字压在底上只有 %.2f:1", name, dark, tone, r)
					}
				}
			}
			if name == app.ThemePresetClassic {
				for _, c := range []struct {
					bg, pref mygoui.Color
				}{{th.Success, toastOnSemantic}, {th.Warning, toastOnSemantic}, {th.Danger, toastOnDanger}} {
					if readableOn(c.bg, c.pref) != c.pref {
						t.Errorf("经典 dark=%v：惯用字色被换掉了（经典要一个像素不变）", dark)
					}
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
