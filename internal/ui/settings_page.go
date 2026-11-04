// 设置页（`/settings`）：原型 `PAGES.settings` + `SETTINGS`，CSS 在 index.html 491–533 行。
//
// 结构：页头（Eyebrow `Preferences` + 「设置」）→ 左边 180 宽的分类导航（常规 / 外观 / 菜单 /
// 快捷键 / 代理 / 关于）+ 缝 48 + 右边那一个 Tab 的分组卡。每个 Tab 在自己的文件里：
//
//	settings_tab_general.go    常规：播放 / 系统 / 下载
//	settings_tab_appearance.go 外观：深浅模式 / 颜色与字体 / 数据显示样式
//	settings_tab_menu.go       菜单：侧栏入口 / 收藏夹显示
//	settings_tab_shortcut.go   快捷键：全局开关 + 三列表
//	settings_tab_proxy.go      代理：三选一 + 自定义表单
//	settings_tab_about.go      关于：版本卡 + 开源许可 / 诊断信息
//
// 每一个控件改的都是**盘上已经有的键**，一个都不新造（`fontScale` 是这一版新增的，
// 老文件缺这个键时落到标准）；快捷键改的是 `shortcuts.json`。
//
// 🔴 本文件里的任何 view **不许**调 `c.SetTheme`：主题只在 `applyFrameTheme` 里装。
package ui

