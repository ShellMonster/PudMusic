// 设置页的「外观」Tab：原型 `SETTINGS.appearance`。原来单独的 `/appearance` 路由并进这里。
//
// 用户拍板：原型的「主题方案」卡片不做（只保留一套主题「深夜唱片行」）。其余照原型：
//
//	深浅模式      三张主题卡（深色 / 浅色 / 跟随系统），选中描 2px 主色
//	颜色与字体    强调色色块（开了封面取色就隐藏）/ 封面取色 / 界面字号 / 收起侧栏 / 字体列表
//	数据显示样式  卡片 / 列表 / 紧凑 三张预览卡
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/leading"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// accentOptions 是强调色那四档（朱砂 / 靛青 / 松绿 / 琥珀），顺序就是画面上从左到右。
var accentOptions = []struct {
	accent app.Accent
	label  string
}{
	{app.AccentCinnabar, "朱砂"},
	{app.AccentIndigo, "靛青"},
	{app.AccentPine, "松绿"},
	{app.AccentAmber, "琥珀"},
}

// themeModes 是三档深浅（深色 / 浅色 / 跟随系统），顺序是原型 `.theme-cards` 里的顺序。
var themeModes = []struct {
	mode  app.ThemeMode
	label string
}{
	{app.ThemeModeDark, "深色"},
	{app.ThemeModeLight, "浅色"},
	{app.ThemeModeSystem, "跟随系统"},
}

// displayStyleCards 是外观 Tab 里「数据显示样式」的三张预览卡，顺序和名字是原型的（卡片 / 列表 / 紧凑）。
var displayStyleCards = []struct {
	mode  app.DisplayMode
	label string
}{
	{app.DisplayModeCard, "卡片"},
	{app.DisplayModeList, "列表"},
	{app.DisplayModeCompact, "紧凑"},
}

const (
	swatchSize = 36 // `.swatch`
	swatchDot  = 26 // `.swatch i`
	themePvH   = 96 // `.theme-card .pv`
	dispPvH    = 64 // `.disp-card .pv`
	fontRowH   = 44 // `.font-row`：上下 10 + 一行 16 号字 + 余量
	fontRows   = 6  // 字体列表一屏露几行
)

// settingsAppearance 是外观那一页。
func (s *Shell) settingsAppearance(c *ui.Context, st app.Settings, ro bool) {
	s.setGroup(c, "mode", "深浅模式", func() {
		ui.Row(c).Key("set-theme-cards").Label("深浅模式").FillWidth().Gap(space.Px16).AlignItems(ui.Start).Children(func() {
			for _, m := range themeModes {
				mode := m.mode
				if s.themeCard(c, m.label, mode, mode == st.ThemeMode, ro).Clicked() {
					s.applySetting("主题", func(cur *app.Settings) { cur.ThemeMode = mode })
				}
			}
		})
	})

	s.setGroup(c, "color", "颜色与字体", func() {
		s.card(c, "color", ro, func(cd *setCard) {
			if !st.CoverAccent {
				cd.row("accent", "强调色", "用在正在播放、选中和主要按钮上", func() { s.accentSwatches(c, st, ro) })
			}
			cd.switchRow("coverAccent", "封面取色", "强调色跟着当前歌曲的封面变", st.CoverAccent,
				func(cur *app.Settings, on bool) { cur.CoverAccent = on })
			selectRow(cd, "fontScale", "界面字号", "", st.FontScale, []enumItem[app.FontScale]{
				{app.FontScaleSmall, "紧凑"}, {app.FontScaleStd, "标准"}, {app.FontScaleLarge, "大"},
			}, func(v app.FontScale) { s.applySetting("界面字号", func(cur *app.Settings) { cur.FontScale = v }) })
			cd.switchRow("collapsed", "收起侧栏", "只显示图标，给内容区更多空间", st.SideMenuCollapsed,
				func(cur *app.Settings, on bool) { cur.SideMenuCollapsed = on })
			cd.divider()
			ui.Column(c).Key("set-font-head").FillWidth().Gap(space.Px16).
				Padding(setRowPadY, setRowPadX, space.Px4, setRowPadX).Children(func() {
				rowTitle(c, "字体")
				rowHelp(c, "每一项都用它自己的字体显示；这台电脑上没有的字体会显示成回退后的样子")
			})
			s.fontList(c, st, ro)
		})
	})

	s.setGroup(c, "display", "数据显示样式", func() {
		s.card(c, "display", ro, func(cd *setCard) {
			cd.row("displayHead", "视频列表的显示方式", "搜索结果的「视频」、UP 主投稿都会跟着变", nil)
			cd.divider()
			p := tok.Of(c.Theme())
			ui.Row(c).Key("set-disp-cards").Label("数据显示样式").FillWidth().Gap(space.Px12).AlignItems(ui.Start).
				Padding(space.Px16, setRowPadX).Children(func() {
				for _, m := range displayStyleCards {
					mode := m.mode
					if s.dispCard(c, p, m.label, mode, mode == st.DisplayMode, ro).Clicked() {
						s.applySetting("数据显示样式", func(cur *app.Settings) { cur.DisplayMode = mode })
					}
				}
			})
		})
	})
}

