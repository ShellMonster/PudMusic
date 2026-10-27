// 设置页的第一个 Tab：常规。照 Biu 的 `src/pages/settings/system-settings.tsx`
// 逐段抄，顺序也照抄（外观 / 播放 / 下载 / 搜索 / 系统）。
//
// ⚠️ 这一段曾经写着「这一份里没有导入导出配置（PLAN 9.12）」——那句话在说它
// 做的**那一天**就是错的。`settingsConfig`（导出 / 导入 / 从 Biu 导入）在
// **`settings_tab_config.go`**，由本文件末尾的 `settingsGeneral` 调它摆出来；
// 版本号与检查更新也早就在 `update_entry.go` 里了（自动更新，commit `8c936d4`）。
// PLAN 9.12 是「持久化迁移」，跟导入导出配置无关，那个编号也是照错的。
//
// 现在的分工照着读：**外观 / 播放 / 下载 / 搜索 / 系统**在这一份；「关于应用」
// （版本号 + 检查更新 + 更新日志）在 `update_entry.go`；「配置」（导出 / 导入 /
// 从 Biu 导入）在 `settings_tab_config.go`，摆在这一页的最末尾。
//
// 顺带记一句为什么它们要拆开：Biu 那边是长在同一段里的，我们拆开是因为改的不是
// 同一样东西——更新器的状态在 `App` 上，用户设置的状态在设置文件里，而只读保护
// 护的是后者（见 `settingsUpdate`（`update_entry.go`）和 `settingsConfig`
// （`settings_tab_config.go`）各自的注释）。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/shadow"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// displayModes 是「数据显示样式」那三档，顺序就是从左到右的顺序。
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

// audioQualities 是音质那几档。Biu 用的是 `w-[180px]` 的下拉（`:247`），
// 键值一字不差（`app.AudioQuality` 那边对过）。
//
// 🔴 **没有「自动」这一档**（票 #581）：默认是「高品质」（音乐库 320K / 投稿
// 192K），无损/杜比是显式选项——以前这里的「自动」有无损先抢无损，每一首
// 投稿都按 9 倍于 192K 的码率起播。设置文件里存着的 `"auto"` 由
// `app.Settings` 归一化兜底成「高品质」，这里不用管。
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

// audioQualityHint 是每一档底下那句话。它跟着选中的那一档变：用户从「无损」
// 换到「高品质」时底下那句话要跟着换，否则「这一档到底是什么」还是没地方看。
//
// ⚠️ **无损/杜比的两句要说清「拿不到会退档」**（票 #581）：点名档不是每首
// 都有，拿不到时自动退到最高有损，界面上另有一句提示说了实际用的哪一档。
var audioQualityHint = map[app.AudioQuality]string{
	app.AudioQualityLossless: "FLAC / Hi-Res（这首没有就自动用 192K）",
	app.AudioQualityDolby:    "杜比全景声（这首没有就自动用 192K）",
	app.AudioQualityHigh:     "192~320 kbps（默认）",
	app.AudioQualityMedium:   "128 kbps",
	app.AudioQualityLow:      "64 kbps",
}