import (
	"strings"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/leading"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// settingsPath 是设置页那条路由，和 router.go 里登记的那一条是同一个字符串。
const settingsPath = "/settings"

// pageMaxW 是原型 `.page` 的最大宽。
const pageMaxW = 1280

// goToDownloadDirSetting 去设置页的「下载目录」那一项：切到常规 Tab、指着那一行
// （标题是主色）、让它滚进视野。从下载记录页的「还没有下载目录」跳过来用。
func (s *Shell) goToDownloadDirSetting() {
	s.settings.tab = settingsTabGeneral
	s.settings.focus = settingsFocusDownload
	s.settings.focusScroll = true
	s.navigate(settingsPath)
}

// goToSettingsTab 去设置页的某一个 Tab（菜单「关于」走它）。
func (s *Shell) goToSettingsTab(tab int) {
	s.settings.tab = tab
	s.navigate(settingsPath)
}

// settingsPageState 是设置页自己那一摊，**只由 view 在主线程上读写**。
type settingsPageState struct {
	// tab 是切在第几个 Tab。建在 Shell 上而不是局部变量：即时模式每一帧都重画一遍。
	tab int

	// eqOpen / eqFullOpen / eqName 是均衡器自定义面板的状态（eq_panel.go，满屏播放器里用）。
	eqOpen     bool
	eqFullOpen bool
	eqName     string

	// scroll 是这一页的滚动位置；换 Tab 时回到顶上。
	scroll ui.ScrollState
	// shownTab 是上一帧画的是第几个 Tab，用它认出「刚按了 Tab」。
	shownTab int

	// focus 是「从别处跳过来时要指的那一项」。它不是路由：用户要的是那一项。
	focus settingsFocus
	// focusScroll 记「把指着的那一行滚进视野」这件事还没做（只做一次，否则用户自己
	// 滚走了又被拉回来）。
	focusScroll bool

	// proxy 是代理自定义表单里还没保存的字（保存并测试才落盘）。
	proxy proxyForm

	// fontList 是外观 Tab 里字体列表（虚拟列表）的状态。
	fontList ui.ListState
}

// settingsFocus 是「从别处跳过来时要指的那一项」的身份。
type settingsFocus string

const (
	settingsFocusNone     settingsFocus = ""
	settingsFocusDownload settingsFocus = "downloadPath"
)

// 六个 Tab。顺序就是从上到下的顺序。
const (
	settingsTabGeneral = iota
	settingsTabAppearance
	settingsTabMenu
	settingsTabShortcut
	settingsTabProxy
	settingsTabAbout
)

// settingsTabs 是左边导航：名字 + 图标（原型 `tabs` 的第 2、3 项）。
var settingsTabs = []struct{ label, icon string }{
	{"常规", "settings"},
	{"外观", "palette"},
	{"菜单", "menu"},
	{"快捷键", "keyboard"},
	{"代理", "globe"},
	{"关于", "info"},
}

// viewSettings 是设置页本身。
//
// ⚠️ 外面这一层是 `Scroll`，里面那一层 `Column` 必须 `FillWidth()`：Scroll 不约束子元素的
// 宽度。收在中间靠 `Center` + `MaxWidth`（原型 `.page { max-width:1280px; margin:0 auto }`）。
func (s *Shell) viewSettings(c *ui.Context, _ *ui.Route) {
	p := tok.Of(c.Theme())
	st := s.app.SettingsSnapshot()
	ro := s.app.SettingsReadOnly()
	// 页边距和标题字号按**窗口宽**走（原型的媒体查询和 `clamp(32px, 3.4vw, 48px)` 都是视口宽）。
	winW, _ := c.Size()
	padX := space.PageHorizontalForWindow(winW)

	scroll := ui.Scroll(c).Key("settings-scroll").Grow(1).TrackScroll(&s.settings.scroll)
	scroll.Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(pageMaxW).
				Padding(space.PageTop, padX, space.PageBottomForWindow(winW), padX).
				Transition(motion.PageIn).Children(func() {
				pud.PageHead(c, "Preferences", "设置", "", winW, nil)

				if ro {
					// 设置来自更新的版本时一个字都不许写回去：控件全关，并说清为什么。
					ui.Text(c, "设置只读：这份设置来自更新的版本，这一版一个字都不会写回去。").
						FontSize(scale.Font(c, scale.Size12)).TextColor(tok.Of(c.Theme()).Err).
						Margin(space.Px8, space.None, space.None, space.None)
				}

				// 原型 `.set-layout`：两列（180 + 缝 48）；窗口 ≤ 1024 时一列、缝 24，分类导航横排（gap 8）。
				stacked := winW <= 1024
				var layout ui.Element
				if stacked {
					layout = ui.Column(c).Key("set-layout").FillWidth().Gap(space.Px24)
				} else {
					layout = ui.Row(c).Key("set-layout").FillWidth().Gap(setLayoutGap).AlignItems(ui.Start)
				}
				layout.Margin(space.Px24, space.None, space.None, space.None).Children(func() {
					s.settingsNav(c, p, stacked)
					ui.Column(c).Key("set-body").Grow(1).Shrink(1).MinWidth(0).Children(func() {
						switch s.settings.tab {
						case settingsTabGeneral:
							s.settingsGeneral(c, st, ro)
						case settingsTabAppearance:
							s.settingsAppearance(c, st, ro)
						case settingsTabMenu:
							s.settingsMenu(c, st, ro)
						case settingsTabShortcut:
							s.settingsShortcuts(c)
						case settingsTabProxy:
							s.settingsProxy(c, st, ro)
						case settingsTabAbout:
							s.settingsAbout(c, st, ro)
						}
					})
				})
			})
		})
	})
	s.trackScrollbar(scroll)

	// 「回到顶部」那一枚（原型 `.btt`）：滚过 400 像素才出现。这一页滚的是整页（外面那个
	// `ui.Scroll`），位置就是那一个 `ScrollState`。
	list.BackToTop(c, s.settings.scroll.Y, func() { s.settings.scroll.Y = 0 })
}

