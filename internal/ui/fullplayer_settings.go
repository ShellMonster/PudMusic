// 全屏播放器的偏好弹层（原型 `popHtml('fprefs')`）。
//
// 一排开关：显示歌词 / 显示频谱图 / 显示封面 / 显示虚化背景 / 显示弹幕；歌词、频谱、
// （关掉虚化背景之后的）背景各跟一排三色的小色块。弹层最小宽 300，最后一行是「视频画质」说明。
//
// 色块的值（原型 `swatch('fp-ly', …)`）：
//
//	歌词    纸白 #ECEBE8（默认，存空串）/ 朱砂浅 #FFB3A3 / 月蓝 #B9C8FF
//	频谱    淡白 22%（默认，存空串）/ 朱砂 55% / 月蓝 45%
//	背景    封面取色（默认，存空串）/ 深蓝 #101826 / 深红 #1A1010
//
// 旧版（Biu 版）的取色器、「用默认」键、均衡器那一行都随旧弹层一起删了；均衡器在底栏上自己有一个弹层。
package ui

import (
	"log"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// fullSwatch 是偏好弹层里的一枚色块（原型 `.mini-sw button`）：24 的圆，2 宽的描边，选中的描边是正文色。
type fullSwatch struct {
	// hex 是存进偏好的值（空串 = 默认）；show 是画出来的颜色；name 是无障碍名字。
	hex, name string
	show      ui.Color
}

var (
	fullLyricSwatches = []fullSwatch{
		{"", "纸白", ui.Hex("#ECEBE8")},
		{"#ffb3a3", "朱砂浅", ui.Hex("#FFB3A3")},
		{"#b9c8ff", "月蓝", ui.Hex("#B9C8FF")},
	}
	fullSpecSwatches = []fullSwatch{
		{"", "淡白", ui.RGBA(236, 235, 232, 0.22)},
		{"#ff5d3d8c", "朱砂", ui.RGBA(255, 93, 61, 0.55)},
		{"#b9c8ff73", "月蓝", ui.RGBA(185, 200, 255, 0.45)},
	}
	fullBgSwatches = []fullSwatch{
		{"", "封面取色", ui.Color{}},
		{"#101826", "深蓝", ui.Hex("#101826")},
		{"#1a1010", "深红", ui.Hex("#1A1010")},
	}
)

// MountFullPlayerPrefs 把全屏播放器那份偏好挂到盘上。
//
// ⚠️ 读盘是阻塞 IO，所以要**在开窗口之前**调（`main.go` 里排在 `MountSettings` 后面）。拿不到
// 目录不算错误：记一条日志，接着用默认。
func MountFullPlayerPrefs(a *app.App) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("pudmusic: 拿不到应用数据目录，全屏播放器的偏好这次关掉就没了：%v", err)
		dir = ""
	}
	a.MountFullPlayerPrefs(app.FullPlayerPrefsPath(dir))
}

// setFullPrefs 读当前那份偏好、改一样、存回去。
//
// ⚠️ **不要在调用点上用帧开头那个快照拼一份新的**：这一帧里前面那个开关可能刚改过一样东西，
// 用快照拼会把那一次改回去（`SetFullPlayerPrefs` 写的是整份）。
func (s *Shell) setFullPrefs(mut func(*app.FullPlayerPrefs)) {
	p := s.app.FullPlayerPrefs()
	mut(&p)
	s.app.SetFullPlayerPrefs(p)
}

// viewFullPrefs 是底栏右边那枚「偏好」键 + 它开出来的弹层。
func (s *Shell) viewFullPrefs(c *ui.Context) {
	b := pud.IconBtn(c, "full:prefs", "settings", "满屏播放器偏好", pud.IconOpts{Color: fullCtrlColor})
	s.fullPop(c, popPrefs, b, "full-pop-prefs", true, true, func() { s.viewFullPrefsContent(c) })
}

