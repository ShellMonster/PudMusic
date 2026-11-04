// 顶栏（原型 `.top`，DESIGN.md §0′.T1）。
//
// 高 60，左右内边距 24，缝 12。从左到右：后退 / 前进（两枚 36 图标键，缝 4）、搜索胶囊
// （440×38，右边一枚「/」键帽，见 `navbar_search.go`）、弹性空白，然后是动态铃铛（登录后才有，
// 有新动态时右上角一个主色点）、命令面板、深浅切换（深色时画太阳、浅色时画月亮）、迷你模式、
// 头像（未登录时是一枚主色小号「登录」键）。
//
// 原型没有的入口已删：打开文件、听歌统计（进了侧栏）、皮肤（并进设置的「外观」）。
// 整条顶栏可拖窗口（控件叠在它上面），联想面板或头像下拉开着时不拖，免得吃掉点击。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

const (
	navBackLabel    = "后退"
	navForwardLabel = "前进"
	// navMinHeight 是顶栏高（原型 `--top-h` 60）。
	navMinHeight = 60
)

// viewNav 是顶栏。
func (s *Shell) viewNav(c *ui.Context) {
	back := func() { s.clearToasts(); s.crash = nil }
	bar := c.TitleBar()

	nav := ui.Row(c).Key("nav").Height(max(bar.Height, tok.TopH)).
		Padding(space.None, bar.Right+space.Px24, space.None, space.Px24).
		Gap(space.Px12).AlignItems(ui.Center)
	if !s.searchSuggest && !s.avatarMenuOpen {
		nav = nav.DragWindow()
	}
	nav.Children(func() {
		ui.Row(c).Key("nav-arrows").Gap(space.Px4).AlignItems(ui.Center).Children(func() {
			s.viewNavBack(c, back)
			s.viewNavForward(c, back)
		})
		s.viewSearchBox(c)
		ui.Column(c).Grow(1).DragWindow()
		s.viewDynamicDot(c)
		c.Key("nav-palette")
		if s.withTip(c, pud.IconBtn(c, "nav-palette-btn", "cmd", "命令面板（⌘K）", pud.IconOpts{}), "命令面板").Clicked() {
			s.toggleCommandPalette()
		}
		themeIcon, themeTip := "moon", "切换到深色"
		if c.Theme().Dark {
			themeIcon, themeTip = "sun", "切换到浅色"
		}
		if s.withTip(c, pud.IconBtn(c, "nav-theme-btn", themeIcon, themeTip, pud.IconOpts{}), themeTip).Clicked() {
			s.toggleDarkLight(c.Theme().Dark)
		}
		if s.withTip(c, pud.IconBtn(c, "nav-mini-btn", "mini", "迷你", pud.IconOpts{}), "迷你播放器").Clicked() {
			s.toggleMiniPlayer()
		}
		s.viewAvatar(c)
	})
}

// toggleDarkLight 是顶栏上的深浅切换：把设置里的主题模式落成和现在相反的那一档
// （跟随系统的人点一下也就固定成相反的那一档，和原型 `toggleTheme` 一样）。
func (s *Shell) toggleDarkLight(nowDark bool) {
	mode := app.ThemeModeDark
	if nowDark {
		mode = app.ThemeModeLight
	}
	if _, err := s.app.UpdateSettings(func(st *app.Settings) { st.ThemeMode = mode }); err != nil {
		s.addToast("主题存不住："+err.Error(), notice.Danger)
	}
	s.wake.invalidate()
}

// viewNavBack 是后退键（原型 `.icon-btn` + `left`，没有历史时 35% 不透明）。
//
// 首页上没有历史时，它退回「发现总览」那一层（首页自己的二级状态，见 `viewHomeBack`）。
func (s *Shell) viewNavBack(c *ui.Context, back func()) {
	if s.router.Path() == homePath && !s.router.CanGoBack() {
		s.viewHomeBack(c, back)
		return
	}
	b := pud.IconBtn(c, "nav-back", "left", navBackLabel, pud.IconOpts{}).Disabled(!s.router.CanGoBack())
	if s.withTip(c, b, navBackLabel).Clicked() {
		back()
		s.router.Back()
	}
}

// 首页「返回发现总览」那一枚的提示。
const (
	navHomeBackTip     = "返回发现总览"
	navHomeBackTipDone = "已经在发现总览"
)

// viewHomeBack 是首页上的后退键：不能再退时灰掉，能退时回到「发现总览」。
func (s *Shell) viewHomeBack(c *ui.Context, back func()) {
	canGo := s.home != nil && !s.home.overview
	tip := navHomeBackTip
	if !canGo {
		tip = navHomeBackTipDone
	}
	b := pud.IconBtn(c, "nav-home-back", "left", navBackLabel, pud.IconOpts{}).Disabled(!canGo)
	if s.withTip(c, b, tip).Clicked() {
		back()
		s.backToHomeOverview()
	}
}

// viewNavForward 是前进键。
func (s *Shell) viewNavForward(c *ui.Context, back func()) {
	b := pud.IconBtn(c, "nav-forward", "right", navForwardLabel, pud.IconOpts{}).Disabled(!s.router.CanGoForward())
	if s.withTip(c, b, navForwardLabel).Clicked() {
		back()
		s.router.Forward()
	}
}
