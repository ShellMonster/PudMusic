// 主题方案的预览卡：设置页「主题方案」那一行。
//
// 每张卡是应用窗口的缩略图（侧栏 / 封面 / 播放条加一枚强调色圆），颜色取自方案在
// **当前深浅**下的色板，卡下面是方案名。选中的那张带 2px 强调色描边和右上角对勾，
// 对应设计规范 §7.9 与概念图 S4。
//
// 缩略图是用元素搭的（几块色块），不是图片：色板一改它自己跟着改，不用重新出图。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// presetThumbH 是缩略图的高。宽由「四张卡平分控件列」算出来（约 69），按
	// 窗口的 3:2 左右取 46。
	presetThumbH = 46
	// presetThumbSide 是缩略图里侧栏那一条的宽。
	presetThumbSide = 14

	// 缩略图和卡片自己的圆角。刻意**不跟主题圆角**走：缩略图是「应用窗口的缩影」，
	// 四张卡要一样大小的圆角才整齐；不然用户把全局圆角拖到 24，缩略图里的小块
	// 会圆成一个点。
	presetThumbRadius = 5 // 缩略图外框
	presetBlockRadius = 2 // 缩略图里的封面小块
	presetDotRadius   = 3 // 缩略图里播放条上那枚圆（直径 6）
)

// presetPalette 是预览卡画缩略图用的那几个颜色。
type presetPalette struct {
	bg, sidebar, cover, accent ui.Color
}

// presetPaletteFor 取方案在这一档深浅下的缩略图颜色。经典不在方案表里，
// 直接从「经典」那份主题里取。
func presetPaletteFor(name app.ThemePreset, dark bool) presetPalette {
	if sk, _, ok := presetSkinFor(name, dark); ok {
		return presetPalette{ui.Hex(sk.bg), ui.Hex(sk.sidebar), ui.Hex(sk.surface2), ui.Hex(sk.accent)}
	}
	s := app.DefaultSettings("")
	s.ThemePreset = app.ThemePresetClassic
	th := BuildTheme(s, dark)
	return presetPalette{th.Background, th.Background, th.Surface, th.Accent}
}

// presetThumb 画缩略图：左边侧栏条，右边 2×2 封面块，底下一条播放条带一枚圆。
func presetThumb(c *ui.Context, p presetPalette, border ui.Color) {
	ui.Row(c).FillWidth().Height(presetThumbH).Radius(presetThumbRadius).Background(p.bg).
		Border(1, border).Clip().Children(func() {
		ui.Column(c).Width(presetThumbSide).FillHeight().Background(p.sidebar).Children(func() {})
		ui.Column(c).Grow(1).FillHeight().Padding(space.Px4).Gap(space.Px3).Children(func() {
			for range 2 {
				ui.Row(c).FillWidth().Grow(1).Gap(space.Px3).Children(func() {
					for range 2 {
						ui.Column(c).Grow(1).FillHeight().Radius(presetBlockRadius).Background(p.cover).Children(func() {})
					}
				})
			}
			ui.Row(c).FillWidth().Height(space.Px6).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Width(space.Px6).Height(space.Px6).Radius(presetDotRadius).Background(p.accent).Children(func() {})
			})
		})
	})
}

// presetCard 画一张预览卡，返回它（调用方问 Clicked）。
func (s *Shell) presetCard(c *ui.Context, name app.ThemePreset, label string, w float32, selected, ro bool) ui.Element {
	t := c.Theme()
	pal := presetPaletteFor(name, t.Dark)

	card := ui.ButtonBase(c).Key("preset:" + string(name)).Width(w).
		Padding(space.Px2).Radius(t.Radius).Disabled(ro)
	ring := ui.Color{}
	switch {
	case selected:
		ring = t.Accent
	case card.Hovered():
		ring = t.TextMuted
	}
	if ring != (ui.Color{}) {
		card = card.Border(2, ring)
	} else {
		card = card.Border(2, ui.Color{})
	}
	card.Children(func() {
		// ⚠️ 按钮默认是横排（图标 + 字），这里要缩略图在上、名字在下，所以里面再套一列。
		ui.Column(c).FillWidth().Gap(space.Px4).AlignItems(ui.Center).Children(func() {
			presetThumb(c, pal, t.Border)
			ui.Text(c, label).FontSize(scale.Font(c, scale.Size12)).SingleLine().
				TextColor(map[bool]ui.Color{true: t.Text, false: t.TextMuted}[selected])
		})
	})
	return card
}
