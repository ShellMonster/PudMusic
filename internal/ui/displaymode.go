// 设置里的「数据显示样式」：各页怎么读它，以及设置页旁边那张预览。
//
// # 这一格以前是不工作的
//
// 「数据显示样式」三档从进项目起就摆在那儿、能存进设置文件，而**没有任何一页读
// 它**。症状是「改了没有任何页面跟着变」——比「设置没用」更让人火大的是
// 「改了只有搜索页变了」，所以本文件把读它这件事收在一个出口上（`displayView`），
// 由每一页在每一帧调一次，而不是各页自己去 `switch` 那三个字符串。
//
// # 三档的分工
//
//   - 列表：一行一首（`widgets/list` 的 `List`）
//   - 紧凑：还是一行一首，行高更矮（`List` 的紧凑档）
//   - 网格：一行几首（`widgets/list` 的 `Grid`）
//
// 枚举和对照在 `widgets/list/view.go`，那边只有一份来源。这边要做的只是**每帧
// 把它从设置里读出来交给页面**。
//
// # 本地音乐页为什么是另一回事
//
// 见 `localmusic.go` 顶上那段：那一页自己有「表格」这一档（文件大小 / 格式 /
// 创建时间这些列和封面没关系），全局设置管不到它，于是那一页的页内开关改成
// 「表格 / 歌曲」两档，选了「歌曲」之后由这一份设置说了算。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// displayView 是**这一帧**内容页该摆成什么样。
//
// 每帧调：设置随时会变，而这是唯一一处把设置翻译成画法的地方。认不出的档位由
// `list.ViewOf` 退回列表（设置那边也已经退回默认了）。
//
// 🔴 **别缓存**：设置页改了档之后是靠 `applySetting` → `invalidate` → 重画生效的，
// 而缓存下来的话「改了没变」——正是这一票要消灭的那种坏。
func (s *Shell) displayView() list.View {
	return list.ViewOf(s.app.SettingsSnapshot().DisplayMode)
}