// settingsNav 是左边的分类导航（`.set-nav`）：宽 180、项间缝 2；每项上下 10 左右 12、
// 圆角 8、次要色字、图标 16 隔 10；悬停 hover 底 + 正文色；当前项 surface-2 底 + 正文色 600。
func (s *Shell) settingsNav(c *ui.Context, p tok.Palette, stacked bool) {
	var nav ui.Element
	if stacked {
		nav = ui.Row(c).Key("set-nav").Gap(space.Px8)
	} else {
		nav = ui.Column(c).Key("set-nav").Width(setNavW).Shrink(0).Gap(space.Px2)
	}
	nav.Children(func() {
		for i, tb := range settingsTabs {
			on := s.settings.tab == i
			b := ui.ButtonBase(c).Key("set-tab:"+tb.label).Label(tb.label).FillWidth().
				Padding(space.Px10, space.Px12).Gap(space.Px10).AlignItems(ui.Center).Justify(ui.Start).Radius(tok.R8)
			fg := p.Text2
			switch {
			case on:
				b, fg = b.Background(p.Surface2), p.Text
			case b.Hovered():
				b, fg = b.Background(p.Hover), p.Text
			}
			b = b.Transition(motion.Fade)
			if !stacked {
				b = b.FillWidth()
			}
			b.Children(func() {
				ui.Icon(c, pud.Icon(tb.icon)).FontSize(tok.IconSm).TextColor(fg)
				t := ui.Text(c, tb.label).FontSize(scale.Font(c, scale.Size14)).LineHeight(leading.Body).TextColor(fg).SingleLine()
				if on {
					t.FontWeight(600)
				}
			})
			if b.Clicked() {
				s.settings.tab = i
			}
		}
	})
}

// syncSettings 是设置页每帧的副作用：切走就停录、换 Tab 就回到顶上。
//
// ⚠️ 它们不在 `viewSettings` 里：设置页在满屏开着的时候不画（票 #161），不挪出来的话
// 「正在录快捷键」那一格会一直记着上一次录到一半的东西。
func (s *Shell) syncSettings(c *ui.Context) {
	// 切走就停录：焦点在控件没建出来时会一直等下去，切回来时第一下键就被录进去了。
	if s.settings.tab != settingsTabShortcut {
		s.recording = shortcutSlot{}
		s.rejected = shortcutSlot{}
	} else {
		// 满屏开着时设置页不画，而 `viewShortcuts` 的兜底是「这一帧没画录制格就把正在录的
		// 清掉」——这里置上，免得正在录的被静默取消。
		s.recorderShown = true
	}

	// 按 Tab 时回到顶上。
	if last, tab := s.settings.shownTab, s.settings.tab; last != tab {
		s.settings.shownTab, s.settings.scroll.Y = tab, 0
		// 指着的那一项只在它所在的那一格：换走就没东西可指了。
		if s.settings.focus != settingsFocusNone && tab != settingsTabGeneral {
			s.settings.focus = settingsFocusNone
		}
	}
}

// applySetting 改一项设置；写不出去就弹一条提示。
//
// 提示不能省：静默地丢掉是这一页最坏的失败方式——用户以为改了，下次打开又变回去。
// 落盘失败时档位在内存里是改成功了的（别回滚），丢的是磁盘上那一份。
func (s *Shell) applySetting(what string, fn func(*app.Settings)) {
	s.applySettingOK(what, fn)
}

// applySettingOK 是 applySetting 的「写成功了没有」那一版：开关要在写成功之后才说
// 「开了它会怎样」，写失败时已经弹了「没存上」，再说一句效果就是两句话打架。
func (s *Shell) applySettingOK(what string, fn func(*app.Settings)) bool {
	if _, err := s.app.UpdateSettings(fn); err != nil {
		s.addToast(what+"没存上："+strings.TrimPrefix(err.Error(), "app: "), notice.Danger)
		return false
	}
	// 设置变了，菜单上的勾选态要跟着变（菜单是系统画的，不在窗口的画面里）。
	s.refreshMenuChecked(s.app.Snapshot())
	return true
}

// trimAppErr 去掉应用层错误前面那个「app: 」包名，剩下的是说给用户看的中文。
func trimAppErr(err error) string { return strings.TrimPrefix(err.Error(), "app: ") }
