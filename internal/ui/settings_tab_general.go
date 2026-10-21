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
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// displayModes 是「数据显示样式」那三档，顺序就是从左到右的顺序。
//
// Biu 那一格是三个带图标的 Tab（列表 / 网格 / 紧凑，`system-settings.tsx:65-101`）。
// ⚠️ **这一格还没换成图标**：图标资源里列表 / 网格那两枚已经备齐（`iconViewList` /
// `iconViewGrid`，见 `icons.go`），而**紧凑那一档上游用的是哪一枚还没抄**——三档
// 各配一枚，配错了用户切档时看到的是同一张图。用同一个控件摆三个字的话，值和
// 顺序一字不差。
var displayModes = []struct {
	mode  app.DisplayMode
	label string
}{
	{app.DisplayModeList, "列表"},
	{app.DisplayModeCard, "网格"},
	{app.DisplayModeCompact, "紧凑"},
}

// audioQualities 是音质那五档。Biu 用的是 `w-[180px]` 的下拉（`:247`），
// 键值一字不差（`app.AudioQuality` 那边对过）。
var audioQualities = []struct {
	quality app.AudioQuality
	label   string
}{
	{app.AudioQualityAuto, "自动"},
	{app.AudioQualityLossless, "无损"},
	{app.AudioQualityHigh, "高品质"},
	{app.AudioQualityMedium, "中等"},
	{app.AudioQualityLow, "低品质"},
}

// audioQualityHint 是每一档底下那句话，逐条抄 Biu 的（`system-settings.tsx:236-241`）。
// 它跟着选中的那一档变：用户从「自动」换到「无损」时底下那句话要跟着换，否则
// 「这一档到底是什么」还是没地方看。
var audioQualityHint = map[app.AudioQuality]string{
	app.AudioQualityAuto:     "自动选择最高音质",
	app.AudioQualityLossless: "FLAC / Hi-Res",
	app.AudioQualityHigh:     "180-320 kbps",
	app.AudioQualityMedium:   "100-140 kbps",
	app.AudioQualityLow:      "60-80 kbps",
}

