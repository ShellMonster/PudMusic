// 皮肤页（/appearance）：主题方案 + 主题模式 + 颜色 + 封面取色 + 字体 + 圆角 +
// 背景清晰度，一页办完「这个应用长什么样」的全部选择。
//
// # 它和设置页的关系
//
// 这一页是设置页「常规 → 外观」组里搬出来的**那一整块**——搬走之后设置页那一组
// 只剩「数据显示样式」（它讲的是列表密度，不是皮肤），皮肤这一摊集中在这里。
// 顶栏那枚 palette 图标是它的唯一入口（`viewNav` 里 `nav-appearance` 那一枚）；
// **侧栏不摆**（用户拍板：顶栏图标就是入口，侧栏不重复）。
//
// 控件全部复用设置页那几个 picker（`pickThemePreset` / `pickTheme` / `pickColors` /
// `pickSwitch` / `pickFont` / `pickRadius` / `pickBackdropClarity`），它们写的还是
// 同一份 `app.Settings`——两处读到和写出的都是同一份，不会不一致。
//
// # 为什么不是浮层
//
// 用户拍板要**独立页**不要弹窗：弹层太小摆不下 8 张方案卡 + 深浅四档 + 字体 +
// 颜色两块，硬塞进去就是一层又一层的滚动；而「换皮」恰好是用户会来回比着看
// 的场景，值得给它一整页。
//
// # 版式照设置页
//
// `Scroll` 套 `MaxWidth` 居中那一套和设置页同一个形状（理由见 settings_page.go
// 的 `viewSettings`）——两张页长得一样，用户从「设置」点到「皮肤」不觉得换了
// 一个应用。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// appearancePath 是皮肤页的路由。
const appearancePath = "/appearance"

// viewAppearance 是皮肤页本身：标题 + 那一整块外观控件。
//
// ⚠️ **「只读」也要说**：设置来自更新的版本时，这一页的控件和设置页一样全关
// （`ro`），而那句原因得在这一页自己写一遍——用户从顶栏进来，不一定知道还有
// 个设置页在拦着。
func (s *Shell) viewAppearance(c *ui.Context, _ *ui.Route) {
	t := c.Theme()
	st := s.app.SettingsSnapshot()
	ro := s.app.SettingsReadOnly()

	// 控件列宽按设置页同一个出口算（说明列下限 + 内容内宽）。
	s.settings.descFloor = s.settingsDescFloor(c, st)
	s.settings.contentW = s.settingsContentInnerW(c, st)

	scroll := ui.Scroll(c).Key("appearance-scroll").Grow(1).TrackScroll(&s.appearanceScroll)
	scroll.Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(s.settingsContentMaxWAt(c)).
				Padding(space.None, settingsContentPad, settingsContentPad, settingsContentPad).
				Gap(settingsGroupGap).Children(func() {
				ui.Text(c, "皮肤").FontSize(scale.Font(c, scale.PageTitle))

				if ro {
					ui.Text(c, "设置只读：这份设置来自更新的版本，这一版一个字都不会写回去。").
						FontSize(scale.Font(c, scale.Size12)).TextColor(dangerText(t))
				}

				s.settingGroup(c, "主题方案", func() {
					s.pickThemePreset(c, st, ro)
				})
				s.settingGroup(c, "主题", func() {
					s.pickTheme(c, st, ro)
				})
				s.settingGroup(c, "颜色", func() {
					s.pickColors(c, st, ro)
				})
				s.settingGroup(c, "封面取色", func() {
					s.pickSwitch(c, "封面取色", "封面取色", st.CoverAccent, ro,
						func(cur *app.Settings, on bool) { cur.CoverAccent = on })
				})
				s.settingGroup(c, "字体", func() {
					s.pickFont(c, st, ro)
				})
				s.settingGroup(c, "圆角", func() {
					s.pickRadius(c, st, ro)
				})
				s.settingGroup(c, "背景清晰度", func() {
					s.pickBackdropClarity(c, st, ro)
				})
			})
		})
	})
	// 同设置页：滚动条指针压上去深一档的悬停追踪（`trackScrollbar`）——
	// 漏了的话这一页的滚动条永远停在默认色，和设置页不一样。
	s.trackScrollbar(scroll)
}
