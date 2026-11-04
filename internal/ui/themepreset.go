// 主题：「深夜唱片行」——应用唯一的一套主题，分深色、浅色两份（DESIGN.md §0′）。
//
// 2026-11 用户拍板：暖纸、暮光、柔雾、青瓷、墨宣、紫檀、靛夜、经典八套方案全部
// 删掉，自定义主色、自定义底色、「定时」档也一起删掉。强调色四选一（朱砂 / 靛青 /
// 松绿 / 琥珀）外加「封面取色」。
//
// 颜色令牌的唯一出处是 `widgets/tok`（数值抄自原型 `index.html` 69–120 行）。这个
// 文件只管「卡片那一档」这几样 `ui.Theme` 里没有的东西怎么按帧算、怎么缓存。
//
// 🔴 铁律一不变：主题在 `applyFrameTheme` 里每帧开头装一次（见 `theme.go`）。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// 圆角令牌（原型 `--r-4` / `--r-8` / `--r-12` / `--r-pill`）。
//
//	radiusSm   = 4   小封面、kbd、角标
//	radiusMd   = 8   控件、列表行、封面（也是 `t.Radius`）
//	radiusLg   = 12  卡片、抽屉、弹层、对话框、主视觉区
//	radiusPill = 999 按钮、Chip、搜索框、Toast（框架按高度的一半画）
//
// 公共组件那边用的是 `widgets/pud` 里同名的导出常量，值是同一份。
const (
	radiusSm   float32 = 4
	radiusMd   float32 = 8
	radiusLg   float32 = 12
	radiusPill float32 = 999
)

// sidebarBackground 是侧栏这一帧的底色（`--side`）。
func (s *Shell) sidebarBackground(t *ui.Theme) ui.Color {
	return s.cardStyleOf(t).sidebar
}

// accentSoft 是强调色的淡底（`--accent-soft`，合到 bg 上的不透明色，见 `tok.Palette`）。
func accentSoft(t *ui.Theme) ui.Color {
	return tok.Of(t).AccentSoft
}

// cardPalette 是「卡片那一档」的几个值：底色、圆角、悬停底、侧栏底。
//
// 它们只依赖深浅和强调色——和主题本身是同一层的东西，所以跟着主题一起算、一起
// 缓存（`themeCache.card`），而不是每张卡各算一遍（票 #628 / #632 的理由仍成立）。
type cardPalette struct {
	// bg 是卡片、弹层、抽屉、对话框的底色：`--surface`。
	bg ui.Color
	// radius 是它们的圆角：`--r-12`。
	radius float32
	// hover 是一张卡被指针压着时的底：surface 上叠一层 `--hover`。
	hover ui.Color
	// sidebar 是侧栏、播放条的底色：`--side`。
	sidebar ui.Color
}

// cardPaletteOf 按主题算卡片那一档。纯函数：不读设置、不分配。
func cardPaletteOf(t *ui.Theme) cardPalette {
	p := tok.Of(t)
	return cardPalette{
		bg:      p.Surface,
		radius:  radiusLg,
		hover:   composite(p.Surface, p.Hover),
		sidebar: p.Side,
	}
}

// composite 把半透明的 fg 合到不透明的 bg 上，得到不透明色。
func composite(bg, fg ui.Color) ui.Color {
	return bg.Mix(ui.Color{R: fg.R, G: fg.G, B: fg.B, A: 255}, float32(fg.A)/255)
}

// cardBackground 是卡片、弹层的底色（`--surface`）。
func (s *Shell) cardBackground(t *ui.Theme) ui.Color {
	return s.cardStyleOf(t).bg
}

// cardRadius 是卡片、弹层的圆角（12）。
func (s *Shell) cardRadius(t *ui.Theme) float32 {
	return s.cardStyleOf(t).radius
}

// cardHoverBackground 是「一张卡片被指针压着」那一档底色：从卡片自己的底出发叠
// 一层 `--hover`（原型 `.ucard:hover` 是换到 surface，那一处在 T3 自己画）。
func (s *Shell) cardHoverBackground(t *ui.Theme) ui.Color {
	return s.cardStyleOf(t).hover
}

// cardStyleOf 取出这一帧该用的卡片那一份：缓存里有、深浅也对得上就用缓存，
// 否则现算（测试里拿 `BuildTheme` 现拼的主题会走到现算那一支）。
func (s *Shell) cardStyleOf(t *ui.Theme) cardPalette {
	if s.theme.valid && t.Dark == s.theme.key.dark && t.Accent == s.theme.base.Accent {
		return s.theme.card
	}
	return cardPaletteOf(t)
}

// cardStyle 是弹窗面板用的底色和圆角。
type cardStyle struct {
	bg     ui.Color
	radius float32
}

// dialogCard 是弹窗面板（确认框、关于、更新日志那类不挂在 Shell 上的自由函数）
// 的样子：`--surface` 底、12 圆角。只有一套主题，按深浅现取就是，不用再每帧发布。
func dialogCard(t *ui.Theme) cardStyle {
	return cardStyle{bg: tok.Of(t).Surface, radius: radiusLg}
}