// settingsGeneral 是常规那一页。
func (s *Shell) settingsGeneral(c *ui.Context, st app.Settings, ro bool) {
	s.settingGroup(c, "外观", func() {
		// ⚠️ 这一格走 `viewDisplayMode`（切换 + 预览），不是光 `pickDisplayMode`：预览
		// 是这一格的一部分，理由见 displaymode.go 的文件头。
		s.settingRow(c, "数据显示样式", "",
			func() { s.viewDisplayMode(c, st, ro) })
		s.settingRow(c, "主题方案", "",
			func() { s.pickThemePreset(c, st, ro) })
		s.settingRow(c, "主题", "",
			func() { s.pickTheme(c, st, ro) })
		s.settingRow(c, "颜色", "",
			func() { s.pickColors(c, st, ro) })
		s.settingRow(c, "封面取色", "",
			func() {
				s.pickSwitch(c, "封面取色", "封面取色", st.CoverAccent, ro,
					func(cur *app.Settings, on bool) { cur.CoverAccent = on })
			})
		s.settingRow(c, "字体", "",
			func() { s.pickFont(c, st, ro) })
		s.settingRow(c, "圆角", "",
			func() { s.pickRadius(c, st, ro) })

	})
	s.settingGroup(c, "播放", func() {
		s.settingRow(c, "音质偏好", "",
			func() { s.pickAudioQuality(c, st, ro) })
		s.settingRow(c, "上报本机播放记录", "",
			func() {
				s.pickSwitch(c, "上报本机播放记录", "播放记录上报", st.ReportPlayHistory, ro,
					func(cur *app.Settings, on bool) { cur.ReportPlayHistory = on })
			})
		s.settingRow(c, "切歌淡入淡出", "",
			func() { s.pickTrackFade(c, st, ro) })
		s.settingRow(c, "视频画质", "",
			func() { s.pickVideoQuality(c, st, ro) })
		s.settingRow(c, "均衡器", "",
			func() { s.pickEQ(c, st, ro) })
		s.settingRow(c, "音量均衡", "",
			func() {
				s.pickSwitch(c, "音量均衡", "音量均衡", st.NormalizeVolume, ro,
					func(cur *app.Settings, on bool) { cur.NormalizeVolume = on })
			})

	})
	s.settingGroup(c, "下载", func() {
		// ⚠️ **这一行有身份**（`settingRowAt`）：从下载记录页跳过来的时候要指着它
		// （见 goToDownloadDirSetting）——设置页有四个 Tab、几十行，不指一下用户
		// 还得自己找「下载目录」在哪儿。
		s.settingRowAt(c, string(settingsFocusDownload), "下载目录", "",
			func() { s.pickDownloadPath(c, st, ro) })
		s.settingRow(c, "FFmpeg 路径", "",
			func() { s.pickFFmpegPath(c, st, ro) })
	})

	s.pickLocalMusicDirs(c, st, ro)

	s.settingGroup(c, "搜索", func() {
		s.settingRow(c, "显示搜索历史", "",
			func() {
				s.pickSwitch(c, "显示搜索历史", "显示搜索历史", st.ShowSearchHistory, ro,
					func(cur *app.Settings, on bool) { cur.ShowSearchHistory = on })
			})

	})
	s.settingGroup(c, "系统", func() {
		s.settingRow(c, "窗口关闭", "",
			func() { s.pickCloseWindow(c, st, ro) })
		s.settingRow(c, "开机自启", "",
			func() { s.pickAutoStart(c, st, ro) })

		// 「关于应用」摆在最后，照 Biu 的 `system-settings.tsx` 那一段的顺序。
		//
		// ⚠️ 它**不是** `settingRow`，也不吃 `ro`：更新那一摊的状态在 `App` 上，不在
		// 设置文件里，而「只读」保护的是**设置文件**——更新器写的是自己那一份
		// （`app-update.json`），从没碰过用户的 `app-settings.json`。让这一格跟着
		// `ro` 变灰，会在「设置来自更新的版本」这一个场景里，把一个本来完全可用的
		// 更新入口关掉。
		//
		// ⚠️ 顺序：**在**「配置」段（导出 / 导入 / 从 Biu 导入）**之前**。Biu 那边
		// 导入导出就是长在「关于应用」这一段里的——版本号 + 检查更新那一行之后
		// 紧接着就是 `<ImportExport />`（`system-settings.tsx:388-402`）。两边各自
		// 加了一段，谁在前谁在后没人对过，这里按 Biu 定下来。
	})
	s.settingsUpdate(c, s.app.UpdateSnapshot())

	// 「配置」段（导出 / 导入 / 从 Biu 导入），照 Biu 的位置。
	s.settingsConfig(c)
}

