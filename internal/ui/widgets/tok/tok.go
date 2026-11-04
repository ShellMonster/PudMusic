// Package tok 是「深夜唱片行」的颜色令牌（DESIGN.md §0′）。
//
// # 只有一套主题
//
// 2026-11 起应用只有一套主题「深夜唱片行」，分深色、浅色两份；强调色四选一
// （朱砂 / 靛青 / 松绿 / 琥珀），外加「封面取色」。数值逐项抄自原型
// `index.html` 第 69–120 行（`[data-theme="dark"]` / `[data-theme="light"]` /
// `[data-accent=…]`），**不要在别处另写一份**。
//
// # 怎么取
//
//	p := tok.Of(c.Theme())
//	ui.Text(c, "4:29").TextColor(p.Text3)
//	row.Background(p.Hover)            // 悬停压色（半透明，叠在任何底上）
//	btn.Border(1, p.LineStrong)        // 幽灵按钮、输入框的描边
//
// `Of` 是**纯函数**：只看主题的深浅（`t.Dark`）和主色（`t.Accent`），不读设置、
// 不分配。主色正好是四种强调色之一时，`AccentText` / `AccentSoft` / `Focus` 用原型
// 那一组值；主色是封面取色推出来的别的颜色时，按同一个规则现推（见 `accentSet`）。
//
// # 和 `ui.Theme` 的关系
//
// `internal/ui/theme.go` 的 `buildTheme` 把这里的值装进 MyGo 的主题：
//
//	Background = Bg          Surface = Surface2       Border = Line
//	Text = Text              TextMuted = Text2        Accent = Accent
//	AccentText = AccentInk   Focus = Focus            Inverse = Text（Toast 反色胶囊）
//
// `ui.Theme` 没有的那几档（`Text3`、`LineStrong`、`Hover`、`Press`、`AccentText`、
// `AccentSoft`、`Side`、`Surface`、`Desk`）只能从这里取。
package tok

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	"github.com/egoist/mygo/ui"
)

// Palette 是一种深浅（加一种强调色）下的全部颜色令牌。字段名对着原型的 CSS 变量。
type Palette struct {
	Dark bool

	// Desk 是 `--desk`：窗口外面那块桌面。界面里只给全屏播放器的暗底参考用
	// （见 `Full`），一般控件用不到。
	Desk ui.Color
	// Bg 是 `--bg`：内容区底色。
	Bg ui.Color
	// Side 是 `--side`：侧栏、播放条、分组栏底色。
	Side ui.Color
	// Surface 是 `--surface`：抽屉、弹层、设置卡、对话框、悬停的 UP 卡。
	Surface ui.Color
	// Surface2 是 `--surface-2`：搜索框、Chip、输入框、封面占位底、侧栏选中项。
	Surface2 ui.Color

	// Hover 是 `--hover`：悬停压色，**半透明**（深色白 5%、浅色黑 4.5%），叠在
	// 任何底上都成立。列表行、导航项、图标键、弹层选项的悬停底。
	Hover ui.Color
	// Press 是 `--press`：按下压色（8%）。Chip 的悬停也用这一档。
	Press ui.Color

	// Line 是 `--line`：分隔线、表头线、卡片描边。
	Line ui.Color
	// LineStrong 是 `--line-strong`：幽灵按钮、输入框、进度轨道、kbd、角标描边。
	LineStrong ui.Color

	// Text / Text2 / Text3 是 `--text` / `--text-2` / `--text-3`：正文、次要、第三级
	// （序号、时长、表头、Eyebrow、时间戳）。
	Text, Text2, Text3 ui.Color

	// Accent 是 `--accent`：主按钮底、选中指示条、进度、eq-bars。**只当底色用**
	// （浅色朱砂压在 bg 上只有 4.47:1）。
	Accent ui.Color
	// AccentInk 是 `--accent-ink`：主色底上的字和图标。
	AccentInk ui.Color
	// AccentText 是 `--accent-text`：主色当字色时用的那一档（正在播放的歌名、
	// 榜单前三名、选中项图标、链接）。
	AccentText ui.Color
	// AccentSoft 是 `--accent-soft`：主色淡底（命令面板选中行、拖入区、批量选中行）。
	// 原型是半透明（深 14% / 浅 10%）；这里是**合到 Bg 上的不透明色**，好让
	// `ink` 拿它算对比度。叠在 Surface 上时差一两个通道，看不出来。
	AccentSoft ui.Color
	// Focus 是 `--focus`：键盘焦点环（2 像素实线，外扩 2）。
	Focus ui.Color

	// Ok / Warn / Err 是语义色（`--ok` / `--warn` / `--err`）。取的是
	// design-tokens.html 表 2-1 的落地值（浅色小字也过 4.5:1），不是原型 CSS 那三个。
	Ok, Warn, Err ui.Color
}

// accentSet 是一种强调色在一种深浅下的五个值。
type accentSet struct {
	accent, ink, text, focus ui.Color
	// softA 是淡底的不透明度：深 0.14、浅 0.10。
	softA float32
}

