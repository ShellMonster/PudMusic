// 主题方案：设计规范 v0.1 §2 的三套配色（暖纸·赤陶 / 暮光·琥珀 / 柔雾·蜜桃）。
//
// 每套含浅色、深色各一份，深色是**独立调校**的，不是浅色反相。数值逐个抄自规范的
// token 表（含对比度），改这里之前先改规范。
//
// ## 套在哪一步
//
// `buildTheme` 先照老办法拼出主题（框架默认 + 用户的底色/主色/圆角），**最后**
// 才把方案盖上去：方案选了就压过 `primaryColor` / `backgroundColor` /
// `borderRadius` 三项。空方案（经典）一个字节都不动，见 `app.ThemePreset`。
//
// 🔴 铁律一不变：方案是 `buildTheme` 的一部分，每帧开头照样装一次；`themeKey`
// 里带着方案名，换方案下一帧就生效。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// presetSkin 是一套方案在一种深浅下的颜色。
type presetSkin struct {
	bg, sidebar, surface1, surface2, text1, text2, accent, onAccent string
	// accentSoftA / borderA / focusA 是透明度（0~1）：选中底、描边、焦点环。
	accentSoftA, borderA, focusA float32
	// danger / success / warning 是语义色（三套共用，浅深各一份）。
	danger, success, warning string
}

// presetShape 是一套方案的形状：圆角和行高（行高给列表用，主题里没有这个字段）。
type presetShape struct {
	radius    float32
	rowHeight float32
	// radiusLg 是卡片、弹层的圆角（规范 §3.2 的 radiusLg）。
	radiusLg float32
}

type preset struct {
	light, dark presetSkin
	shape       presetShape
}

// 语义色：规范 §2.3，三套共用。
const (
	presetDangerLight, presetDangerDark   = "#C93B3B", "#F0716B"
	presetSuccessLight, presetSuccessDark = "#2E7D4F", "#5BC287"
	presetWarningLight, presetWarningDark = "#9A5B06", "#F2B04A"
)

var presets = map[app.ThemePreset]preset{
	app.ThemePresetWarmPaper: {
		light: presetSkin{"#F8F4ED", "#EFE9DE", "#FDFBF7", "#F1EBE0", "#2A2420", "#6B6158", "#A8552E", "#FFFFFF", 0.14, 0.12, 0.60, presetDangerLight, presetSuccessLight, presetWarningLight},
		dark:  presetSkin{"#1C1815", "#231E1A", "#2A2420", "#342D28", "#F3ECE3", "#B8AC9F", "#E58A5A", "#1C1409", 0.18, 0.14, 0.70, presetDangerDark, presetSuccessDark, presetWarningDark},
		shape: presetShape{radius: 8, rowHeight: 40, radiusLg: 12},
	},
	app.ThemePresetDuskAmber: {
		light: presetSkin{"#FAF5EA", "#F1E9D8", "#FFFDF8", "#F3EAD8", "#2B2218", "#6A5C48", "#9A5F08", "#FFFFFF", 0.14, 0.12, 0.60, presetDangerLight, presetSuccessLight, presetWarningLight},
		dark:  presetSkin{"#16120E", "#1D1813", "#26201A", "#302922", "#F6EEE2", "#BDAF9C", "#E9A23B", "#1A1208", 0.18, 0.14, 0.70, presetDangerDark, presetSuccessDark, presetWarningDark},
		shape: presetShape{radius: 10, rowHeight: 44, radiusLg: 14},
	},
	app.ThemePresetSoftPeach: {
		light: presetSkin{"#FFF9F5", "#FBEFE8", "#FFFFFF", "#FBEEE6", "#2E2523", "#6F605C", "#B4492E", "#FFFFFF", 0.12, 0.10, 0.55, presetDangerLight, presetSuccessLight, presetWarningLight},
		dark:  presetSkin{"#1F1A1E", "#262026", "#2E272E", "#382F38", "#F7EEF0", "#C0B2B8", "#F2937A", "#2A140D", 0.18, 0.14, 0.70, presetDangerDark, presetSuccessDark, presetWarningDark},
		shape: presetShape{radius: 12, rowHeight: 44, radiusLg: 18},
	},
}

