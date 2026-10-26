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
// # 四个开关**每一个都真的管画面**（票 #236）
//
// 「显示歌词」和「歌词字体颜色」以前是**死控件**：偏好里存着，而满屏那一屏
// 没有歌词视图（歌词区在迷你播放器那一页，见 `nowplaying.go`），拨了画面上什么
// 都不会变——用户只会以为整个设置面板坏了。现在满屏接上了歌词
// （`fullplayer_lyrics.go`），那两个开关真的管右边那一列歌词。
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
//
// ## 面板顶上那一行
//
// 标题 + 一枚「×」（UX 报告 B4：面板没有标题、没有关闭方式）。

package ui

import (
	"log"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	// fullSettingsTitle 是偏好面板的标题。
	fullSettingsTitle = "显示"

	// fullSettingsAlignPad 是面板左沿往右让开多少，好让**面板的左沿和齿轮的
	// 图形左沿对齐**。
	//
	// ⚠️ **5 是那枚键自己的留白**：齿轮的点击区是 32 见方（`iconHit`），图形是
	// 22，所以图形在框里居中、左右各留 5（`viewFullTopBar` 里的 `Padding(0, 5)`）。
	// 面板默认贴在**点击区**的左沿上，于是它比图形左沿还左 5——看过去就是
	// 「从按钮底下歪出来一块」（UX 报告 B4 的第三条）。
	//
	// ⚠️ **值从间距表里取**（`space.Px5`）：满屏那几处的间距都在那张表上
	// （票 #267 把三百多处字面量收进了一张表），这一处是同一个刻度上的同一档。
	fullSettingsAlignPad = space.Px5
)

// colorSwatchRadius 是那枚 48×32 的色块的圆角。**16**——它是 32 的一半，
// 所以那一块是**药丸**（胶囊）。
//
// ⚠️ **它不跟全局圆角令牌**：那一块是「用户挑的那个颜色」的样本，形状是
// 固定的；跟着用户的圆角走的话，用户调到 24，色块就从一个药丸变成一个方块，
// 而它旁边那圈描边还在。
const colorSwatchRadius = 16

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

