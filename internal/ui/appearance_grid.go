// 皮肤页的「货架」：主题方案 + 主题模式都画成大预览卡，不是设置页那种
// 「左边标题右边控件」的行——换肤是看图挑的，卡本身就是答案。
//
// # 为什么是货架不是表单
//
// 参照网易云音乐的「个性换肤」：每张卡是一张完整界面缩略图，点一张整窗
// 立刻换，选中那张右上角一个对勾。用户认的是「这张图长什么样」，不是
// 「这一项的值是多少」——所以这一页的两组卡都铺满内容宽（不局限在控件
// 列 340 里），缩略图也比设置页那版大得多。
//
// # 缩略图是真画的
//
// 颜色取自那一档深浅下的主题（`BuildTheme`），
// 不是图片：色板一改缩略图自己跟着改，不用重新出图。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

const (
	// modeCardColumns 是深浅模式那一组一行摆几张：深色 / 浅色 / 跟随系统
	// 三张一行（原型 `.theme-cards` 是 `repeat(3, 1fr)`、缝 16）。
	modeCardColumns = 3
	// modeCardThumbH 是模式卡的缩略图高（原型 `.theme-card .pv` 96）。
	modeCardThumbH = 96

	// 缩略图的几档圆角和侧栏宽（刻意不跟主题走：缩略图是窗口的缩影）。
	presetThumbRadius = 8
	presetThumbSide   = 14
	presetBlockRadius = 2
	presetDotRadius   = 3

	// swatchSize / swatchDot 是强调色那一圈：外圈 36（选中时 2 像素正文色描边）、
	// 里面一枚 26 的色点（原型 `.swatch` / `.swatch i`），四枚之间 12 的缝。
	swatchSize = 36
	swatchDot  = 26
	swatchGap  = space.Px12
)

// pickAccent 是强调色那一排：四枚色点（朱砂 / 靛青 / 松绿 / 琥珀）。
//
// 封面取色开着时强调色跟着封面走，这一排**不画**（原型 `appearance()` 里
// `!s.coverAccent` 才出这一行）。外观页的版式由 T6 重做；这一排是 T0 换掉旧的
// 「主题方案」网格时留下的最小可用实现。
func (s *Shell) pickAccent(c *ui.Context, st app.Settings, ro bool) {
	t := c.Theme()
	row := ui.Row(c).Key("appearance:accent").Label("强调色").Gap(swatchGap).AlignItems(ui.Center)
	row.Children(func() {
		for _, o := range accentOptions {
			selected := o.accent == st.Accent
			b := ui.ButtonBase(c).Key("accent:" + string(o.accent)).Label(o.label).
				Width(swatchSize).Height(swatchSize).Radius(swatchSize / 2).
				Justify(ui.Center).AlignItems(ui.Center).Disabled(ro)
			ring := ui.Color{}
			if selected {
				ring = t.Text
			}
			b = b.Border(2, ring)
			b.Children(func() {
				ui.Column(c).Width(swatchDot).Height(swatchDot).Radius(swatchDot / 2).
					Background(tok.AccentColor(string(o.accent), t.Dark)).Children(func() {})
			})
			if b.Clicked() {
				pick := o.accent
				s.applySetting("强调色", func(cur *app.Settings) { cur.Accent = pick })
			}
		}
	})
}

// cardLabelColor 是货架卡上那一行名字的字色。
//
// ⚠️ **只读（ro）时换成 `ink.Disabled`**：框架对整个禁用控件乘 50% 不透明度，
// `TextMuted` 乘完只剩 ~2:1——「主题」那四张卡在只读设置页里字就淡得看不见了。
// `ink.Disabled` 是反着算的那一支：先假设会被乘 0.5，选一个合完之后仍 ≥3:1
// 的色（`widgets/ink` 那一段注释）。
func (s *Shell) cardLabelColor(t *ui.Theme, selected, ro bool) ui.Color {
	if ro {
		return ink.Disabled(t)
	}
	if selected {
		return t.Text
	}
	return t.TextMuted
}