// presetSkinFor 取方案在这一档深浅下的颜色；经典（或不认识的名字）返回 false。
func presetSkinFor(p app.ThemePreset, dark bool) (presetSkin, presetShape, bool) {
	pr, ok := presets[p]
	if !ok {
		return presetSkin{}, presetShape{}, false
	}
	if dark {
		return pr.dark, pr.shape, true
	}
	return pr.light, pr.shape, true
}

// applyPreset 把方案盖到已经拼好的主题上。经典（空方案）什么都不做。
//
// 映射（规范 token → `ui.Theme`）：
//
//	bg        → Background
//	surface2  → Surface（控件那一层）；SurfaceHover / SurfacePressed 往文字色推 4% / 8%
//	text1     → Text；text2 → TextMuted
//	border    → Border = text1 @ borderA（透明度，换底色自动适配）
//	accent    → 走 `applyAccent`，再把 AccentText 换成方案的 onAccent，Focus 换成方案的焦点环
//
// sidebar / surface1 在 `ui.Theme` 里没有对应字段，**这一版不接**：侧栏仍用
// Background。要接得单开一票改侧栏和卡片的画法。
func applyPreset(t *ui.Theme, name app.ThemePreset) {
	sk, shape, ok := presetSkinFor(name, t.Dark)
	if !ok {
		return
	}
	hex := func(s string) ui.Color { return ui.Hex(s) }

	t.Background = hex(sk.bg)
	t.Text, t.TextMuted = hex(sk.text1), hex(sk.text2)
	t.Surface = hex(sk.surface2)
	t.SurfaceHover = t.Surface.Mix(t.Text, 0.04)
	t.SurfacePressed = t.Surface.Mix(t.Text, 0.08)
	t.Border = t.Text.Alpha(sk.borderA)

	acc := hex(sk.accent)
	applyAccent(t, acc)
	t.AccentText = hex(sk.onAccent)
	t.Focus = acc.Alpha(sk.focusA)

	t.Danger, t.Success, t.Warning = hex(sk.danger), hex(sk.success), hex(sk.warning)
	t.Radius = shape.radius
}

// sidebarBackground 是侧栏这一帧的底色：方案里的 `sidebar`（规范 §2），经典不套方案、
// 沿用主题的 Background。
func (s *Shell) sidebarBackground(t *ui.Theme) ui.Color {
	if sk, _, ok := presetSkinFor(s.app.SettingsSnapshot().ThemePreset, t.Dark); ok {
		return ui.Hex(sk.sidebar)
	}
	return t.Background
}

// accentSoft 是「强调色的淡底」：底色往强调色推一点（规范 §2 的 `accentSoft`，
// 浅色 12~14%、深色 18%）。选中行、选中的左栏项用它做底。
func accentSoft(t *ui.Theme) ui.Color {
	a := float32(0.14)
	if t.Dark {
		a = 0.18
	}
	return t.Background.Mix(t.Accent, a)
}

// cardBackground 是卡片、弹层的底色：方案里的 `surface1`；经典沿用控件那一层 `Surface`。
func (s *Shell) cardBackground(t *ui.Theme) ui.Color {
	if sk, _, ok := presetSkinFor(s.app.SettingsSnapshot().ThemePreset, t.Dark); ok {
		return ui.Hex(sk.surface1)
	}
	return t.Surface
}

// cardRadius 是卡片、弹层的圆角：方案里的 `radiusLg`；经典就是主题圆角的 1.5 倍。
func (s *Shell) cardRadius(t *ui.Theme) float32 {
	if _, shape, ok := presetSkinFor(s.app.SettingsSnapshot().ThemePreset, t.Dark); ok {
		return shape.radiusLg
	}
	return t.Radius * 1.5
}