// pickSwitch 是一个开关行。set 拿到的是这一帧开关上的新值。
//
// 写不进去（只读）时下一帧 `on` 会重新从设置里读出来，开关自己弹回去——和
// pickAutoStart 里那个开关是同一个道理，注释见那里。
//
// ⚠️ 那个 `Label` **不能省**：开关画出来只有一个旋钮，一个字都没有，于是它对读屏
// 软件、对无头 Tester 都是「一个没有名字的东西」。左边那行标题是它的兄弟节点，
// 不是它的名字——不配 Label 的话读屏念出来只有「开关」，用户不知道是哪一个。
//
// 名字写成「开关：那一行的标题」而不是照抄标题，有两个用处：读屏念出来是
// 「开关：上报本机播放记录」（知道这是个开关）；界面上那行标题**一字不差**地摆着，
// 名字撞上去的话无头 Tester 的 `Find` 只会找到标题那一处，点都点不到开关。
//
// ⚠️ **效果看不见的那几格，拨开之后要说一句**（`saySwitchHint`）：界面上没有任何
// 变化能告诉用户「它生效了」。说得出口的是那一句短的，说不出口的不说（理由见
// `switch_hint.go`）。
func (s *Shell) pickSwitch(c *ui.Context, title, key string, on bool, ro bool, set func(*app.Settings, bool)) {
	// ⚠️ 和 pickAutoStart 一样不给 Switch 加 Key（见那里的注释：加了 Key 之后
	// 旋钮动画每帧从头开始，那一页永远停不下来）。
	if springSwitch(c, &on).Label("开关：" + title).Disabled(ro).Changed() {
		// ⚠️ **写成功了才说**：写失败的时候已经弹了一条「没存上」，再说一句
		// 「开了它会怎样」就是两句话打架（`applySettingOK` 那一版）。
		if s.applySettingOK(key, func(cur *app.Settings) { set(cur, on) }) {
			s.saySwitchHint(title, on)
		}
	}
}

// pickDisplayMode 是「数据显示样式」那三档。
//
// ⚠️ **它和「主题」「窗口关闭」那两处走同一个出口**（`settingsSegment`）：
// 设置页只有一种分段控件（票 #240 / 报告 F-17）。
//
// 这一格此前是框架的 `ui.Segmented`（**灰底轨道 + 白色滑块**），而同一页的
// 「主题」是自绘的三个按钮（**没有轨道，选中的那一档是实心主色底**）——两个并列的
// 分段控件长得不一样。为什么归到实心主色底那一档而不是反过来，理由写在
// `settingsSegment` 的注释里。
func (s *Shell) pickDisplayMode(c *ui.Context, st app.Settings, ro bool) {
	chosen := 0
	opts := make([]segmentOption, len(displayModes))
	for i, m := range displayModes {
		opts[i] = segmentOption{label: m.label, icon: m.icon}
		if m.mode == st.DisplayMode {
			chosen = i
		}
	}
	s.settingsSegment(c, "setting:displayModePick", "显示样式档位", 0,
		opts, chosen, ro, func(i int) {
			mode := displayModes[i].mode
			s.applySetting("数据显示样式", func(cur *app.Settings) { cur.DisplayMode = mode })
		})
}

// pickRadius 是全局圆角：一个滑块加一个 px 读数。
//
// 拖动时每一帧都是一次改动，落盘在后台合并成一次（`writeDebounce`，见
// internal/app/settings.go），不是一帧写一遍盘。读数显示的是这一帧拖到的值，
// 不是设置里还存着的旧值——慢一帧的话读数会一直追不上手指。
func (s *Shell) pickRadius(c *ui.Context, st app.Settings, ro bool) {
	v := st.BorderRadius
	// 套了方案时，滑块显示方案真在用的圆角，而不是被压过的设置值。
	if st.ThemePreset != app.ThemePresetClassic {
		v = float64(c.Theme().Radius)
	}
	ui.Row(c).Key("setting:borderRadius").Width(s.settingsControlFitW(c, settingsControlW)).
		Gap(space.Px12).
		AlignItems(ui.Center).Children(func() {
		sl := ui.Slider(c, &v, 0, app.MaxBorderRadius).Grow(1).Label("全局圆角").Disabled(ro)
		if sl.Changed() {
			s.applySetting("圆角", func(cur *app.Settings) { cur.BorderRadius, cur.ThemePreset = v, app.ThemePresetClassic })
		}
		ui.Textf(c, "%.0fpx", v).FontSize(scale.Font(c, scale.Size12)).Width(radiusReadoutW)
	})
}

