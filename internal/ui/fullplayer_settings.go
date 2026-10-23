// 全屏播放器的偏好面板。
//
// 照 Biu 的 `components/full-screen-player/settings-panel.tsx`（235 行）：四个开关
// （显示歌词 / 显示频谱图 / 显示封面 / 显示虚化背景）和三块颜色（歌词字体颜色 /
// 频谱图颜色 / 背景颜色）。颜色跟着它那个开关走——上游是
// `{values?.showLyrics && …}`、`{values?.showSpectrum && …}`、
// `{!values?.showBlurredBackground && …}`：关着的时候调了界面上什么也不会变，
// 留着那一行只会让人白调一次。
//
// 文案一字不改照抄上游：
//
//	显示歌词        settings-panel.tsx:119
//	歌词字体颜色    settings-panel.tsx:128
//	显示频谱图      settings-panel.tsx:154
//	频谱图颜色      settings-panel.tsx:165
//	显示封面        settings-panel.tsx:189
//	显示虚化背景    settings-panel.tsx:200
//	背景颜色        settings-panel.tsx:209
//
// 四个开关的默认值照 Biu 的 store：歌词 / 封面 / 虚化背景开着，频谱关着
// （`store/full-screen-player-settings.ts:21-29`）。看
// `TestTheSpectrumIsOffUntilTheUserTurnsItOn` 和 `TestTheDefaultsMatchBiusStore`。
//
// ⚠️ **满屏这一屏没有歌词视图**（歌词区在迷你播放器那一页，见
// `nowplaying.go`），所以「显示歌词」和「歌词字体颜色」改的是偏好里的值，
// 这一屏的画面上没有歌词跟着变。票 #169 要的是**面板补齐**——上游那七项一项
// 不少，键名和默认值都和它的 store 对齐，将来满屏接歌词时直接用这一份。
//
// ## 面板的尺寸
//
// 上游是 `min-w-[320px] space-y-4 p-4`（`settings-panel.tsx:117`）：最窄 320、
// 行与行之间 16、内边距 16。以前这里是 280 / 14 / 12，四项的时候摆得下，七项
// 就挤了。
//
// ## 颜色块
//
// 上游那块是 `h-8 w-12 rounded-full border`（48×32 的药丸，`settings-panel.tsx:142`）。
// 取色器仍然是框架的 `ui.ColorWell`（和设置页 `colorItem` 同一个），只是把
// 它的底色和描边让给外面那颗药丸——设置页那口是 100×40 的方色块，这里照上游。

package ui

import (
	"log"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// MountFullPlayerPrefs 把全屏播放器那份偏好挂到盘上。
//
// ⚠️ 读盘是阻塞 IO，所以要**在开窗口之前**调（`main.go` 里排在 `MountSettings`
// 后面）。拿不到目录不算错误：记一条日志，接着用默认——而默认恰好就是 Biu 的
// `defaultSettings`（见 `app.DefaultFullPlayerPrefs`）。
//
// 为什么不用 `ui.MountSettings` 那一套错误上报：那边记的是「用户设过的东西丢了」，
// 而这一份丢了最坏的结果是七个键回到默认，用户重开一次就好。
func MountFullPlayerPrefs(a *app.App) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("biu: 拿不到应用数据目录，全屏播放器的偏好这次关掉就没了：%v", err)
		dir = ""
	}
	a.MountFullPlayerPrefs(app.FullPlayerPrefsPath(dir))
}

