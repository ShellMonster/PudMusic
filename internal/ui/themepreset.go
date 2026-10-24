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
	"sync/atomic"

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

// 方案里控件那三档的比例（`applyPreset`）：悬停往文字色推 4%、按下推 8%。
//
// ⚠️ **卡片那一档的悬停推的量也是这 4%**（`cardHoverBackground`）——所以这
// 两个数是一对，别在别处另写。
const (
	presetHoverMix   = 0.04
	presetPressedMix = 0.08
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
	t.SurfaceHover = t.Surface.Mix(t.Text, presetHoverMix)
	t.SurfacePressed = t.Surface.Mix(t.Text, presetPressedMix)
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

// cardHoverBackground 是「一张卡片被指针压着」那一档底色：从**卡片自己的底**
// 出发，往文字色推一档。
//
// # 为什么基数是卡片底，而不是页面底或控件层
//
// 常态底是**卡片那一档**（`cardBackground`），于是悬停那一档必须**从它出发**
// 推——推出来的两档在**同一条色阶**上（卡片底 → 卡片底的悬停）。而「常态在
// 页面底、悬停跳到控件层」那一种，悬停看着像是**换了一张卡**，不是这张卡被
// 按住了。
//
// # 推多少：对着上游 `content1 → content2` 那一档
//
// 上游那张关注卡是 HeroUI 的 `<Card isHoverable>`（`follow-list/user-card.tsx:60`），
// 常态 `bg-content1`、悬停 `bg-content2`。HeroUI 的 content1 → content2 是
// **zinc 的一格**：浅色 zinc-0 → zinc-100（#ffffff → #f4f4f5）、深色
// zinc-900 → zinc-800（#18181b → #27272a）。
//
// 而那正好就是我们主题里 `Background → Surface` 走的那一档（`applyBackground`
// 的 `surfaceStep`：浅色往黑 4.3%、深色往白 6.5%）——所以这里用的是**那一
// 档的比例**，基数换成卡片底。经典那一支推出来的值和主题自己的 `SurfaceHover`
// 逐字节相同（浅色 #e9e9ec），因为经典里卡片那一档就是 `Surface`。
//
// ⚠️ **不是 `SurfaceHover` 那一档**：那是**两档**（浅色 8.6%、深色 11.3%），
// 推过去比上游大了一倍，悬停会跳得太远。
//
// # 两支各按各的算法（和主题自己算 `SurfaceHover` 时一样）
//
//   - 经典：底色往黑/白推 `surfaceStep`（`applyBackground` 那个算法）；
//   - 方案：往文字色推 `presetHoverMix`（`applyPreset` 那个算法）。
func (s *Shell) cardHoverBackground(t *ui.Theme, cardBg ui.Color) ui.Color {
	if _, _, ok := presetSkinFor(s.app.SettingsSnapshot().ThemePreset, t.Dark); ok {
		return cardBg.Mix(t.Text, presetHoverMix)
	}
	mix := ui.Color{A: 255} // 黑
	step := surfaceStepLight
	if t.Dark {
		mix = ui.Color{R: 255, G: 255, B: 255, A: 255}
		step = surfaceStepDark
	}
	return cardBg.Mix(mix, step)
}

// cardStyle 是这一帧弹窗面板用的底色和圆角。
type cardStyle struct {
	bg     ui.Color
	radius float32
}

// frameCard 是「这一帧的弹窗面板长什么样」：`applyFrameTheme` 每帧开头发布，
// 弹窗（`confirmModal` 那类不挂在 Shell 上的自由函数）读。
//
// 为什么要发布：`ui.Theme` 里没有「卡片底色」这个字段，而自由函数拿不到 Shell。
// 同一个进程里所有窗口共用同一份设置，所以这是一份全局值而不是每窗口一份。
var frameCard atomic.Pointer[cardStyle]

// publishCard 在每帧开头算好弹窗面板的样子并发布。方案里的 `surface1` / `radiusLg`；
// 经典保持原样（底色 = Background，圆角 = 2.5 个间距单位），一个像素不动。
func (s *Shell) publishCard(t *ui.Theme) {
	cs := cardStyle{bg: t.Background, radius: t.Space(2.5)}
	if sk, shape, ok := presetSkinFor(s.app.SettingsSnapshot().ThemePreset, t.Dark); ok {
		cs = cardStyle{bg: ui.Hex(sk.surface1), radius: shape.radiusLg}
	}
	frameCard.Store(&cs)
}

// dialogCard 读这一帧发布的弹窗面板样子；还没发布过（测试里直接画一块）就用经典那份。
func dialogCard(t *ui.Theme) cardStyle {
	if p := frameCard.Load(); p != nil {
		return *p
	}
	return cardStyle{bg: t.Background, radius: t.Space(2.5)}
}
