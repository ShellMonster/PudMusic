// 设置页的「常规」Tab：原型 `SETTINGS.general`（播放 / 系统 / 下载三组）。
//
// 原型有、底层没有的条目**不做假开关**：无缝播放（解码层本来就裁头尾静音，没有开关）、
// 恢复上次的播放（断点续播常开，没有开关）、记录播放历史、切歌时显示通知、默认下载内容
// （音频 / 视频）——这几项设置里没有对应的键，见 DESIGN.md §0′.T6。
// 原型没有的旧条目（切歌淡入淡出、音量均衡、均衡器、迷你歌词卷帘、搜索历史、FFmpeg 路径、
// 本地音乐目录、背景清晰度）界面入口已删除，设置里的键和 service 层能力保留。
package ui

import (
	"runtime"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/leading"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// audioQualities 是音质那几档，键值和 `app.AudioQuality` 一字不差。
//
// 🔴 没有「自动」这一档（票 #581）：默认是「高品质」，无损 / 杜比是显式选项，拿不到时
// 取流层自动退档（切到这两档时 `qualitySwitchToast` 说一句）。
var audioQualities = []struct {
	quality app.AudioQuality
	label   string
}{
	{app.AudioQualityLossless, "无损"},
	{app.AudioQualityDolby, "杜比全景声"},
	{app.AudioQualityHigh, "高品质"},
	{app.AudioQualityMedium, "中等"},
	{app.AudioQualityLow, "低品质"},
}

// audioQualityHint 是每一档底下那句话（原型 `popHtml` 里的 note，设置页不画，留给气泡 / 提示用）。
var audioQualityHint = map[app.AudioQuality]string{
	app.AudioQualityLossless: "FLAC / Hi-Res（这首没有就自动用 192K）",
	app.AudioQualityDolby:    "杜比全景声（这首没有就自动用 192K）",
	app.AudioQualityHigh:     "192~320 kbps（默认）",
	app.AudioQualityMedium:   "128 kbps",
	app.AudioQualityLow:      "64 kbps",
}

// displayModes 是「数据显示样式」那三档（列表 / 网格 / 紧凑），原生菜单「显示」栏用它。设置页「外观」
// Tab 里的三张预览卡用的是原型的顺序和名字（卡片 / 列表 / 紧凑），见 `displayStyleCards`。
//
// Biu 那一格是三个带图标的 Tab（列表 / 网格 / 紧凑，`system-settings.tsx:65-101`）。
//
// ✅ **这一格已经换成图标了**（票 #297）：三档各配一枚 18px 的图标，语义对得上：
//
//   - 列表 → `Editor/list-check-2.svg`（`iconViewList`，一份清单）
//   - 网格 → `Design/layout-grid-line.svg`（`iconViewGrid`，一格一格）
//   - 紧凑 → `Document/file-list-2-line.svg`（`iconFileList`，**上游那一档用的
//     正是它**：`RiFileListLine`，`system-settings.tsx:94`）
//
// ⚠️ **三档必须各有各的图标**：配错了用户切档时看到的是同一张图，而那一格
// 的选中信号只有一个（药丸），图形是它唯一的补充。
var displayModes = []struct {
	mode  app.DisplayMode
	label string
	icon  string
}{
	{app.DisplayModeList, "列表", iconViewList},
	{app.DisplayModeCard, "网格", iconViewGrid},
	{app.DisplayModeCompact, "紧凑", iconFileList},
}

// updateNotificationTitle 是「更新提醒」那一行的标题。它和 `switchHint` 的键是同一个字
// （见 `switch_hint.go`）：那一格的效果在界面上看不出来（通知弹在通知中心），所以拨开之后
// 要有一句话说明它会做什么，那句话是按标题查表的。
const updateNotificationTitle = "更新提醒"

// settingsGeneral 是常规那一页。
func (s *Shell) settingsGeneral(c *ui.Context, st app.Settings, ro bool) {
	s.setGroup(c, "play", "播放", func() {
		s.card(c, "play", ro, func(cd *setCard) {
			qItems := make([]enumItem[app.AudioQuality], len(audioQualities))
			for i, q := range audioQualities {
				qItems[i] = enumItem[app.AudioQuality]{value: q.quality, label: q.label}
			}
			selectRow(cd, "quality", "默认音质", "切换后会重新取流，并从当前位置继续播放", st.AudioQuality, qItems,
				func(q app.AudioQuality) {
					s.applySetting("音质偏好", func(cur *app.Settings) { cur.AudioQuality = q })
					// 应用层把这一笔同步给正在放的那首；切到无损 / 杜比的那一下说一句降档规矩。
					qualitySwitchToast(s, app.PrefToBili(q))
				})

			vItems := make([]enumItem[int], len(app.VideoQualitySteps))
			for i, q := range app.VideoQualitySteps {
				label := "720P"
				if q != 0 {
					label = bili.VideoQuality(q).Label()
				}
				vItems[i] = enumItem[int]{value: q, label: label}
			}
			selectRow(cd, "videoq", "视频画质", "满屏播放器里看视频时用的清晰度", st.VideoQuality, vItems,
				func(q int) { s.applySetting("视频画质", func(cur *app.Settings) { cur.VideoQuality = q }) })

			cd.switchRow("report", "上报本机播放记录", "把播放进度同步到 B 站账号，换设备可以接着看", st.ReportPlayHistory,
				func(cur *app.Settings, on bool) { cur.ReportPlayHistory = on })

			cd.switchRow("autoplayCollection", "自动连播合集", "合集播完一集接着下一集；关闭时每个合集只播第一首",
				st.AutoplayCollectionsEnabled(),
				func(cur *app.Settings, on bool) {
					v := on
					cur.AutoplayCollections = &v
				})
		})
	})

	s.setGroup(c, "system", "系统", func() {
		s.card(c, "system", ro, func(cd *setCard) {
			cd.row("autostart", "开机自启", "登录后会自动打开", func() { s.autoStartSwitch(c, st, ro) })
			cd.switchRow("tray", "关闭窗口时留在菜单栏", "播放不会中断，可以从菜单栏图标回来",
				st.CloseWindowOption == app.CloseWindowHide,
				func(cur *app.Settings, on bool) {
					cur.CloseWindowOption = app.CloseWindowExit
					if on {
						cur.CloseWindowOption = app.CloseWindowHide
					}
				})
			if runtime.GOOS == "darwin" {
				cd.switchRow("notch", "开启刘海播放器", "指针移到屏幕顶部中央，即可暂停、切歌", st.NotchPlayerOn(),
					func(cur *app.Settings, on bool) { v := on; cur.NotchPlayer = &v })
				cd.switchRow("notchActivity", "播放时显示实时活动", "在刘海两侧显示封面和频谱，可能遮挡旁边的菜单栏图标",
					st.NotchActivityOn(),
					func(cur *app.Settings, on bool) { v := on; cur.NotchActivity = &v })
				cd.switchRow("notchVirtual", "在无刘海的屏幕上常驻显示", "关闭时，将鼠标移到屏幕顶部中央即可唤出",
					st.NotchVirtualOn(),
					func(cur *app.Settings, on bool) { v := on; cur.NotchVirtual = &v })
			}
			cd.switchRow("updateRemind", updateNotificationTitle, "有新版本时提醒你", st.UpdateNotificationEnabled(),
				func(cur *app.Settings, on bool) { v := on; cur.UpdateNotification = &v })
		})
	})

	s.setGroup(c, "download", "下载", func() {
		s.card(c, "download", ro, func(cd *setCard) {
			focused := s.settings.focus == settingsFocusDownload
			p := tok.Of(c.Theme())
			cd.rowFocus("downloadDir", focused, func() {
				t := rowTitle(c, "下载目录")
				if focused {
					t.TextColor(p.AccentText)
				}
				typo.Mono(ui.Text(c, s.downloadDirShown(st))).FontSize(scale.Font(c, scale.Size13)).LineHeight(leading.Body).
					TextColor(p.Text2).Wrap()
			}, func() {
				if pud.ButtonDisabled(c, "set-pick-dir", pud.Ghost, pud.Sm, "更改", "", ro).Clicked() {
					s.pickPath("下载目录", true, func(path string) {
						s.applySetting("下载目录", func(cur *app.Settings) { cur.DownloadPath = path })
						s.addToast("下载目录已改：下载记录页上写的就是它，下完的歌放在那儿", notice.Success)
					})
				}
			})
		})
	})
}

// autoStartSwitch 是开机自启那个开关。
//
// 它不是改一个键就完了：得真的去系统里注册 / 注销登录项（`Shell.SetAutoStart`——先动系统、
// 成功了才写设置）。动不了时弹一条提示，设置一个字都不改，开关下一帧自己弹回去：亮着一个
// 实际上没开的开关比暗着更坏。
func (s *Shell) autoStartSwitch(c *ui.Context, st app.Settings, ro bool) {
	on := st.AutoStart
	if !pud.Switch(c, "set-sw:autostart", &on, "开关：开机自启") || ro {
		return
	}
	if err := s.SetAutoStart(on); err != nil {
		s.addToast("开机自启没设上："+trimAppErr(err), notice.Danger)
		return
	}
	s.saySwitchHint("开机自启", on)
}

// downloadDirShown 是设置页上那一格显示的路径：歌真正落下来的地方，不是设置里存的原始值
// （默认是空串，实际落到「音乐」目录下的应用子目录；和下载记录页页头是同一个字符串）。
func (s *Shell) downloadDirShown(st app.Settings) string {
	if p := st.DownloadPath; p != "" {
		return p
	}
	return s.app.DownloadDir()
}

// pick 是「二选一」的那个小工具，Go 没有内建的三元。
func pick[T any](cond bool, yes, no T) T {
	if cond {
		return yes
	}
	return no
}