// pickThemeModeShelf 是深浅模式那一组：三张 mini 卡，每张画的是「这档主题
// 下窗口长什么样」——浅色那张画浅色主题、深色那张画深色主题、跟随系统
// 画左右半半（左浅右深）。
func (s *Shell) pickThemeModeShelf(c *ui.Context, st app.Settings, ro bool) {
	w := s.settings.contentW
	if w <= 0 {
		w = settingsContentMaxW
	}
	cardW := (w - float32(modeCardColumns-1)*space.Px16) / modeCardColumns
	row := ui.Row(c).Key("appearance:themeMode").Label("深浅模式").Gap(space.Px16).FillWidth()
	row.Children(func() {
		for _, m := range themeModes {
			mode := m.mode
			if s.modeCard(c, m, cardW, mode == st.ThemeMode, ro).Clicked() {
				s.applySetting("主题", func(cur *app.Settings) { cur.ThemeMode = mode })
			}
		}
	})
}

// modeCard 画主题模式那一张小卡。缩略图是「这档深浅下的迷你窗口」：
// 跟随系统是左浅右深两半。
func (s *Shell) modeCard(c *ui.Context, m struct {
	mode  app.ThemeMode
	label string
	icon  string
}, w float32, selected, ro bool) ui.Element {
	t := c.Theme()

	card := ui.ButtonBase(c).Key("mode:" + m.label).Width(w).
		Padding(space.Px4).Radius(t.Radius).Disabled(ro)
	if card.Hovered() && !selected {
		card = card.Background(t.SurfaceHover)
	}
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
		ui.Column(c).FillWidth().Gap(space.Px6).AlignItems(ui.Center).Children(func() {
			s.modeThumb(c, m.mode, t.Border)
			ui.Text(c, m.label).FontSize(scale.Font(c, scale.Size12)).SingleLine().
				TextColor(s.cardLabelColor(t, selected, ro))
		})
	})
	return card
}

// modeThumb 画模式卡里那张小缩略图。
func (s *Shell) modeThumb(c *ui.Context, mode app.ThemeMode, border ui.Color) {
	st := app.DefaultSettings("")
	switch mode {
	case app.ThemeModeLight:
		s.modeThumbHalf(c, *BuildTheme(st, false), border)
	case app.ThemeModeDark:
		s.modeThumbHalf(c, *BuildTheme(st, true), border)
	default:
		// 跟随系统：左半画浅色、右半画深色，一眼读出「两边都会换」。
		ui.Row(c).FillWidth().Height(modeCardThumbH).Radius(presetThumbRadius).Border(1, border).
			Clip().Children(func() {
			ui.Column(c).Grow(0.5).FillHeight().Children(func() {
				s.modeThumbFill(c, *BuildTheme(st, false))
			})
			ui.Column(c).Grow(0.5).FillHeight().Children(func() {
				s.modeThumbFill(c, *BuildTheme(st, true))
			})
		})
	}
}

// modeThumbHalf 画单色那一档（浅色整浅、深色整深）。
func (s *Shell) modeThumbHalf(c *ui.Context, th ui.Theme, border ui.Color) {
	ui.Row(c).FillWidth().Height(modeCardThumbH).Radius(presetThumbRadius).Border(1, border).
		Clip().Children(func() {
		ui.Column(c).Grow(1).FillHeight().Children(func() {
			s.modeThumbFill(c, th)
		})
	})
}

// modeThumbFill 是缩略图里的内容：一条侧栏 + 两行封面块 + 一条播放条，
// 色板来自那一档深浅的主题。
func (s *Shell) modeThumbFill(c *ui.Context, th ui.Theme) {
	ui.Row(c).Grow(1).FillHeight().Background(th.Background).Children(func() {
		ui.Column(c).Width(presetThumbSide * 2).FillHeight().Background(th.Surface).Children(func() {})
		ui.Column(c).Grow(1).FillHeight().Padding(space.Px6).Gap(space.Px4).Children(func() {
			for range 2 {
				ui.Row(c).FillWidth().Grow(1).Gap(space.Px4).Children(func() {
					for range 2 {
						ui.Column(c).Grow(1).FillHeight().Radius(presetBlockRadius + 1).
							Background(th.SurfaceHover).Children(func() {})
					}
				})
			}
			ui.Row(c).FillWidth().Height(space.Px8).Justify(ui.Center).AlignItems(ui.Center).
				Children(func() {
					ui.Column(c).Width(space.Px8).Height(space.Px8).Radius(presetDotRadius + 1).
						Background(th.Accent).Children(func() {})
				})
		})
	})
}