// 原型 `index.html` 115–120 行。
var (
	darkAccents = map[string]accentSet{
		"cinnabar": {ui.Hex("#FF5D3D"), ui.Hex("#1A0905"), ui.Hex("#FF7A5E"), ui.Hex("#FFB3A3"), 0.14},
		"indigo":   {ui.Hex("#7F9CFF"), ui.Hex("#0A0F24"), ui.Hex("#9DB3FF"), ui.Hex("#C3D0FF"), 0.14},
		"pine":     {ui.Hex("#4CC58E"), ui.Hex("#04170E"), ui.Hex("#6CD6A5"), ui.Hex("#B4ECD2"), 0.14},
		"amber":    {ui.Hex("#F0B43C"), ui.Hex("#1D1302"), ui.Hex("#F5C463"), ui.Hex("#F8DCA0"), 0.14},
	}
	lightAccents = map[string]accentSet{
		"cinnabar": {ui.Hex("#D2391B"), ui.Hex("#FFFFFF"), ui.Hex("#B8300F"), ui.Hex("#D2391B"), 0.10},
		"indigo":   {ui.Hex("#3451C9"), ui.Hex("#FFFFFF"), ui.Hex("#2D47B3"), ui.Hex("#3451C9"), 0.10},
		"pine":     {ui.Hex("#167A4D"), ui.Hex("#FFFFFF"), ui.Hex("#126B43"), ui.Hex("#167A4D"), 0.10},
		"amber":    {ui.Hex("#8F5D00"), ui.Hex("#FFFFFF"), ui.Hex("#7F5300"), ui.Hex("#8F5D00"), 0.10},
	}
)

// AccentNames 是四种强调色的名字（和 `app.Accent` 的值一样），按设置页里的顺序。
var AccentNames = []string{"cinnabar", "indigo", "pine", "amber"}

// AccentColor 是一种强调色在这一档深浅下的主色底（`--accent`）。不认识的名字给朱砂。
func AccentColor(name string, dark bool) ui.Color {
	return accentOf(name, dark).accent
}

func accentOf(name string, dark bool) accentSet {
	tab := lightAccents
	if dark {
		tab = darkAccents
	}
	if a, ok := tab[name]; ok {
		return a
	}
	return tab["cinnabar"]
}

// base 是不随强调色变的那一部分（原型 69–112 行）。
var (
	darkBase = Palette{
		Dark:       true,
		Desk:       ui.Hex("#050506"),
		Bg:         ui.Hex("#0E0F11"),
		Side:       ui.Hex("#121315"),
		Surface:    ui.Hex("#17181B"),
		Surface2:   ui.Hex("#1E2024"),
		Hover:      ui.RGBA(255, 255, 255, 0.05),
		Press:      ui.RGBA(255, 255, 255, 0.08),
		Line:       ui.Hex("#26282D"),
		LineStrong: ui.Hex("#34373D"),
		Text:       ui.Hex("#ECEBE8"),
		Text2:      ui.Hex("#A6A8AE"),
		Text3:      ui.Hex("#7F828A"),
		Ok:         ui.Hex("#5BC287"),
		Warn:       ui.Hex("#F2B04A"),
		Err:        ui.Hex("#F0716B"),
	}
	lightBase = Palette{
		Desk:       ui.Hex("#D9DBDE"),
		Bg:         ui.Hex("#F6F6F5"),
		Side:       ui.Hex("#EEEFEF"),
		Surface:    ui.Hex("#FFFFFF"),
		Surface2:   ui.Hex("#E9EAEB"),
		Hover:      ui.RGBA(0, 0, 0, 0.045),
		Press:      ui.RGBA(0, 0, 0, 0.08),
		Line:       ui.Hex("#E0E1E3"),
		LineStrong: ui.Hex("#CFD1D4"),
		Text:       ui.Hex("#141518"),
		Text2:      ui.Hex("#50535A"),
		Text3:      ui.Hex("#6A6D74"),
		Ok:         ui.Hex("#2E7D4F"),
		Warn:       ui.Hex("#9A5B06"),
		Err:        ui.Hex("#C93B3B"),
	}
)

// Base 是一种深浅、一种强调色下的整套令牌（不看主题）。`buildTheme` 用它装主题；
// 页面和控件请用 `Of`。
func Base(dark bool, accent string) Palette {
	p := lightBase
	if dark {
		p = darkBase
	}
	a := accentOf(accent, dark)
	p.Accent, p.AccentInk, p.AccentText, p.Focus = a.accent, a.ink, a.text, a.focus
	p.AccentSoft = p.Bg.Mix(a.accent, a.softA)
	return p
}