// viewFullPrefsContent 是弹层的内容。
func (s *Shell) viewFullPrefsContent(c *ui.Context) {
	prefs := s.app.FullPlayerPrefs()
	pud.PopHeading(c, "满屏播放器")

	row := func(key, title string, on bool, save func(*app.FullPlayerPrefs, bool)) {
		v := on
		ui.Row(c).Key(key+":row").FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Gap(space.Px16).
			Padding(space.Px8, space.Px10).Children(func() {
			ui.Text(c, title).FontSize(scale.Font(c, scale.Size13)).TextColor(tok.Full.Text).SingleLine()
			// 原型 `.pop-row .switch { transform: scale(.85); transform-origin: right center }`：
			// 占位还是 44×26，画出来缩到 85%，贴右边。
			var toggled bool
			ui.Box(c).Size(tok.SwitchW, tok.SwitchH).Shrink(0).
				AnimatePose(ui.Pose{X: tok.SwitchW * 0.15 / 2, Scale: 0.85}, ui.Animation{}).Children(func() {
				toggled = pud.Switch(c, key, &v, "开关："+title)
			})
			if toggled {
				s.setFullPrefs(func(p *app.FullPlayerPrefs) { save(p, v) })
			}
		})
	}
	swatches := func(key, title string, list []fullSwatch, cur string, tint ui.Color, save func(*app.FullPlayerPrefs, string)) {
		ui.Row(c).Key(key+":swatches").Gap(space.Px8).Padding(space.None, space.Px10, space.Px8, space.Px10).
			Role(ui.RoleRadioGroup).Label(title).Children(func() {
			for _, sw := range list {
				sw := sw
				if s.viewFullSwatch(c, key+":"+sw.name, sw, strings.EqualFold(cur, sw.hex), tint).Clicked() {
					s.setFullPrefs(func(p *app.FullPlayerPrefs) { save(p, sw.hex) })
				}
			}
		})
	}

	row("full:pref:lyrics", "显示歌词", prefs.ShowLyrics, func(p *app.FullPlayerPrefs, v bool) { p.ShowLyrics = v })
	if prefs.ShowLyrics {
		swatches("full:pref:lyc", "歌词字体颜色", fullLyricSwatches, prefs.LyricsColor, ui.Color{},
			func(p *app.FullPlayerPrefs, v string) { p.LyricsColor = v })
	}
	row("full:pref:spectrum", "显示频谱图", prefs.ShowSpectrum, func(p *app.FullPlayerPrefs, v bool) { p.ShowSpectrum = v })
	if prefs.ShowSpectrum {
		swatches("full:pref:spc", "频谱图颜色", fullSpecSwatches, prefs.SpectrumColor, ui.Color{},
			func(p *app.FullPlayerPrefs, v string) { p.SpectrumColor = v })
	}
	row("full:pref:cover", "显示封面", prefs.ShowCover, func(p *app.FullPlayerPrefs, v bool) { p.ShowCover = v })
	row("full:pref:blur", "显示虚化背景", prefs.ShowBlurredBackground, func(p *app.FullPlayerPrefs, v bool) { p.ShowBlurredBackground = v })
	if !prefs.ShowBlurredBackground {
		swatches("full:pref:bg", "背景颜色", fullBgSwatches, prefs.BackgroundColor, s.fullBgBase(s.app.Snapshot().CoverURL),
			func(p *app.FullPlayerPrefs, v string) { p.BackgroundColor = v })
	}
	row("full:pref:danmaku", "显示弹幕", prefs.ShowDanmaku, func(p *app.FullPlayerPrefs, v bool) { p.ShowDanmaku = v })

	if s.app.VideoAvailable() {
		ui.Box(c).FillWidth().Height(1).Background(tok.Full.Line).Margin(space.Px6, space.Px4)
		_, cur := s.app.VideoQualities()
		pud.PopText(c, "视频画质："+cur.Label()+"（在播放视频时点右上角的画质键改）")
	}
}

// viewFullSwatch 画一枚色块。tint 只给「封面取色」那一枚用（它没有固定颜色）。
func (s *Shell) viewFullSwatch(c *ui.Context, key string, sw fullSwatch, checked bool, tint ui.Color) ui.Element {
	fill := sw.show
	if fill.A == 0 {
		fill = tint
		if fill.A == 0 {
			fill = tok.Full.Surface2
		}
	}
	// 半透明的色块压在弹层底上才是看得到的那个颜色。
	fill = tok.Full.Surface.Mix(fill, float32(fill.A)/255).Alpha(1)
	border := ui.Transparent
	if checked {
		border = tok.Full.Text
	}
	b := ui.ButtonBase(c).Key(key).Label(sw.name).Size(24, 24).Radius(12).Shrink(0).
		Background(fill).Border(2, border).Transition(motion.Fade)
	return b
}