// viewFullSettings 是偏好面板的**内容**。
//
// 面板那个浮层是 `viewFullSettingsPanel`（外观 + 对齐），内容是这里。
func (s *Shell) viewFullSettings(c *ui.Context) {
	prefs := s.app.FullPlayerPrefs()

	// ⚠️ 这几个局部值是**这一帧**开关上的值：用户刚按下的那一下写的是它们，
	// 而 `prefs` 是这一帧开头拍的那一份快照。下面「跟不跟着开关走」那几行判的
	// 是局部值——判快照的话那一行要等下一帧才跟上。
	showLyrics, showSpectrum := prefs.ShowLyrics, prefs.ShowSpectrum
	showCover, blurred := prefs.ShowCover, prefs.ShowBlurredBackground

	ui.Column(c).Width(320).Padding(space.Px16).Gap(space.Px16).Children(func() {
		s.fullSettingsHeader(c)
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

// viewFullSettingsPopover 把偏好面板那个浮层挂在齿轮上。
//
// ⚠️ **自己拼**（`PopoverBase`），不是框架那个 `ui.Popover`——理由见
// `viewFullSettingsPanel`。
func (s *Shell) viewFullSettingsPopover(c *ui.Context, gear ui.Element) {
	ui.PopoverBase(c, gear, &s.full.settings, func(panel ui.Element) {
		s.viewFullSettingsPanel(c, panel, gear)
	})
}

// viewFullSettingsPanel 画偏好面板那个浮层：外观 + 对齐，内容是 `viewFullSettings`。
//
// # ⚠️ **面板自己拼**（`PopoverBase`），不是框架那个 `ui.Popover`
//
// 框架那一版把面板钉在锚点（那枚齿轮键）的**左沿**，给不了「往右让开 5」这个
// 偏移——而那正是「面板的左缘和齿轮对齐」要的那一档：齿轮的点击区是 32 见方、
// 图形是 22（左右各留 5），面板默认贴在点击区的左沿上，于是它比**图形**的左沿
// 还左 5，看过去就是「从按钮底下歪出来一块」（UX 报告 B4）。
//
// 拼出来的面板**走共享出口 `popupPanel`**：底色、描边、圆角、阴影四件一样都不少
// （理由见 popup.go；`popup_test.go` 的 `TestEveryHandMadePopupHasAShadow` 盯着
// 这一条）。
//
// 而「点外面关掉」是框架那个浮层自己管的，`PopoverBase` 一样有
// （`mygo/ui/base.go` 的 `popover`）——换成自己拼并不会丢掉那个手势。
//
// ⚠️ **「Esc 关掉」有条件**：框架把键先给焦点链，它们都不处理才轮到浮层
// （`mygo/ui/input.go` 的 `routeKeys`）——焦点在一个自己吃 Esc 的控件里时，
// 它关不掉（理由见 `playbar.go` 里 `viewTrackError` 那处注释）。
func (s *Shell) viewFullSettingsPanel(c *ui.Context, panel, gear ui.Element) {
	t := c.Theme()
	popupPanel(t, panel.
		AttachTo(gear, ui.AnchorBottomStart, ui.AnchorTopStart).
		// ⚠️ **Margin 的四个值是「上、右、下、左」**：这里给的是面板与齿轮之间
		// 那道缝（框架自己那个浮层也留 4，见 `stylePanel`）。
		Margin(t.Space(1), space.None, space.None, space.None).
		MarginStart(fullSettingsAlignPad).
		// 面板自己有个名字：读屏软件念得出「全屏设置面板」，而无头 Tester 也
		// 靠它找这个盒子（对齐那一条要量它的左沿）。
		Label("全屏设置面板"))
	s.viewFullSettings(c)
}

// fullSettingsHeader 是面板顶上那一行：左边标题，右边一枚「×」。
//
// ⚠️ **标题是「这是什么面板」**：以前面板一打开就是四行开关，用户得自己判断
// 「显示什么」（UX 报告 B4 的第一条）。
//
// ⚠️ **关闭的那一枚是图标键**（`iconClose` 是 `System/close-line.svg`）：面板顶上
// 摆一枚带框的药丸会把标题压住，而「×」是这一类面板的通用记号。
//
// 而**面板本来就能关**（点外面、按 Esc，框架那个浮层自己管），这一枚是给「忘了
// 齿轮在哪」的人留的出口（UX 报告 B4 的第二条：只能再点一次齿轮关掉）。
func (s *Shell) fullSettingsHeader(c *ui.Context) {
	ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
		ui.Text(c, fullSettingsTitle).FontSize(scale.Font(c, scale.Size14)).FontWeight(500)
		if flatIconButton(c, iconClose, "关闭设置", 16).
			Size(24, 24).Padding(space.None, space.Px4).Clicked() {
			s.full.settings = false
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
// ⚠️ 用 `springSwitch`，不是 `ui.Switch`——理由见 switch.go 顶上那一段：框架
// 那一颗的旋钮是 140ms 的**匀出**（不是弹簧），而我们要的是弹一下。设置页上
// 每一个开关用的也正是它——**同一个开关只有一种样子**（同语义的东西用同样的
// 动作，DESIGN.md §五《规则》第 21 条）。
//
// ⚠️ **这一格不弹提示**：偏好面板上这几格的效果**看得见**（频谱出来、虚化背景
// 变糊、歌词出现、封面换一张）——判据见 `switch_hint.go`。
//
// ⚠️ 开关的名字写成「开关：那一行的标题」，不是照抄标题：标题是它**兄弟节点**
// 上的可见文字，两者同名的话无头 Tester 的 `Find` 先找到标题（点它什么也不会
// 发生），读屏也会把同一个名字念两遍。这条约定和设置页的 `pickSwitch` 一致。
func (s *Shell) fullSwitchRow(c *ui.Context, title string, on *bool, save func(bool)) {
	ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(scale.Font(c, scale.Size16)).FontWeight(500)
		if springSwitch(c, on).Label("开关：" + title).Changed() {
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
		ui.Text(c, title).FontSize(scale.Font(c, scale.Size16)).FontWeight(500)
		ui.Row(c).Gap(space.Px8).AlignItems(ui.Center).Children(func() {
			if overridden {
				if flatButton(c, "用默认").FontSize(scale.Font(c, scale.Size12)).
					TextColor(t.TextMuted).Clicked() {
					reset()
				}
			}
			// 药丸：底色先铺成用户挑的那个颜色，再把 ColorWell 的底色/描边让掉，
			// 让里面那块色片和外面同色、看起来就是一整块 48×32 的颜色。
			ui.Box(c).Width(48).Height(32).Radius(colorSwatchRadius).Clip().
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