// Of 是这一份主题对应的整套令牌。
//
// 主色是四种强调色之一 → 原型那一组值；不是（封面取色）→ 字色那一档从当前底色
// 现推到 ≥4.5:1（`ink.AccentOn`），底上字取主题的 `AccentText`，焦点环取主色本身。
func Of(t *ui.Theme) Palette {
	p := lightBase
	tab := lightAccents
	softA := float32(0.10)
	if t.Dark {
		p, tab, softA = darkBase, darkAccents, 0.14
	}
	p.Accent = t.Accent
	matched := false
	for _, a := range tab {
		if a.accent == t.Accent {
			p.AccentInk, p.AccentText, p.Focus = a.ink, a.text, a.focus
			matched = true
			break
		}
	}
	if !matched {
		p.AccentInk = t.AccentText
		p.AccentText = ink.AccentOn(t)
		p.Focus = t.Accent
	}
	p.AccentSoft = p.Bg.Mix(t.Accent, softA)
	// 高对比度时主题会把 TextMuted 往正文推（`buildTheme`）；第三级跟着推一档，
	// 免得它反而比次要字更淡得多。
	if t.TextMuted != p.Text2 && t.TextMuted.A != 0 {
		p.Text2 = t.TextMuted
		p.Text3 = p.Text3.Mix(t.Text, 0.3)
	}
	return p
}

// OfFull 是全屏播放器**里面**的整套令牌：不随深浅变（原型 `.full` 把 `--text` /
// `--surface` / `--line` / `--hover` 等变量整套改成暗色），强调色换成**深色那一档**
// （浅色主题的 `#D2391B` 压在暗底上读不出来）。封面取色出来的非四色主色保持原样。
//
// 全屏里的 `pud` 组件靠它取色（见 `pud.SetFull`）。
func OfFull(t *ui.Theme) Palette {
	p := Of(t)
	for name, a := range lightAccents {
		if a.accent == t.Accent {
			d := darkAccents[name]
			p.Accent, p.AccentInk, p.AccentText, p.Focus = d.accent, d.ink, d.text, d.focus
			break
		}
	}
	p.Dark = true
	p.Desk, p.Bg, p.Side = Full.Base, Full.Base, darkBase.Side
	p.Surface, p.Surface2 = Full.Surface, Full.Surface2
	p.Hover, p.Press = Full.Hover, ui.RGBA(255, 255, 255, 0.10)
	p.Line, p.LineStrong = Full.Line, Full.LineStrong
	p.Text, p.Text2, p.Text3 = Full.Text, Full.Text2, Full.Text3
	p.Ok, p.Warn, p.Err = darkBase.Ok, darkBase.Warn, darkBase.Err
	p.AccentSoft = p.Bg.Mix(p.Accent, 0.14)
	return p
}

// Full 是全屏播放器的固定暗色（design-tokens.html 表 2-5）：深浅两套主题下
// 全屏播放器都是暗底，这几档**不随主题变**。
var Full = struct {
	// Base 是混色基底：背景 = Base.Mix(封面色, TintMix)。
	Base ui.Color
	// TintMix 是封面色在背景里的占比（原型 `color-mix(tint 30%, #0b0b0c)`）。
	TintMix float32
	// Text / Text2 / Text3 是主文字、次要（歌手、专辑）、第三级（时间读数）。
	Text, Text2, Text3 ui.Color
	// LyricCurrent 是当前句；LyricIdle 是非当前句（38%），悬停到 LyricHover（70%）。
	LyricCurrent, LyricIdle, LyricHover ui.Color
	// Line / LineStrong 是描边（白 12% / 22%）；Hover 是悬停压色（白 7%）。
	Line, LineStrong, Hover ui.Color
	// Surface / Surface2 是全屏里的弹层底和控件底。
	Surface, Surface2 ui.Color
	// Spectrum 是频谱柱（装饰性，22%）。
	Spectrum ui.Color
	// PlayBg / PlayInk 是 64 的大播放键：浅底深图形。
	PlayBg, PlayInk ui.Color
}{
	Base:         ui.Hex("#0B0B0C"),
	TintMix:      0.30,
	Text:         ui.Hex("#ECEBE8"),
	Text2:        ui.Hex("#B6B5B2"),
	Text3:        ui.Hex("#8F8E8B"),
	LyricCurrent: ui.Hex("#FFFFFF"),
	LyricIdle:    ui.RGBA(236, 235, 232, 0.38),
	LyricHover:   ui.RGBA(236, 235, 232, 0.70),
	Line:         ui.RGBA(255, 255, 255, 0.12),
	LineStrong:   ui.RGBA(255, 255, 255, 0.22),
	Hover:        ui.RGBA(255, 255, 255, 0.07),
	Surface:      ui.Hex("#1B1B1D"),
	Surface2:     ui.RGBA(255, 255, 255, 0.08),
	Spectrum:     ui.RGBA(236, 235, 232, 0.22),
	PlayBg:       ui.Hex("#ECEBE8"),
	PlayInk:      ui.Hex("#0B0B0C"),
}

// 封面取色混合比例（design-tokens.html 表 2-4）：底色 = Bg.Mix(封面色, 比例)。
const (
	// TintHero 是首页「今日一张」那一块。
	TintHero = 0.22
	// TintCollection 是收藏夹 / 合集头部。
	TintCollection = 0.26
	// TintUpBand 是 UP 主空间头图带。
	TintUpBand = 0.34
	// TintVideoThumb 是视频卡缩略图的留白底：`color-mix(tint 40%, #0b0b0c)`。
	TintVideoThumb = 0.40
)
