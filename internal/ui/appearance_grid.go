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
// 颜色取自方案/主题在**当前深浅**下的色板（`presetPaletteFor` / `BuildTheme`），
// 不是图片：色板一改缩略图自己跟着改，不用重新出图。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// skinCardColumns 是主题方案那一组一行摆几张。九套方案折成 3 行
	//（3 + 3 + 2），每张够大才看得出「这是一张界面」而不是一块色。
	skinCardColumns = 3
	// skinCardThumbH 是货架卡里缩略图的高。约 3:2，比控件列那版（46）
	// 大两倍——那版要塞进 79 宽的格子，这一版一张卡 ~220 宽。
	skinCardThumbH = 132
	// modeCardColumns 是主题模式那一组一行摆几张：跟随系统 / 浅色 /
	// 深色 / 定时 四张一行。
	modeCardColumns = 4
	// modeCardThumbH 是模式卡的缩略图高，比方案卡矮一点——它只有一个
	// 「深浅」的维度要表达。
	modeCardThumbH = 88
)

// pickThemePresetShelf 是主题方案那一组货架卡：一行三张铺满内容宽。
//
// 写法和设置页 `pickThemePreset` 同一条心路：点一张 → `ThemePreset` 写进
// 设置，应用层把那一套色板盖到主题上。选中卡带主色描边（`presetCard`
// 那一套），hover 带文字色描边。
func (s *Shell) pickThemePresetShelf(c *ui.Context, st app.Settings, ro bool) {
	w := s.settings.contentW
	if w <= 0 {
		w = settingsContentMaxW
	}
	cardW := (w - float32(skinCardColumns-1)*space.Px12) / skinCardColumns
	col := ui.Column(c).Key("appearance:themePreset").Label("主题方案").Gap(space.Px12).FillWidth()
	col.Children(func() {
		for start := 0; start < len(themePresets); start += skinCardColumns {
			end := min(start+skinCardColumns, len(themePresets))
			ui.Row(c).FillWidth().Gap(space.Px12).Children(func() {
				for _, p := range themePresets[start:end] {
					if s.skinCard(c, p.preset, p.label, cardW, p.preset == st.ThemePreset, ro).Clicked() {
						pick := p.preset
						s.applySetting("主题方案", func(cur *app.Settings) { cur.ThemePreset = pick })
					}
				}
			})
		}
	})
}

// skinCard 是货架上那张大卡：缩略图在上（高 skinCardThumbH），名字在下。
func (s *Shell) skinCard(c *ui.Context, name app.ThemePreset, label string, w float32, selected, ro bool) ui.Element {
	t := c.Theme()
	pal := presetPaletteFor(name, t.Dark)

	card := ui.ButtonBase(c).Key("preset:" + string(name)).Width(w).
		Padding(space.Px4).Radius(t.Radius).Disabled(ro)
	if card.Hovered() && !selected {
		// ⚠️ **hover 也填底**：货架卡的描边在外圈，卡里字那一块没有填充变化
		// 的话「指针落上来」在字的区域一个像素都不变——和 flatButton 的 hover
		// 手感（SurfaceHover 淡底）对齐。
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
		ui.Column(c).FillWidth().Gap(space.Px8).AlignItems(ui.Center).Children(func() {
			presetThumb(c, pal, t.Border, skinCardThumbH)
			ui.Text(c, label).FontSize(scale.Font(c, scale.Size14)).SingleLine().
				TextColor(s.cardLabelColor(t, selected, ro))
		})
	})
	return card
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

// pickThemeModeShelf 是主题模式那一组：四张 mini 卡，每张画的是「这档主题
// 下窗口长什么样」——浅色那张画浅色主题、深色那张画深色主题、跟随系统
// 和定时画左右半半（左浅右深）。
func (s *Shell) pickThemeModeShelf(c *ui.Context, st app.Settings, ro bool) {
	w := s.settings.contentW
	if w <= 0 {
		w = settingsContentMaxW
	}
	cardW := (w - float32(modeCardColumns-1)*space.Px12) / modeCardColumns
	row := ui.Row(c).Key("appearance:themeMode").Label("主题模式").Gap(space.Px12).FillWidth()
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
// 跟随系统和定时是左浅右深两半（它们本来就是「看时间决定」的档）。
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
		// 跟随系统 / 定时：左半画浅色、右半画深色，一眼读出「两边都会换」。
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
// 和 `presetThumb` 同一形状，只是色板来自主题而不是方案。
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
