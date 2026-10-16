// 设置页：Biu 的 `/settings`（`src/pages/settings/`，本体是 `system-settings.tsx`）。
//
// ## 这一版摆什么、不摆什么
//
// 摆的是「用户改了立刻能看见差别」的那三项：主题、全局圆角、窗口关闭。
// Biu 那一页上的下载目录、代理、音质、字体、播放记录**先不在这里**——不是忘了，
// 是它们要么还没有对应的功能（在线播放、代理、托盘），要么是别的阶段的活儿。
// 摆一堆按了没反应的开关比不摆更糟：用户分不清「还没做」和「按了没用」。
//
// 每一个控件改的都是**盘上已经有的键**，一个都不新造（见
// `internal/app/settings_test.go` 的 `biuKeys`）。设置页是那些键的入口，不是
// 键名的发明地——用户来回切 Biu 和 MyGo 时丢设置，是最难查的一类数据丢失。
//
// 🔴 本文件里的任何 view **不许**调 `c.SetTheme`：主题每帧开头装一次就够，
// 别的页面照样生效（铁律二，见 theme.go）。
package ui

import (
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// settingsPath 是设置页那条路由，和 router.go 里登记的那一条是同一个字符串。
//
// 拿来和 `Route.Match` 比，而不是拿 `routes[6]`：那条表一改顺序，这里跟着错，
// 而且错得很安静——设置页退回占位页，没人看得出来。
const settingsPath = "/settings"

// 控件的宽度。Biu 那一页右边固定 360px（`w-[360px]`），三档切换占 300。
//
// 300 是量出来的，不是挑的：主题那三个按钮里「跟随系统」四个汉字最宽，按钮
// 自己还要留内边距。窄了它会被画成「跟随…」——用户看见半个词，第一反应是
// 这个应用坏了，而不是「这个按钮窄了一点」。
const (
	segWidth       = 300
	radiusWidth    = 360
	radiusReadoutW = 48
)

// closeWindowOptions 是窗口关闭那两档，顺序就是从左到右的顺序。
//
// 值和 Biu 的 `closeWindowOption` 一字不差（`shared/settings/app-settings.ts:3`）。
//
// ⚠️「隐藏到托盘」这一档这一版**只有存储和选择**，托盘实现在 issue #28。
// 页面上要把这件事说出来：一个点了没反应的选项比没有更让人困惑。
var closeWindowOptions = []struct {
	value app.CloseWindowOption
	label string
}{
	{app.CloseWindowHide, "隐藏到托盘"},
	{app.CloseWindowExit, "直接退出"},
}

// ─────────────────────────────────────────────────────────────────────────────
// 路由分派
// ─────────────────────────────────────────────────────────────────────────────

// pageContent 画一页的内容：做了的走自己的画法，还没做的走占位页。
//
// 分派放在本包而不是 router.go 那个 switch 里：那里这一阶段别的分支正在改，
// 而「这一页已经不是占位页了」是界面层自己的事。
func (s *Shell) pageContent(c *ui.Context, r *ui.Route, rt Route) {
	if body := s.builtPage(rt); body != nil {
		body(c, r)
		return
	}
	s.placeholder(c, r, rt)
}

// builtPage 是「这一页已经有内容了」的路由和画法；还没做就返回 nil。
//
// router.go 的 navigate 用它判断该不该弹「这一页还没做」——弹给一个真做了的
// 页面，是在说一件假事。
func (s *Shell) builtPage(rt Route) func(*ui.Context, *ui.Route) {
	if rt.Match == settingsPath {
		return s.viewSettings
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 页面
// ─────────────────────────────────────────────────────────────────────────────

// viewSettings 是设置页本身。
//
// ⚠️ 这里用 Column 而不是 Scroll：MyGo v0.5.1 的 `Scroll` **不约束子元素的
// 宽度**（它是 `coreBox` 加一个滚动标志，宽度按内容算）。实测：右边那些固定
// 宽度的控件会被整排顶到窗口外面去——「深色」「直接退出」点不到，用户看见的
// 是一片空白加半个按钮。等设置项多到装不下的时候再换 Scroll，同时把控件宽度
// 从固定值改成能自己收窄的那种（现在 `settingRow` 右边是写死的宽度）。
func (s *Shell) viewSettings(c *ui.Context, _ *ui.Route) {
	t := c.Theme()
	st := s.app.SettingsSnapshot()
	// 设置来自更新的版本时一个字都不许写回去。与其让用户按了没反应，不如把
	// 控件全关掉，并说清为什么——跟顶栏那三个按钮一个道理。
	ro := s.app.SettingsReadOnly()

	ui.Column(c).Key("page:/settings").Grow(1).Padding(28).Gap(18).Children(func() {
		ui.Text(c, "设置").FontSize(22).Bold()

		if ro {
			ui.Text(c, "设置只读：这份设置来自更新的版本，这一版一个字都不会写回去。").
				FontSize(12).TextColor(t.Danger)
		}

		s.settingSection(c, "外观")
		s.settingRow(c, "主题", "选择浅色或深色主题",
			func() { s.pickTheme(c, st, ro) })
		s.settingRow(c, "圆角", "调整界面控件的圆角大小",
			func() { s.pickRadius(c, st, ro) })

		ui.Divider(c)

		s.settingSection(c, "系统")
		s.settingRow(c, "窗口关闭", "选择窗口关闭时的行为",
			func() { s.pickCloseWindow(c, st, ro) })
		// 托盘还没做（issue #28），这一版只存不生效——得说出来，否则「隐藏到托盘」
		// 点了和没点一样，用户会当成 bug。
		ui.Text(c, "托盘还没做（issue #28），「隐藏到托盘」这一版先只存着。").
			FontSize(12).TextColor(t.TextMuted)
	})
}

// settingSection 是一组设置上面的那个标题。Biu 用的是 `<h2>`（system-settings.tsx
// 的「外观」「播放」「下载」…）。
func (s *Shell) settingSection(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(16).Bold().Padding(0, 0, 8, 0)
}

// settingRow 是设置项的一行：左边标题加一句说明，右边控件。
//
// 排版照 Biu 那一行（`flex items-center justify-between`，左边 `mr-6 space-y-1`
// 的标题和说明，右边固定宽度的控件）。两边的伸缩比例照抄：文字吃掉剩下的
// 宽度，说明换行了控件也不会被挤走。
func (s *Shell) settingRow(c *ui.Context, title, desc string, control func()) {
	t := c.Theme()
	ui.Row(c).FillWidth().Gap(24).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Children(func() {
			ui.Text(c, title).FontSize(14).Bold()
			ui.Text(c, desc).FontSize(12).TextColor(t.TextMuted).Wrap()
		})
		control()
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 三项控件
// ─────────────────────────────────────────────────────────────────────────────

// pickTheme 是主题那三档。
//
// 换完之后 `applyFrameTheme` 下一帧就装上新主题：改档位 → UpdateSettings →
// invalidate → 重画，一帧之内的事。走 `applySetting` 而不是 `SetThemeMode`，
// 是为了和另外两项共用同一条报错提示。
func (s *Shell) pickTheme(c *ui.Context, st app.Settings, ro bool) {
	t := c.Theme()
	ui.Row(c).Key("setting:themeMode").Width(segWidth).Gap(4).Children(func() {
		for _, opt := range themeModes {
			b := ui.Button(c, opt.label).Grow(1).Radius(6).Disabled(ro)
			if opt.mode == st.ThemeMode {
				b = b.Background(t.Accent)
			}
			if b.Clicked() {
				s.applySetting("主题", func(cur *app.Settings) { cur.ThemeMode = opt.mode })
			}
		}
	})
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
		ui.Textf(c, "%.0fpx", v).FontSize(12).Width(radiusReadoutW)
	})
}

// pickCloseWindow 是窗口关闭那两档。照 Biu 用的是横排单选（RadioGroup
// orientation="horizontal"），我们用同一排按钮，选中的那个上主色。
func (s *Shell) pickCloseWindow(c *ui.Context, st app.Settings, ro bool) {
	t := c.Theme()
	ui.Row(c).Key("setting:closeWindowOption").Width(segWidth).Gap(4).Children(func() {
		for _, opt := range closeWindowOptions {
			b := ui.Button(c, opt.label).Grow(1).Radius(6).Disabled(ro)
			if opt.value == st.CloseWindowOption {
				b = b.Background(t.Accent)
			}
			if b.Clicked() {
				s.applySetting("窗口关闭", func(cur *app.Settings) { cur.CloseWindowOption = opt.value })
			}
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────

// applySetting 改一项设置；写不出去就弹一条提示。
//
// **提示不能省**：静默地丢掉是这一页最坏的失败方式——用户以为改了，下次打开
// 又变回去，中间屏幕上什么也没发生过。要报的不是「按了没反应」，是「存不住」：
// 落盘失败时档位在内存里是改成功了的（这一点是对的，别回滚），丢的是磁盘上
// 那一份，用户下次启动看到的才是旧值。
func (s *Shell) applySetting(what string, fn func(*app.Settings)) {
	if _, err := s.app.UpdateSettings(fn); err != nil {
		s.addToast(what + "没存上：" + strings.TrimPrefix(err.Error(), "app: "))
	}
}