// pickAudioQuality 是音质那个下拉。
func (s *Shell) pickAudioQuality(c *ui.Context, st app.Settings, ro bool) {
	items := make([]enumItem[app.AudioQuality], len(audioQualities))
	for i, q := range audioQualities {
		items[i] = enumItem[app.AudioQuality]{value: q.quality, label: q.label}
	}
	// 一帧一读的局部量：控件建的时候把新值写进 `quality`，紧接着这里读到的就是它。
	// 存不进去（只读）时下一帧重新从设置里读，下拉自己弹回去。
	quality := st.AudioQuality
	enumSelect(c, &quality, items, "选择音质偏好", s.settingsControlFitW(c, selectWidth), 0, ro)
	if quality != st.AudioQuality {
		s.applySetting("音质偏好", func(cur *app.Settings) { cur.AudioQuality = quality })
		// 应用层把这一笔同步给正在放的那首（`UpdateSettings` → `SetAudioQuality`）。
		// 切到无损/杜比的那一下说一句降档规矩——只在切换时说，理由见那边的注释。
		qualitySwitchToast(s, app.PrefToBili(quality))
	}
}

// pickDownloadPath 是下载目录那一格：一个只读的路径 + 一个「选择」。
//
// ⚠️ **改完要说一声它管到哪儿去**（报告 D3）：设置页和下载记录页是**两页**，
// 用户在这一页改完，看不见另一页变了——而「改了一个地方、另一个地方不变」是
// 用户最容易当成「设置没生效」的那一种形态。这一句说清两件事：下载记录页上
// 写的就是它，下完的歌也放在那儿。
func (s *Shell) pickDownloadPath(c *ui.Context, st app.Settings, ro bool) {
	ui.Row(c).Key("setting:downloadPath").Width(s.settingsControlFitW(c, settingsControlW)).
		Gap(space.Px8).Children(func() {
		pathField(c, s.downloadDirShown(st), "下载目录", ro)
		if settingsButton(c, "选择").Width(72).Disabled(ro).Clicked() {
			s.pickPath("下载目录", true, func(path string) {
				s.applySetting("下载目录", func(cur *app.Settings) { cur.DownloadPath = path })
				s.addToast("下载目录已改：下载记录页上写的就是它，下完的歌放在那儿",
					notice.Success)
			})
		}
	})
}

// downloadDirShown 是设置页上那一格显示的路径。
//
// ⚠️ **显示的是「歌真正落下来的地方」，不是设置里存的那个原始值**：设置里
// `downloadPath` 的默认值是**空串**（`DefaultSettings("")`），而绝大多数用户
// 走的正是这一条——空串显示成「未设置」，而歌其实落到了「音乐」目录下的应用
// 子目录里。用户看到的和实际发生的对不上，而这一格恰恰是回答「我的歌下到哪儿
// 了」的那一格。
//
// 所以没选过时显示**默认落点**（`App.DownloadDir()`，也就是
// `~/Music/BiuGo` 那条路），选过就显示选过的那个。
//
// ⚠️ **它和下载记录页页头上写的那条路径是同一个字符串**：按钮上写的、按下去
// 打开的、歌真正落下来的地方，必须是同一个（见 `app.DownloadDir` 的注释）。
func (s *Shell) downloadDirShown(st app.Settings) string {
	if p := st.DownloadPath; p != "" {
		return p
	}
	return s.app.DownloadDir()
}

// pickFFmpegPath 是 FFmpeg 路径那一格，和下载目录同一套。
func (s *Shell) pickFFmpegPath(c *ui.Context, st app.Settings, ro bool) {
	ui.Row(c).Key("setting:ffmpegPath").Width(s.settingsControlFitW(c, settingsControlW)).
		Gap(space.Px8).Children(func() {
		pathField(c, st.FFmpegPath, "FFmpeg 路径", ro)
		if settingsButton(c, "选择").Width(72).Disabled(ro).Clicked() {
			s.pickPath("FFmpeg 路径", false, func(path string) {
				s.applySetting("FFmpeg 路径", func(cur *app.Settings) { cur.FFmpegPath = path })
			})
		}
	})
}