// viewDisplayMode 是设置里那一格：三档切换 + 底下一张预览。
//
// 预览摆在**切换控件底下**而不是右边：右边那格已经有一个 300 宽的切换了，而这一
// 页的页面区最窄是 900（`WindowOptions.MinWidth`）减掉侧栏和内边距 ≈ 660——
// 再挤一列预览会把「外观」那一段的每一行都顶到换行。
func (s *Shell) viewDisplayMode(c *ui.Context, st app.Settings, ro bool) {
	ui.Column(c).Key("setting:displayMode").Width(segWidth).Gap(space.Px8).Children(func() {
		s.pickDisplayMode(c, st, ro)
		viewDisplayModePreview(c, list.ViewOf(st.DisplayMode))
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 预览
// ─────────────────────────────────────────────────────────────────────────────

// 预览那三张小图各多大。
//
// ⚠️ **里面一个字的都没有**：全用色块摆形状。理由有两个——
//
//   - 五像素的字读不出来，而读不出来的字摆在那儿只是噪声；
//   - 无头 Tester 的 `Find` / `Click` 认的是**控件的文本**（`settings_tabs_test.go`
//     的 `TestTheGeneralTabStoresEachOfItsChoices` 就在点「网格」）。预览里再画一遍
//     「网格」两个字，那条测试点到的就可能是预览而不是切换控件——而症状是
//     「测试偶尔红」而不是「设置没生效」。
//
// 所以每一张有 `Label`（「预览：网格」这种带前缀的）而没有可见文字：读屏软件
// 念得出它是哪一张，Tester 也认得出，可它和切换控件那几个「网格」撞不上。
const (
	previewW    = 88
	previewH    = 56
	previewGap  = space.Px10
	previewRows = 3 // 列表 / 网格预览里摆几行

	// previewChipRadius 是预览缩略图里那些小色块的圆角。**1**——
	// 那一块只有 6×6 像素，圆角 1 就已经能看出是个小方块而不是纯方了。
	//
	// ⚠️ **它不跟全局圆角令牌**：那不是控件，是**缩略图里画出来的一个假的**
	// ——它画的是「真实的列表行长什么样」，而真实那一行的圆角是 6（上游
	// `rounded-md`）。所以这里的数是「照着画」，不是「跟主题」。
	previewChipRadius = 1
	// previewCoverRadius 是预览里那个封面方块的圆角。**2**——
	// 12×12 的方块，圆角 2。
	//
	// ⚠️ 同上：它是缩略图里画出来的**假封面**，照着上游 `rounded-md`（6）
	// 缩小后的样子，不跟全局圆角令牌。
	previewCoverRadius = 2
)

// viewDisplayModePreview 画那三张缩略图，选中的那张描一圈主色。
//
// ⚠️ **不可点，也不可灰**。它是预览，不是第二个入口：摆一个能点的，用户会问「点它
// 和点上面那个有什么区别」，而答案是「没有区别」——同一件事两个控件，坏了一个另一
// 个还在，用户会以为另一个也坏了。而只读保护（`ro`）灰的是**改不了的东西**：设置
// 来自更新的版本时这一格动不了，预览照样看得见（它一个字都没改）。
func viewDisplayModePreview(c *ui.Context, chosen list.View) {
	t := c.Theme()
	ui.Row(c).Key("setting:displayModePreview").Width(segWidth).
		Gap(previewGap).AlignItems(ui.Start).Children(func() {
		for v := list.View(0); v < list.ViewCount; v++ {
			previewOne(c, t, v, v == chosen)
		}
	})
}

// previewOne 画一张缩略图。
func previewOne(c *ui.Context, t *ui.Theme, v list.View, chosen bool) {
	border := t.Border
	if chosen {
		// 选中的那一张描主色，和设置里其它「当前是这一档」的画法一样
		//（`pickTheme` 给选中的按钮铺主色底）。
		border = t.Accent
	}

	ui.Column(c).Label("预览："+v.String()).
		Width(previewW).Height(previewH).Radius(t.Radius).
		// 描边走 `Border` 的第二格（选中那一档给主色）：描 1 像素在缩略图上
		// 足够认出来，而铺一整块主色会把里面那些色块盖住。
		Border(1, border).Padding(space.Px6).Gap(space.Px3).Children(func() {
		switch v {
		case list.ViewCard:
			previewGrid(c, t)
		case list.ViewCompact:
			previewRowsOf(c, t, previewRows+1, true)
		default:
			previewRowsOf(c, t, previewRows, false)
		}
	})
}

// previewRowsOf 画「一行几首」的那种缩略图：几行，每行一个封面方块加一条字条。
//
// tiny 摆的是**紧凑**那一档：同样的高度里多摆一行，字条也矮一截——这正是两档的
// 区别（`widgets/list/card.go` 的紧凑常量）。
func previewRowsOf(c *ui.Context, t *ui.Theme, n int, tiny bool) {
	barH := float32(3)
	if tiny {
		barH = 2
	}
	ui.Column(c).Grow(1).Gap(space.Px3).Children(func() {
		for range n {
			ui.Row(c).Grow(1).Gap(space.Px3).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Width(6).Height(6).Radius(previewChipRadius).Background(t.SurfaceHover)
				ui.Box(c).Grow(1).Height(barH).Radius(previewChipRadius).Background(t.SurfaceHover)
			})
		}
	})
}

// previewGrid 画网格那一档：三列，每列一个封面方块加一条字条。
//
// 封面的方块画得比列表那一档大——网格本来就是靠封面认歌的（Biu 的 music-card），
// 缩略图上不把这一点画出来的话，三张图看着只差「几行」，而那不是它们真正的区别。
func previewGrid(c *ui.Context, t *ui.Theme) {
	const cols = 3
	ui.Row(c).Grow(1).Gap(space.Px4).Children(func() {
		for range cols {
			ui.Column(c).Grow(1).Gap(space.Px3).Children(func() {
				ui.Box(c).FillWidth().Height(12).Radius(previewCoverRadius).Background(t.SurfaceHover)
				ui.Box(c).FillWidth().Height(3).Radius(previewChipRadius).Background(t.SurfaceHover)
			})
		}
	})
}