// accentSwatches 是强调色那一排：四枚 36 的圆圈（选中时 2px 正文色描边），里面 26 的色点，缝 12。
func (s *Shell) accentSwatches(c *ui.Context, st app.Settings, ro bool) {
	p := tok.Of(c.Theme())
	ui.Row(c).Key("set-accent").Label("强调色").Gap(space.Px12).AlignItems(ui.Center).Shrink(0).Children(func() {
		for _, o := range accentOptions {
			b := ui.ButtonBase(c).Key("accent:" + string(o.accent)).Label(o.label).
				Width(swatchSize).Height(swatchSize).Radius(swatchSize / 2).
				Justify(ui.Center).AlignItems(ui.Center).Disabled(ro)
			ring := ui.Color{}
			if o.accent == st.Accent {
				ring = p.Text
			}
			b = b.Border(2, ring).Transition(motion.Fade)
			b.Children(func() {
				ui.Box(c).Key("accent-dot:" + string(o.accent)).Width(swatchDot).Height(swatchDot).
					Radius(swatchDot / 2).Background(tok.AccentColor(string(o.accent), c.Theme().Dark))
			})
			if b.Clicked() {
				pickAccent := o.accent
				s.applySetting("强调色", func(cur *app.Settings) { cur.Accent = pickAccent })
			}
		}
	})
}

// cardBorder 是三种预览卡共用的 2px 描边：选中主色，平时 line，悬停时 line-strong。
func cardBorder(b ui.Element, p tok.Palette, selected bool) ui.Element {
	col := p.Line
	switch {
	case selected:
		col = p.Accent
	case b.Hovered():
		col = p.LineStrong
	}
	return b.Border(2, col).Transition(motion.Fade)
}

// themeCard 是深浅模式的一张卡（`.theme-card`）：描边 2、圆角 12、内边距 8；上面 96 高的
// 迷你窗口（左 28% 侧栏，右边内容 + 底下 20 高的播放条），下面 13/500 的名字。
func (s *Shell) themeCard(c *ui.Context, label string, mode app.ThemeMode, selected, ro bool) ui.Element {
	p := tok.Of(c.Theme())
	b := ui.ButtonBase(c).Key("theme-card:" + label).Label(label).Grow(1).Shrink(1).MinWidth(0).
		Padding(space.Px8).Radius(tok.R12).Background(p.Surface).Disabled(ro)
	b = cardBorder(b, p, selected)
	// 三张预览的颜色（原型 `mode(k, l, bg, side, bar, fg)`）。
	var bg, side, bar, fg ui.Color
	switch mode {
	case app.ThemeModeDark:
		bg, side, bar, fg = ui.Hex("#0e0f11"), ui.Hex("#121315"), ui.Hex("#121315"), ui.Hex("#ecebe8")
	case app.ThemeModeLight:
		bg, side, bar, fg = ui.Hex("#f6f6f5"), ui.Hex("#eeefef"), ui.Hex("#eeefef"), ui.Hex("#141518")
	default:
		bg, side, bar, fg = ui.Hex("#2a2b2e"), ui.Hex("#121315"), ui.Hex("#888888"), ui.Hex("#888888")
	}
	b.Children(func() {
		ui.Column(c).FillWidth().Children(func() {
			ui.Row(c).FillWidth().Height(themePvH).Radius(tok.R8).Background(bg).Clip().Children(func() {
				ui.Box(c).Key("tp-side:" + label).Grow(28).FillHeight().Background(side)
				ui.Column(c).Grow(72).FillHeight().Children(func() {
					ui.Column(c).Grow(1).FillWidth().Padding(space.Px12).Children(func() {
						ui.Box(c).Key("tp-a:" + label).Width(0).FillWidth().Height(8).Radius(tok.R4).Background(fg.Alpha(0.8))
						ui.Box(c).Key("tp-b:"+label).FillWidth().Height(6).Radius(tok.R3).Background(fg.Alpha(0.4)).
							Margin(space.Px6, space.None, space.None, space.None)
					})
					ui.Box(c).Key("tp-bar:" + label).FillWidth().Height(20).Background(bar)
				})
			})
			ui.Text(c, label).FontSize(scale.Font(c, scale.Size13)).LineHeight(leading.Body).FontWeight(500).TextColor(p.Text).SingleLine().
				Padding(space.Px10, space.Px4, space.Px4, space.Px4)
		})
	})
	return b
}