// viewFullSettings 是偏好面板。
//
// 它在 `ui.Popover` 里面（挂在顶上那个齿轮上，Biu 那边是 PopoverTrigger +
// PopoverContent）。`Popover` 自己带「点外面关掉」和「Esc 关掉」。
func (s *Shell) viewFullSettings(c *ui.Context) {
	prefs := s.app.FullPlayerPrefs()

	// ⚠️ 这几个局部值是**这一帧**开关上的值：用户刚按下的那一下写的是它们，
	// 而 `prefs` 是这一帧开头拍的那一份快照。下面「跟不跟着开关走」那几行判的
	// 是局部值——判快照的话那一行要等下一帧才跟上。
	showLyrics, showSpectrum := prefs.ShowLyrics, prefs.ShowSpectrum
	showCover, blurred := prefs.ShowCover, prefs.ShowBlurredBackground

	ui.Column(c).Width(320).Padding(space.Px16).Gap(space.Px16).Children(func() {
		s.fullSwitchRow(c, "显示歌词", &showLyrics, func(on bool) {
			s.setFullPrefs(func(p *app.FullPlayerPrefs) { p.ShowLyrics = on })
		})
		if showLyrics {
			s.fullColorRow(c, "歌词字体颜色", prefs.LyricsColor, "#ffffff",
				prefs.LyricsColor != "",
				func(col string) {
					s.setFullPrefs(func(p *app.FullPlayerPrefs) { p.LyricsColor = col })
				},
				func() {
					s.setFullPrefs(func(p *app.FullPlayerPrefs) { p.LyricsColor = "" })
				})
		}

		s.fullSwitchRow(c, "显示频谱图", &showSpectrum, func(on bool) {
			s.setFullPrefs(func(p *app.FullPlayerPrefs) { p.ShowSpectrum = on })
		})
		if showSpectrum {
			s.fullColorRow(c, "频谱图颜色", prefs.SpectrumColor, spectrumDefaultColor,
				prefs.SpectrumColor != "",
				func(col string) {
					s.setFullPrefs(func(p *app.FullPlayerPrefs) { p.SpectrumColor = col })
				},
				func() {
					s.setFullPrefs(func(p *app.FullPlayerPrefs) { p.SpectrumColor = "" })
				})
		}

		s.fullSwitchRow(c, "显示封面", &showCover, func(on bool) {
			s.setFullPrefs(func(p *app.FullPlayerPrefs) { p.ShowCover = on })
		})

		s.fullSwitchRow(c, "显示虚化背景", &blurred, func(on bool) {
			s.setFullPrefs(func(p *app.FullPlayerPrefs) { p.ShowBlurredBackground = on })
		})
		if !blurred {
			s.fullColorRow(c, "背景颜色", prefs.BackgroundColor,
				toHex(c.Theme().Background), prefs.BackgroundColor != "",
				func(col string) {
					s.setFullPrefs(func(p *app.FullPlayerPrefs) { p.BackgroundColor = col })
				},
				func() {
					s.setFullPrefs(func(p *app.FullPlayerPrefs) { p.BackgroundColor = "" })
				})
		}
	})
}

// setFullPrefs 读当前那份偏好、改一样、存回去。
//
// ⚠️ **不要在调用点上用帧开头那个 `prefs` 快照拼一份新的**：这一帧里前面那个
// 开关可能刚改过一样东西，用快照拼会把那一次改回去（`SetFullPlayerPrefs` 写的是
// 整份）。
func (s *Shell) setFullPrefs(mut func(*app.FullPlayerPrefs)) {
	p := s.app.FullPlayerPrefs()
	mut(&p)
	s.app.SetFullPlayerPrefs(p)
}

// fullSwitchRow 是偏好面板里的一行开关：左边标题，右边开关。
//
// ⚠️ 用 `staticSwitch`，不是 `ui.Switch`——理由见 static_switch.go 顶上那一段：
// 框架那一颗的旋钮要等 140ms 才滑过去，而无头 Tester 的钟站着不动，实测点下去
// 之后开关那一块**一个像素都没变**。设置页上每一个开关用的也正是它。
//
// ⚠️ 开关的名字写成「开关：那一行的标题」，不是照抄标题：标题是它**兄弟节点**
// 上的可见文字，两者同名的话无头 Tester 的 `Find` 先找到标题（点它什么也不会
// 发生），读屏也会把同一个名字念两遍。这条约定和设置页的 `pickSwitch` 一致。
func (s *Shell) fullSwitchRow(c *ui.Context, title string, on *bool, save func(bool)) {
	ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(scale.Font(c, 16)).FontWeight(500)
		if staticSwitch(c, on).Label("开关：" + title).Changed() {
			save(*on)
		}
	})
}

// fullColorRow 是一行颜色：左边标题，右边一块 48×32 的药丸色块。
//
// `fallback` 是「还没挑过」时画出来的那个颜色（歌词白、频谱 #666、背景主题色）；
// `overridden` 为真时旁边多一个「用默认」——Biu 没有那个出口，但挑过一次之后
// 就再也回不到默认，而本项目的设置页一直给这个出口，两处保持一致。
func (s *Shell) fullColorRow(c *ui.Context, title, hex, fallback string, overridden bool,
	set func(string), reset func()) {
	t := c.Theme()
	shown := fallback
	if hex != "" {
		shown = hex
	}
	col := colorOfHex(shown)

	ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(scale.Font(c, 16)).FontWeight(500)
		ui.Row(c).Gap(space.Px8).AlignItems(ui.Center).Children(func() {
			if overridden {
				if flatButton(c, "用默认").FontSize(scale.Font(c, 12)).
					TextColor(t.TextMuted).Clicked() {
					reset()
				}
			}
			// 药丸：底色先铺成用户挑的那个颜色，再把 ColorWell 的底色/描边让掉，
			// 让里面那块色片和外面同色、看起来就是一整块 48×32 的颜色。
			ui.Box(c).Width(48).Height(32).Radius(16).Clip().
				Background(col).Border(1, t.Border).Children(func() {
				if ui.ColorWell(c, &col).Fill().
					Background(ui.Transparent).Border(0, ui.Transparent).
					Label("色块：" + title).Changed() {
					set(toHex(col))
				}
			})
		})
	})
}