// pathField 是一个只读的路径输入框，空的时候摆「未设置」两个字。
//
// Biu 那边用的是 `isDisabled`（`system-settings.tsx:295`），连字一起灰掉，用户
// 看不出那里本来摆着一条路径。这里用 `ReadOnly`：字还能被选中复制（「这个目录
// 到底是哪儿」是用户真会问的问题），要改只能走「选择」。
//
// ⚠️ **它是包级变量而不是方法**：设置页上那一格显示的**是哪条路径**，是
// 「用户看到的当前下载目录」这件事的全部，而只读输入框里的那个值**不在渲染树
// 里**——`Tester.Texts()` 量不到它（那是框架画的，不是元素）。所以「那一格拿到
// 了哪条路径」只能从**这一层**钉，而这一层要能替身，替身就得是个变量。
//
// （跟 `userDataDir`（用户数据目录）、`coverCacheDir`（封面缓存）是同一类接缝。）
var pathField = func(c *ui.Context, path, label string, ro bool) {
	shown := path
	if shown == "" {
		shown = "未设置"
	}
	// ⚠️ **不许给输入框加 Key**：Key 只能挂在「创建时没有处理输入」的控件上，
	// 挂上去直接 panic（`ui/element.go:658`）。外面那一格有 Key 就够了。
	ui.TextInput(c, &shown).Grow(1).Height(settingsSelectH).Label(label).ReadOnly(true)
}

// ─────────────────────────────────────────────────────────────────────────────
// 颜色
// ─────────────────────────────────────────────────────────────────────────────

// 色块多大。Biu 是 `w-[100px]` 加一个 `h-[40px]` 的上半截加一行字
// （`color-settings.tsx:26-28`）。
//
// ⚠️ **宽度跟着控件列的基准走**（`settingsControlW`）：这一行里只有那两块色卡，
// 它们不填满那一格的话，**这一行的右边缘就比上面那几行短一截**——那正是
// 「控件列没有共同基准」的一半（UI 报告 §4 的 F-04）。
//
// 上游是 100：它那一行的格子也就是它自己的宽度，而**色块是纯色**——宽一点
// 不损失任何信息（颜色还是那个颜色），换来的是这一行和上面几行一样齐。
const (
	colorItemGap    = space.Px12
	colorItemHeight = 40
)

// colorItemWidth 是**一块**色卡多宽：那一格 340 宽，中间 12 的缝。
const colorItemWidth = (settingsControlW - colorItemGap) / 2

