// 侧栏（原型 `.side`，DESIGN.md §0′.T1）：品牌 + 导航 + 收藏夹。
//
// 全高、宽固定 232，收起后 72（设置里的「收起侧栏」开关，或窗口宽 ≤ 1024 时自动收起）。
// 底色 `side`，右缘一条实色 `line`。上面是品牌区，中间一块可滚的内容（导航 + 收藏夹），
// **没有**拖宽把手、没有底部折叠键——那两样原型里没有，入口已删。
//
// 导航项（`.nav-item`）：高 40、左右 12、缝 12、圆角 8、图标 18；平时次要色，悬停 `hover`
// 底 + 正文色，选中 `surface-2` 底 + 正文色 600 字 + 图标 `accent-text` + 左边 3 宽的
// `accent` 竖条（上下各缩 10，圆角 2）。「下载记录」右边挂着进行中的任务数（等宽 11、
// text-3）。收起后只剩图标，居中。
//
// 登录态决定哪几项出现：「我的关注 / 稍后再看 / 历史记录」未登录时整项不摆
// （原型 `n.login`），也不再有「登录后可见」那句提示。设置里「菜单」页关掉的入口
// 照旧隐藏（`HiddenMenuKeys`）。
package ui

import (
	"runtime"
	"slices"
	"strconv"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/service/download"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

const (
	// sideIconSize 是导航图标的边长（原型 `.nav-item .od-icon { 18px }`）。
	sideIconSize = tok.Icon
	// sideIconCell 是收起后收藏夹行 / 导航行的最小命中高度（原型 `.fav-item` 收起 44）。
	sideIconCell = 44
	// sideBarInset 是选中项左边那条竖条上下各缩多少（原型 `top: 10px; bottom: 10px`）。
	sideBarInset = 10
)

// sideCollapsedAt 报侧栏这一帧是不是收起的：设置里的开关，或窗口宽 ≤ 1024
// （原型 `@media (max-width: 1024px) { .win { --side-w: 72px } }`）。
func sideCollapsedAt(st app.Settings, winW float32) bool {
	return st.SideMenuCollapsed || winW <= tok.AutoCollapseBelow
}

// sideTargetWidth 是**只看设置**时侧栏该多宽（232 / 72）。画的时候还要看窗口宽，
// 见 `sideCollapsedAt`；这个只给「还没画过侧栏」时的兜底用。
func sideTargetWidth(st app.Settings) float64 {
	if st.SideMenuCollapsed {
		return float64(tok.SideCollapsedW)
	}
	return float64(tok.SideW)
}

// sideDrawnWidth 是**这一帧侧栏实际占多宽**（`viewSide` 记下的那个数）。
// 页面用它算内容区宽；侧栏没画（被替掉了）时退回设置里的目标宽。
func (s *Shell) sideDrawnWidth(st app.Settings) float32 {
	if s.sideDrawnW > 0 {
		return s.sideDrawnW
	}
	return float32(sideTargetWidth(st))
}

// viewSide 是左侧栏。
//
// ⚠️ 必须自己拉满高：外层 Row 默认把子元素**居中**跨轴摆放，不写 `AlignSelf(Stretch)`
// 的话这一栏会按内容高度浮在中间。
func (s *Shell) viewSide(c *ui.Context) {
	st := s.app.SettingsSnapshot()
	p := tok.Of(c.Theme())
	winW, _ := c.Size()
	collapsed := sideCollapsedAt(st, winW)
	w := tok.SideW
	if collapsed {
		w = tok.SideCollapsedW
	}
	// 🔴 记下这一帧真正参与布局的宽（内容区、横幅、设置页拿它算宽），理由见
	// `sideDrawnWidth`。`underlay.go` 每帧在建侧栏之前把它归零。
	s.sideDrawnW = w

	side := ui.Column(c).Key("side")
	// 🔴 **方向键的入口在这一层**（票 #245 的 E4）：焦点在侧栏里的任何一处按 ↓，
	// 就把焦点交给内容区。绑在容器上是因为容器是焦点链上的祖先。
	side.Bind(&s.sideHandle)
	if side.Shortcut(0, ui.KeyDown) {
		s.focusContent()
	}
	side.Width(w).AlignSelf(ui.Stretch).Background(p.Side).
		BorderWidth(0, 1, 0, 0).BorderColor(p.Line).
		Children(func() {
			s.viewSideBrand(c, collapsed)
			// 导航和收藏夹一起滚（原型 `.side-scroll`：左右 12、下 16）。
			sideScroll := ui.Scroll(c).Key("side-scroll").Grow(1).FillWidth()
			sideScroll.Children(func() {
				ui.Column(c).FillWidth().Padding(space.None, space.Px12, space.Px16, space.Px12).Children(func() {
					s.viewSideMenu(c, st, collapsed)
					s.viewFavorites(c, st, collapsed)
				})
			})
			s.trackScrollbar(sideScroll)
		})
}

// viewSideBrand 是左上角品牌区（原型 `.traffic` + `.brand`）。
//
// macOS 上红黄绿是系统画的，浮在侧栏左上角（`TrafficLightPosition` 在
// `shell_window.go`，x=18、y=18，和原型 `.traffic` 的 18/18 对上）；它们占
// 44 高，品牌区在它们下面（原型 `.brand` 上 16、左右 20、下 20）。其它平台没有
// 这一格，品牌区直接从上面开始。
//
// 唱片标志 32×32（原型 `brandMark`）+「布丁音乐」宋体 700 20 号、字距 0.04em +
// `PUDMUSIC` 等宽 11 号 text-3、字距 0.18em。收起后只剩标志，居中。
func (s *Shell) viewSideBrand(c *ui.Context, collapsed bool) {
	p := tok.Of(c.Theme())
	top := s.sideBrandTopPad() + space.Px16
	row := ui.Row(c).Key("side-brand").FillWidth().Gap(space.Px12).AlignItems(ui.Center).
		Padding(top, space.Px20, space.Px20, space.Px20)
	if collapsed {
		row = row.Padding(top, space.None, space.Px20, space.None).Justify(ui.Center)
	}
	row.Children(func() {
		ui.Box(c).Size(32, 32).Shrink(0).Draw(func(pp *ui.Painter, r ui.Rect) {
			circle := func(inset, radius float32, col ui.Color) {
				pp.Fill(ui.Rect{X: r.X + inset, Y: r.Y + inset, W: 2 * radius, H: 2 * radius}, col, radius)
			}
			ring := func(inset, radius float32, col ui.Color) {
				pp.Stroke(ui.Rect{X: r.X + inset, Y: r.Y + inset, W: 2 * radius, H: 2 * radius}, col, radius, 1)
			}
			circle(1, 15, p.Text)
			ring(4, 12, p.Bg.Alpha(0.35))
			ring(6.5, 9.5, p.Bg.Alpha(0.35))
			circle(10, 6, p.Accent)
			circle(14.4, 1.6, p.Text)
		})
		if collapsed {
			return
		}
		ui.Column(c).Children(func() {
			typo.Display(ui.Text(c, appName), scale.Font(c, scale.Size20)).LetterSpacing(0.8).SingleLine()
			typo.Mono(ui.Text(c, "PUDMUSIC")).FontSize(scale.Size11).LetterSpacing(2).
				TextColor(p.Text3).SingleLine()
		})
	})
}

// sideBrandTopPad 是品牌区上面给 macOS 红黄绿让的那一格（原型 `.traffic` 高 44）；
// 全屏或非 macOS 没有红黄绿，不让。
func (s *Shell) sideBrandTopPad() float32 {
	if runtime.GOOS == "darwin" && !s.fullScreen {
		return 44
	}
	return 0
}

// toggleSidebar 翻侧栏的折叠状态。
//
// ⚠️ **翻的是设置里那个开关，不是这一帧画出来的那一档**：拖动的时候视觉档
// 可能和设置不一致（`sideCollapsedVisual`），而翻的永远是设置（上游的
// `onToggleCollapsed` 也是这么写的）。
//
// ⚠️ **菜单里那一项也走这里**（`menu.go` 的「显示/隐藏侧栏」）——所以两处
// 翻的是同一份状态，而菜单上那个勾选态跟着设置走（`refreshMenuChecked`）。
func (s *Shell) toggleSidebar() {
	st := s.app.SettingsSnapshot()
	if _, err := s.app.UpdateSettings(func(cur *app.Settings) {
		cur.SideMenuCollapsed = !st.SideMenuCollapsed
	}); err != nil {
		s.addToast("折叠状态存不住："+err.Error(), notice.Danger)
	}
	s.refreshMenuChecked(s.app.Snapshot())
	s.wake.invalidate()
}

// runningDownloads 是进行中（正在下 + 排队）的下载任务数，挂在「下载记录」右边。
func (s *Shell) runningDownloads() int {
	n := 0
	for _, d := range s.app.DownloadSnapshotList().Rows {
		if d.Status == download.StatusRunning || d.Status == download.StatusQueued {
			n++
		}
	}
	return n
}

// viewSideMenu 是导航那一组（原型 `.nav-list`，缝 2）。
//
// 图标配在路由表里（`router.go` 的 `Route.Icon`）。折叠态每一项是一枚居中的图标，
// 悬停给提示（`sideItemTip`），无障碍名字是菜单名。
func (s *Shell) viewSideMenu(c *ui.Context, st app.Settings, collapsed bool) {
	cur := s.router.Path()
	loggedIn := s.app.LoggedIn()
	// first 是「这一帧第一个画出来的菜单项」——它登记成「焦点回侧栏时落回的那一项」。
	first := true
	running := s.runningDownloads()

	ui.Column(c).Key("side-menu").FillWidth().Gap(space.Px2).Children(func() {
		for _, rt := range routes {
			// 设置页「菜单」里藏起来的那几项不摆。
			if !rt.InMenu || slices.Contains(st.HiddenMenuKeys, rt.Match) {
				continue
			}
			// 未登录时要登录才有内容的入口整项不摆（原型 `!n.login || S.loggedIn`）。
			if rt.HideInMenuWhenLoggedOut && !loggedIn {
				continue
			}
			badge := 0
			if rt.Match == downloadListPath {
				badge = running
			}
			item := s.sideMenuItem(c, rt, rt.Match == cur, collapsed, badge)
			if item.Clicked() {
				s.navigate(rt.Match)
			}
			// 🔴 只有第一项登记成「焦点回侧栏时落回的那一项」：登记是「一个句柄绑一个
			// 控件」，全登记的话框架当场 panic。
			if first {
				first = false
				s.sideFocusItem(item)
			}
		}
	})
}

// sideMenuItem 是导航里的一项（原型 `.nav-item`）。badge > 0 时右边挂一个计数。
func (s *Shell) sideMenuItem(c *ui.Context, rt Route, selected, collapsed bool, badge int) ui.Element {
	p := tok.Of(c.Theme())
	b := ui.ButtonBase(c).Key("side-item:" + rt.Match).FillWidth().Height(tok.NavRowH).
		Radius(tok.R8).AlignItems(ui.Center).Shrink(0)
	if collapsed {
		b = b.Label(rt.Title).Justify(ui.Center)
	} else {
		b = b.PaddingX(space.Px12).Gap(space.Px12)
	}
	fg := p.Text2
	switch {
	case selected:
		b = b.Background(p.Surface2)
		fg = p.Text
	case b.Hovered():
		b = b.Background(p.Hover)
		fg = p.Text
	}
	b.Children(func() {
		if selected {
			ui.Box(c).Absolute().Left(0).Top(sideBarInset).Bottom(sideBarInset).Width(3).
				Radius(tok.R2).Background(p.Accent)
		}
		iconCol := fg
		if selected {
			iconCol = p.AccentText
		}
		ui.Icon(c, icon(rt.Icon)).FontSize(sideIconSize).TextColor(iconCol)
		if collapsed {
			return
		}
		name := ui.Text(c, rt.Title).FontSize(scale.Font(c, scale.Size14)).TextColor(fg).SingleLine().Grow(1)
		if selected {
			name.FontWeight(600)
		}
		if badge > 0 {
			typo.Mono(ui.Text(c, strconv.Itoa(badge))).FontSize(scale.Size11).TextColor(p.Text3).SingleLine()
		}
	})
	if collapsed {
		s.sideItemTip(c, b, rt.Title)
	}
	return b
}

// sideItemTip 是折叠态侧栏那一项的悬停提示。
//
// # 位置照上游：贴在那一项的**右边**
//
// 上游折叠态那一项外面套的是 `<Tooltip content={title} placement="right">`
// （`biu/src/components/menu/menu-item.tsx:78`）。框架自带的提示摆在**上面**，
// 而折叠态的侧栏贴着窗口左边——摆在上面的话提示会盖住上面那几项，摆在左边
// 又被窗口边钳住。所以走 `tipAt` 的自定义位置那一支。
//
// # 时延也是上游那一档
//
// 那一处还写了 `closeDelay={0}`：指针离开这一项，提示立刻收。菜单是用户顺着
// 一列扫的，晚 500ms 收会压在下一项上。
func (s *Shell) sideItemTip(c *ui.Context, anchor ui.Element, text string) {
	s.tipAt(c, anchor, text, tipMenu, func(panel, anchor ui.Element, t *ui.Theme) {
		panel.AttachTo(anchor, ui.AnchorRight, ui.AnchorLeft).MarginStart(space.Px6)
	}, true)
}

// sideFocusable 是侧栏里那些「鼠标点得动、键盘走不到」的元素**统一的出口**。
//
// # 为什么它们要能进键盘焦点
//
// `Element.Clicked()` 只设 `flagClickable`（`mygo/ui/input.go:1042`）——
// **不设 `flagFocusable`**；而 Tab 只走可聚焦的元素（`mygo/ui/input.go` 的
// `focusChain`）。于是那一行鼠标点得动、键盘却走不到：Tab 跳过它，回车也
// 按不到它。
//
// 报告（BMG-246 的 `audit-side-settings.md` 的 (b) 一节「键盘那一路」）把这一条
// 列成**四个断口**的头一条：**侧栏的收藏夹列表键盘完全走不到**（展开态那一列
// 和折叠态那层浮层里的行都是）。
//
// # ⚠️ **焦点环是框架画的那一圈**
//
// `.Focusable()` 之后框架给这个元素画焦点环（`mygo/ui/paint.go:334`）——
// 和侧栏菜单项、顶栏那几枚图标键**同一档**，不另立一套。
//
// ⚠️ **框架那一圈画在元素外面 3 像素**（`mygo/ui/paint.go` 的 `FocusRing`）：
// 收藏夹那一行左右各有 8 像素的留白，画在外面压不到字上。
//
// # ⚠️ **可聚焦之后，回车 / 空格就能按下去了**
//
// 框架给「有焦点而且可点」的元素补上那一下（`mygo/ui/input.go:741`），
// 所以这一行不但是「键盘走得到」，也是「键盘按得动」。
func sideFocusable(e ui.Element) ui.Element {
	return e.Focusable()
}

// collectionPath 是一个收藏夹在侧栏里点进去的路由。
//
// ⚠️ 前缀用 `collectionPathPrefix`（collectionpage.go 里那一处）：那边要从同
// 一个前缀上切出 id，两边各写一遍字符串的话，改了一边就会变成「侧栏点进去
// 404」。
func collectionPath(id int64) string {
	return collectionPathPrefix + strconv.FormatInt(id, 10)
}
