// 全屏播放器的偏好面板。
//
// Biu 的 `components/full-screen-player/settings-panel.tsx`（235 行）里有四个开关
// 和三个颜色。这一票只搬**频谱**那两样过来：封面开关、歌词开关、模糊背景开关
// 要么是别的票的活（歌词是 BMG-65），要么是视觉取舍还没定（毛玻璃是 PLAN §4 R24
// 那一笔账）。摆上摆不动的东西比没有更坏——用户会去点它。
//
// 文案一字不改照抄上游：
//
//	显示频谱图     settings-panel.tsx:154
//	频谱图颜色     settings-panel.tsx:165
//
// 频谱默认**关**（`store/full-screen-player-settings.ts` 的
// `showSpectrum: false`），所以用户第一次开全屏是看不到频谱的，得自己来这儿开。
// 见 `TestTheSpectrumIsOffUntilTheUserTurnsItOn`。

package ui

import (
	"log"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// MountFullPlayerPrefs 把全屏播放器那份偏好挂到盘上。
//
// ⚠️ 读盘是阻塞 IO，所以要**在开窗口之前**调（`main.go` 里排在 `MountSettings`
// 后面）。拿不到目录不算错误：记一条日志，接着用零值——而零值恰好就是「频谱
// 关着」，也就是 Biu 的默认值。
//
// 为什么不用 `ui.MountSettings` 那一套错误上报：那边记的是「用户设过的东西丢了」，
// 而这一份丢了最坏的结果是频谱回到默认（关着），用户重开一次就好。
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

	ui.Column(c).Width(280).Padding(14).Gap(12).Children(func() {
		show := prefs.ShowSpectrum
		if ui.Switch(c, &show).Label("显示频谱图").Changed() {
			s.app.SetFullPlayerPrefs(app.FullPlayerPrefs{
				ShowSpectrum:  show,
				SpectrumColor: prefs.SpectrumColor,
			})
		}

		// 频谱关着的时候不摆颜色：Biu 那边是 `{values?.showSpectrum && …}`，
		// 关着的时候调了颜色界面上什么也不会变，留着那一行只会让人白调一次。
		if !prefs.ShowSpectrum {
			return
		}
		s.viewSpectrumColor(c, prefs.SpectrumColor)
	})
}

// viewSpectrumColor 是那个颜色。照抄 settings_tab_general.go 的 colorItem
// （同一个 `ui.ColorWell` 那一套），所以两处取色器的观感是一致的。
func (s *Shell) viewSpectrumColor(c *ui.Context, hex string) {
	shown := spectrumDefaultColor
	if hex != "" {
		shown = hex
	}
	s.colorItem(c, "频谱图颜色", shown, false, hex != "", func(col string) {
		s.app.SetFullPlayerPrefs(app.FullPlayerPrefs{ShowSpectrum: true, SpectrumColor: col})
	}, func() {
		s.app.SetFullPlayerPrefs(app.FullPlayerPrefs{ShowSpectrum: true, SpectrumColor: ""})
	})
}