// pickColors 是背景色和主色那两块。照 Biu 的 `ColorSettings`：两块并排。
//
// ⚠️ **两块是并排铺满控件列那一格的**（各占一半，中间 12 的缝），不是上游那个
// 100 宽各自一块：这一行里只有它们，不铺满的话这一行的右边缘就比上面那几行
// 短一截（UI 报告 §4 的 F-04）。
//
// ⚠️ **宽度走 `settingsControlFitW`**：色卡是定宽的两块，窄窗口那一档不钳的话
// 会和下拉一样溢出分组卡片。
//
// ⚠️ **两列顶对齐**（`AlignItems(ui.Start)`）：这一行的 Row 默认把子项**垂直
// 居中**，而「用默认」只出现在自定义过的那块底下——只有一块有它的时候，另一块
// 整列被居中得往下挪（2026-10-26 用户截图实测挪了 14 像素，两块色卡一高一低）。
// 顶对齐之后色卡永远齐着，多出来的「用默认」只在底下多占一行。
//
// 背景色的默认值是**空串**，意思是「跟着主题走」。Biu 那一格在值为空时显示的是
// 当前主题的背景色（`pickerValue = isHex(value) ? value : backgroundColor`），
// 点开挑色才写进去——照抄。标签行上另加一个「用默认」，Biu 没有那个出口：挑过一
// 次之后就再也回不到「跟着主题走」，而那正是多数人想要的常态。
func (s *Shell) pickColors(c *ui.Context, st app.Settings, ro bool) {
	// 「当前主题的背景色」就是这一帧真在画的那份——ApplyTheme 在 view 最开头
	// 已经装好了，所以 `c.Theme()` 读到的就是它，不用再拼一遍。
	dark := c.Theme().Dark
	ui.Row(c).Key("setting:colors").Width(s.settingsControlFitW(c, settingsControlW)).
		Gap(colorItemGap).AlignItems(ui.Start).Children(func() {
		bg := pick(st.BackgroundColor != "", st.BackgroundColor, themeBackground(dark))
		// 套了方案时，色块要显示**方案真在画的色**，不是设置里那两个已被压过的值——
		// 否则暖纸底上摆着一块白、一块 Biu 绿，用户以为那就是当前配色。
		if st.ThemePreset != app.ThemePresetClassic {
			bg = toHex(c.Theme().Background)
		}
		s.colorItem(c, "背景", bg, ro, st.BackgroundColor != "", func(col string) {
			s.applySetting("背景色", func(cur *app.Settings) { cur.BackgroundColor, cur.ThemePreset = col, app.ThemePresetClassic })
		}, func() {
			s.applySetting("背景色", func(cur *app.Settings) { cur.BackgroundColor, cur.ThemePreset = "", app.ThemePresetClassic })
		})

		primary := pick(st.PrimaryColor != app.DefaultPrimaryColor,
			st.PrimaryColor, app.DefaultPrimaryColor)
		if st.ThemePreset != app.ThemePresetClassic {
			primary = toHex(c.Theme().Accent)
		}
		s.colorItem(c, "主色", primary, ro, st.PrimaryColor != app.DefaultPrimaryColor,
			func(col string) {
				s.applySetting("主色", func(cur *app.Settings) { cur.PrimaryColor, cur.ThemePreset = col, app.ThemePresetClassic })
			}, func() {
				s.applySetting("主色", func(cur *app.Settings) {
					cur.PrimaryColor, cur.ThemePreset = app.DefaultPrimaryColor, app.ThemePresetClassic
				})
			})
	})
}

// colorItem 是一块颜色：一个**全出血的色块** + 底下一行「标签 / 用默认」。
//
// # 色块就是取色板本身
//
// 画它的是 `ui.ColorWell`：点它在自己的下面摆出 AppKit 那样的取色板（色相方块 +
// 亮度条 + 不透明度 + 手写 hex + 常用色），Esc 或者点外面关掉，`.Changed()` 报
// 新颜色。但框架的 ColorWell 自带一层「输入框」的壳——`t.Surface` 底、1px 描边、
// 中间一颗 36×20 的小色块（`mygo/ui/colorpicker.go` 的 `coreColorWell`）——
// 此前把套在一张色卡里面，那层壳把色卡的底色整个盖住，画面上是**两个输入框、
// 每个中间一小块颜色**，看不出是色卡（2026-10-26 用户原话：「一堆省略号看不出
// 是啥」那一张截图的下半截）。
//
// 现在反过来：**链 `.Background(col)` 把壳的底色换成这个颜色本身**，色块
// 从头到脚都是它，中间那颗小色块同色相融（不透明色看不出来；半透明色的中间
// 会露出取色板的棋盘格，正好当「这一格带透明度」的提示）。
//
// ⚠️ **允许写 `#rgb` / `#rgba` 的问题**：取色板允许写 3/4 位 hex，而设置那边的
// normalize 只认 6 位和 8 位（`internal/app/settings.go` 的 hexColor）——所以
// 写进去之前统一补齐成 6 位，不补的话用户挑了个 `#f00`，落盘时被 normalize
// 悄悄换回默认色，而界面上还亮着他刚挑的那个颜色。
//
// # 「用默认」和标签同一行
//
// 两列的结构因此**永远一样**（色块 + 一行字），不管哪块自定义过、哪块没有——
// 「用默认」单独占一行的话，只有一块出现它，两列就不一样高（这一行的 Row 又
// 是居中对齐的，色卡会一高一低）。那一行左边是标签（12 / 次要色），右边是
// 「用默认」（11 / 次要色），只在**自定义过**且**不只读**时出现。
func (s *Shell) colorItem(c *ui.Context, label, hex string, ro, overridden bool, set func(string), reset func()) {
	t := c.Theme()
	col := colorOfHex(hex)
	ui.Column(c).Grow(1).MaxWidth(colorItemWidth).Gap(space.Px4).Children(func() {
		// ⚠️ **那一圈影是上游给的**：Biu 那一格是一张 HeroUI 的卡（描边 +
		// `shadow="sm"`）。HeroUI 的 `shadow="sm"` 深浅两套值不一样（见
		// `widgets/shadow` 的文件头）：浅色那套是 1 像素的**外**黑边，深色那套
		// 是 1 像素的**内**白边——深底上黑边看不见，那道白边就是这张卡和底下
		// 那一页之间的分界。
		well := ui.ColorWell(c, &col).FillWidth().Height(colorItemHeight).
			Background(col).Radius(t.Radius).Border(1, t.Border)
		well = shadow.HeroSmall(well, t)
		well = shadow.HeroInnerRim(well, t)
		well.Label(label).Disabled(ro)
		if well.Changed() {
			set(toHex(col))
		}
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, label).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
			if overridden && !ro {
				// ⚠️ **按钮的垂直内边距收成 0**：它和标签同行，行高由最高那个孩子
				// 决定——留着 flatButton 默认那 6×2 的垂直内边距，这一行会比另一块
				// 色卡的标签行高出一截，行内的标签被居中得往下挪，两块色卡的标签
				// 基线就错开了（实测 5 像素）。收成 0 之后按钮的盒子和标签一样高，
				// 两行同高、基线齐。
				if flatButton(c, "用默认").FontSize(scale.Font(c, scale.Size11)).
					Padding(space.None, space.Px10).TextColor(t.TextMuted).Clicked() {
					reset()
				}
			}
		})
	})
}