// settingsGeneral 是常规那一页。
func (s *Shell) settingsGeneral(c *ui.Context, st app.Settings, ro bool) {
	s.settingSection(c, "外观")
	// ⚠️ 这一格走 `viewDisplayMode`（切换 + 预览），不是光 `pickDisplayMode`：预览
	// 是这一格的一部分，理由见 displaymode.go 的文件头。
	s.settingRow(c, "数据显示样式", "内容页排成列表、网格还是紧凑",
		func() { s.viewDisplayMode(c, st, ro) })
	s.settingRow(c, "主题", "选择浅色或深色主题",
		func() { s.pickTheme(c, st, ro) })
	s.settingRow(c, "颜色", "背景色和主色，点色块挑颜色",
		func() { s.pickColors(c, st, ro) })
	s.settingRow(c, "字体", "选择界面显示的字体",
		func() { s.pickFont(c, st, ro) })
	s.settingRow(c, "圆角", "调整界面控件的圆角大小",
		func() { s.pickRadius(c, st, ro) })

	s.settingSectionEnd(c)
	s.settingSection(c, "播放")
	s.settingRow(c, "音质偏好", audioQualityHint[st.AudioQuality],
		func() { s.pickAudioQuality(c, st, ro) })
	s.settingRow(c, "上报本机播放记录", "将播放进度同步到Bilibili服务器",
		func() {
			s.pickSwitch(c, "上报本机播放记录", "播放记录上报", st.ReportPlayHistory, ro,
				func(cur *app.Settings, on bool) { cur.ReportPlayHistory = on })
		})

	s.settingSectionEnd(c)
	s.settingSection(c, "下载")
	s.settingRow(c, "下载目录", "选择音视频保存的位置",
		func() { s.pickDownloadPath(c, st, ro) })
	s.settingRow(c, "FFmpeg 路径", "手动指定 FFmpeg 可执行文件路径",
		func() { s.pickFFmpegPath(c, st, ro) })

	s.pickLocalMusicDirs(c, st, ro)

	s.settingSectionEnd(c)
	s.settingSection(c, "搜索")
	s.settingRow(c, "显示搜索历史", "在搜索框中显示搜索历史记录",
		func() {
			s.pickSwitch(c, "显示搜索历史", "显示搜索历史", st.ShowSearchHistory, ro,
				func(cur *app.Settings, on bool) { cur.ShowSearchHistory = on })
		})

	s.settingSectionEnd(c)
	s.settingSection(c, "系统")
	s.settingRow(c, "窗口关闭", "选择窗口关闭时的行为",
		func() { s.pickCloseWindow(c, st, ro) })
	s.pickTrayNote(c)
	s.settingRow(c, "开机自启", "系统登录后自动启动应用",
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
	s.settingSectionEnd(c)
	s.settingsUpdate(c, s.app.UpdateSnapshot())

	s.settingSectionEnd(c)
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
func (s *Shell) pickSwitch(c *ui.Context, title, key string, on bool, ro bool, set func(*app.Settings, bool)) {
	// ⚠️ 和 pickAutoStart 一样不给 Switch 加 Key（见那里的注释：加了 Key 之后
	// 旋钮动画每帧从头开始，那一页永远停不下来）。
	if staticSwitch(c, &on).Label("开关：" + title).Disabled(ro).Changed() {
		s.applySetting(key, func(cur *app.Settings) { set(cur, on) })
	}
}

// pickDisplayMode 是「数据显示样式」那三档。
func (s *Shell) pickDisplayMode(c *ui.Context, st app.Settings, ro bool) {
	chosen := 0
	for i, m := range displayModes {
		if m.mode == st.DisplayMode {
			chosen = i
		}
	}
	labels := make([]string, len(displayModes))
	for i, m := range displayModes {
		labels[i] = m.label
	}
	if ui.Segmented(c, &chosen, labels...).Disabled(ro).Changed() {
		mode := displayModes[chosen].mode
		s.applySetting("数据显示样式", func(cur *app.Settings) { cur.DisplayMode = mode })
	}
}

// pickRadius 是全局圆角：一个滑块加一个 px 读数。
//
// 拖动时每一帧都是一次改动，落盘在后台合并成一次（`writeDebounce`，见
// internal/app/settings.go），不是一帧写一遍盘。读数显示的是这一帧拖到的值，
// 不是设置里还存着的旧值——慢一帧的话读数会一直追不上手指。
func (s *Shell) pickRadius(c *ui.Context, st app.Settings, ro bool) {
	v := st.BorderRadius
	ui.Row(c).Key("setting:borderRadius").Width(radiusWidth).Gap(12).
		AlignItems(ui.Center).Children(func() {
		sl := ui.Slider(c, &v, 0, app.MaxBorderRadius).Grow(1).Label("全局圆角").Disabled(ro)
		if sl.Changed() {
			s.applySetting("圆角", func(cur *app.Settings) { cur.BorderRadius = v })
		}
		ui.Textf(c, "%.0fpx", v).FontSize(scale.Font(c, 12)).Width(radiusReadoutW)
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
	enumSelect(c, &quality, items, "选择音质偏好", selectWidth, ro)
	if quality != st.AudioQuality {
		s.applySetting("音质偏好", func(cur *app.Settings) { cur.AudioQuality = quality })
	}
}

// pickDownloadPath 是下载目录那一格：一个只读的路径 + 一个「选择」。
func (s *Shell) pickDownloadPath(c *ui.Context, st app.Settings, ro bool) {
	ui.Row(c).Key("setting:downloadPath").Width(radiusWidth).Gap(8).Children(func() {
		s.pathField(c, st.DownloadPath, "下载目录", ro)
		if flatButton(c, "选择").Width(64).Disabled(ro).Clicked() {
			s.pickPath("下载目录", true, func(path string) {
				s.applySetting("下载目录", func(cur *app.Settings) { cur.DownloadPath = path })
			})
		}
	})
}

// pickFFmpegPath 是 FFmpeg 路径那一格，和下载目录同一套。
func (s *Shell) pickFFmpegPath(c *ui.Context, st app.Settings, ro bool) {
	ui.Row(c).Key("setting:ffmpegPath").Width(radiusWidth).Gap(8).Children(func() {
		s.pathField(c, st.FFmpegPath, "FFmpeg 路径", ro)
		if flatButton(c, "选择").Width(64).Disabled(ro).Clicked() {
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
func (s *Shell) pathField(c *ui.Context, path, label string, ro bool) {
	shown := path
	if shown == "" {
		shown = "未设置"
	}
	// ⚠️ **不许给输入框加 Key**：Key 只能挂在「创建时没有处理输入」的控件上，
	// 挂上去直接 panic（`ui/element.go:537`）。外面那一格有 Key 就够了。
	ui.TextInput(c, &shown).Grow(1).Label(label).ReadOnly(true)
}

// ─────────────────────────────────────────────────────────────────────────────
// 颜色
// ─────────────────────────────────────────────────────────────────────────────

// 色块多大。Biu 是 `w-[100px]` 加一个 `h-[40px]` 的上半截加一行字
// （`color-settings.tsx:26-28`）。
const (
	colorItemWidth  = 100
	colorItemHeight = 40
)

// pickColors 是背景色和主色那两块。照 Biu 的 `ColorSettings`：两块并排，靠右。
//
// 背景色的默认值是**空串**，意思是「跟着主题走」。Biu 那一格在值为空时显示的是
// 当前主题的背景色（`pickerValue = isHex(value) ? value : backgroundColor`），
// 点开挑色才写进去——照抄。上面另加一个「用默认」，Biu 没有那个出口：挑过一次之
// 后就再也回不到「跟着主题走」，而那正是多数人想要的常态。
func (s *Shell) pickColors(c *ui.Context, st app.Settings, ro bool) {
	// 「当前主题的背景色」就是这一帧真在画的那份——ApplyTheme 在 view 最开头
	// 已经装好了，所以 `c.Theme()` 读到的就是它，不用再拼一遍。
	dark := c.Theme().Dark
	ui.Row(c).Key("setting:colors").Width(segWidth + 12).Gap(12).Children(func() {
		bg := pick(st.BackgroundColor != "", st.BackgroundColor, themeBackground(dark))
		s.colorItem(c, "背景", bg, ro, st.BackgroundColor != "", func(col string) {
			s.applySetting("背景色", func(cur *app.Settings) { cur.BackgroundColor = col })
		}, func() {
			s.applySetting("背景色", func(cur *app.Settings) { cur.BackgroundColor = "" })
		})

		primary := pick(st.PrimaryColor != app.DefaultPrimaryColor,
			st.PrimaryColor, app.DefaultPrimaryColor)
		s.colorItem(c, "主色", primary, ro, st.PrimaryColor != app.DefaultPrimaryColor,
			func(col string) {
				s.applySetting("主色", func(cur *app.Settings) { cur.PrimaryColor = col })
			}, func() {
				s.applySetting("主色", func(cur *app.Settings) { cur.PrimaryColor = app.DefaultPrimaryColor })
			})
	})
}

// colorItem 是一块颜色：一个色块 + 一个「用默认」。
//
// 色块是 `ui.ColorWell`：点它在自己的下面摆出 AppKit 那样的取色板（色相方块 +
// 亮度条 + 不透明度 + 手写 hex + 常用色），Esc 或者点外面关掉，`.Changed()` 报
// 新颜色。取色板允许写 `#rgb` / `#rgba`，而设置那边的 normalize 只认 6 位和 8 位
// （`internal/app/settings.go` 的 hexColor）——所以写进去之前统一补齐成 6 位，
// 不补的话用户挑了个 `#f00`，落盘时被 normalize 悄悄换回默认色，而界面上还亮着
// 他刚挑的那个颜色。
func (s *Shell) colorItem(c *ui.Context, label, hex string, ro, overridden bool, set func(string), reset func()) {
	t := c.Theme()
	col := colorOfHex(hex)
	ui.Column(c).Width(colorItemWidth).Gap(4).Children(func() {
		ui.Box(c).Width(colorItemWidth).Height(colorItemHeight).Radius(t.Radius).
			Background(col).Border(1, t.Border).Children(func() {
			if ui.ColorWell(c, &col).Fill().Disabled(ro).Label(label).Changed() {
				set(toHex(col))
			}
		})
		ui.Text(c, label).FontSize(scale.Font(c, 12)).TextColor(t.TextMuted).Center()
		if overridden && !ro {
			if flatButton(c, "用默认").FontSize(scale.Font(c, 11)).TextColor(t.TextMuted).Clicked() {
				reset()
			}
		}
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