// dispCard 是数据显示样式的一张卡（`.disp-card`）：描边 2、圆角 12、内边距 10；上面 64 高的
// 预览（卡片 2×3 格，列表 3 行，紧凑 5 行，格子 surface-2、圆角 4、缝 4），下面 13/500 的名字。
func (s *Shell) dispCard(c *ui.Context, p tok.Palette, label string, mode app.DisplayMode, selected, ro bool) ui.Element {
	b := ui.ButtonBase(c).Key("disp-card:" + label).Label(label).Grow(1).Shrink(1).MinWidth(0).
		Padding(space.Px10).Radius(tok.R12).Disabled(ro)
	b = cardBorder(b, p, selected)
	cell := func(key string) {
		ui.Box(c).Key(key).Grow(1).FillWidth().FillHeight().Radius(tok.R4).Background(p.Surface2)
	}
	b.Children(func() {
		ui.Column(c).FillWidth().Children(func() {
			pv := ui.Column(c).Key("dp:" + label).FillWidth().Height(dispPvH).Gap(space.Px4)
			pv.Children(func() {
				switch mode {
				case app.DisplayModeCard:
					for r := range 2 {
						ui.Row(c).Key("dp-r:" + label + string(rune('0'+r))).Grow(1).FillWidth().Gap(space.Px4).Children(func() {
							for i := range 3 {
								cell("dp-c:" + label + string(rune('0'+r*3+i)))
							}
						})
					}
				default:
					n := 3
					if mode == app.DisplayModeCompact {
						n = 5
					}
					for i := range n {
						ui.Box(c).Key("dp-l:" + label + string(rune('0'+i))).Grow(1).FillWidth().Radius(tok.R4).Background(p.Surface2)
					}
				}
			})
			ui.Text(c, label).FontSize(scale.Font(c, scale.Size13)).LineHeight(leading.Body).FontWeight(500).TextColor(p.Text).SingleLine().
				Padding(space.Px8, space.None, space.None, space.None)
		})
	})
	return b
}

// fontList 是字体列表（`.font-list`）：整个卡片里的一张单选清单，每一项**用它自己的字体**
// 显示「名字 · 晴天 夜航电台 0123」，选中项 surface-2 底 + 右边一枚对勾。
//
// 名单是问系统要来的（`fontOptionList`，两百多条），所以用虚拟列表，一屏露 6 行。
func (s *Shell) fontList(c *ui.Context, st app.Settings, ro bool) {
	p := tok.Of(c.Theme())
	options := fontOptionList()
	// 手改过的设置文件可能带一个不在名单里的族名：加一条「设置里的：xxx」，别把它当系统默认。
	if _, ok := findFont(options, st.FontFamily); !ok {
		options = append(options, fontOption{label: "设置里的：" + st.FontFamily, family: st.FontFamily})
	}
	n := min(len(options), fontRows)
	h := float32(n)*fontRowH + float32(n-1)*2
	ui.Column(c).Key("set-font-list").Label("字体").FillWidth().Padding(space.Px8).Children(func() {
		lst := ui.List(c, &s.settings.fontList, len(options)).FillWidth().Height(h).
			ItemKey(func(i int) any { return options[i].family }).
			Rows(func(r ui.ListRow) {
				opt := options[r.Index]
				on := opt.family == st.FontFamily
				rc := r.Context
				b := ui.ButtonBase(rc).Key("font:"+opt.family).Label(opt.label).FillWidth().Height(fontRowH).
					Padding(space.Px10, space.Px12).Gap(space.Px16).AlignItems(ui.Center).
					Radius(tok.R8).Disabled(ro)
				switch {
				case on:
					b = b.Background(p.Surface2)
				case b.Hovered():
					b = b.Background(p.Hover)
				}
				b = b.Transition(motion.Fade)
				b.Children(func() {
					ui.Text(rc, opt.label+" · 晴天 夜航电台 0123").Font(opt.family).
						FontSize(scale.Font(rc, scale.Size16)).TextColor(p.Text).SingleLine().Ellipsis("…").Grow(1)
					if on {
						ui.Icon(rc, pud.Icon("check")).FontSize(14).TextColor(p.Text)
					}
				})
				if b.Clicked() && !on {
					fam := opt.family
					s.applySetting("字体", func(cur *app.Settings) { cur.FontFamily = fam })
				}
			})
		s.trackScrollbar(lst)
	})
}