// colorOfHex 把设置里那个字符串变成一个颜色，认不出就退回不透明的黑。
//
// 解析用的是 `theme.go` 里那一份，**不是** `ui.Hex`：后者对不合法的输入直接
// panic（`ui/color.go:33`），而这里是设置文件里的用户输入，为一个画错的颜色把
// 整个进程带走不划算。
func colorOfHex(s string) ui.Color {
	if col, ok := parseHexColor(s); ok {
		return col
	}
	return ui.Color{A: 255}
}

// themeBackground 是深浅两档各自默认的背景色。
func themeBackground(dark bool) string {
	if dark {
		return "#18181b"
	}
	return "#ffffff"
}

// toHex 把一个颜色写成设置里存的那个串。
//
// 🔴 **不透明的时候才写 6 位；半透明写 8 位**（`#rrggbbaa`）。只写 RGB 的话，取色板
// 上那条 **Opacity** 滑块调出来的透明度会被**悄悄丢掉**：用户拖到一半，界面上是半透明
// 的，存下去却是全不透明，下一帧又跳回去（实测：拖到中点保存下来还是 `#ef4444`，
// `BuildTheme().Accent.A` 是 255 而不是 ~128）。
//
// 8 位写法设置那边认得：`internal/app/settings.go` 的 `hexColor` 收 3/4/6/8 位，
// `theme.go` 的 `parseHexColor` 也解 8 位——所以这不是新格式，是那个格式本来就有、
// 一直没人写。
func toHex(col ui.Color) string {
	const hex = "0123456789abcdef"
	out := "#" + string([]byte{
		hex[col.R>>4], hex[col.R&0xf],
		hex[col.G>>4], hex[col.G&0xf],
		hex[col.B>>4], hex[col.B&0xf],
	})
	// 全不透明时仍然写 6 位：Biu 的默认色是 `#1ed760` 这种 6 位的，多两位会让
	// 「和 Biu 存的那份一模一样」这件事变得比必要的更难。
	if col.A == 255 {
		return out
	}
	return out + string([]byte{hex[col.A>>4], hex[col.A&0xf]})
}

// pick 是「三样里挑一样」的那个小工具，Go 没有内建的三元。
func pick[T any](cond bool, yes, no T) T {
	if cond {
		return yes
	}
	return no
}
