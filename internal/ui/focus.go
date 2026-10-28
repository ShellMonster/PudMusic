// 焦点交接：键盘怎么从侧栏走进内容区、又怎么走回来（票 #245 的 E4）。
//
// # 断在哪里
//
// 列表**本身**是能键盘走的：点一行之后连按 ↓，焦点环落在第几行（UX 报告
// §E4 说这一条是好的，别改）。断的是「**进内容区**」这一段——Tab 能走到侧栏，
// 可方向键停在侧栏的按钮上时什么也不发生：按钮不吃方向键，而内容区那一块
// 在焦点链之外，方向键根本到不了它（框架的快捷键按**焦点链**派发，
// `mygo/ui/input.go` 的 `routeKey`）。
//
// # 这一层做两件事
//
//  1. 侧栏那一项拿到焦点时按 ↓ → 把焦点交给内容区里当前那一个列表。
//     （「当前那一个」是列表自己每帧登记的，见 `widgets/list` 的 `focus.go`。）
//  2. 内容区的列表在**顶上一行**时按 ↑ → 把焦点交回侧栏的第一项。
//
// # 为什么「回侧栏的第一项」，而不是「回刚才那一项」
//
// 侧栏按 ↓ 走进内容区、内容区按 ↑ 回到侧栏，是一条**竖直的线**；侧栏是竖着
// 排的，而内容区在它右边。回到「刚才那一项」要跨帧记住那个元素，而侧栏的第
// 一项是稳定的、每次都一样——**「焦点顺序对」靠的是这个稳定**，不靠记忆。
//
// # 「进内容区」为什么是**请求**而不是当场兑现
//
// 请求是侧栏在 build 的时候收到的，而内容区**在侧栏之后才画**——当场兑现的
// 话，内容区里还没有登记任何列表，焦点给谁？于是记下来，等这一遍画完了再
// 兑现（见 `applyContentFocus`）。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// localTableFocus 是「本地音乐页的表格那一档」的焦点目标。
//
// ⚠️ **它自己管游标**（`sel` 指向本地音乐页那个 `p.sel`），而框架的焦点句柄
// 是表格元素自己的（`ui.ListState.Handle`）——两者不是同一个 `ListState`，所以
// 不能直接用 `list.HandleFocus`。
type localTableFocus struct {
	h   *ui.Handle
	sel *int
}

// TakeFocus 把键盘焦点接到这张表格上，并把游标摆到第一行。
func (f localTableFocus) TakeFocus() {
	if f.sel != nil {
		*f.sel = 0
	}
	if f.h != nil {
		f.h.Focus()
	}
}

// focusNotRegistered 是「自己画了列表、但那一张**不是内容区**的列表」的文件。
//
// ⚠️ **这是一份「例外」清单**：它列的是**不该登记**的那些，剩下的（凡是自己画了
// 列表、又不在这份清单里的文件）**都必须登记**。
//
// ⚠️ **为什么不能反过来列**（「该登记的那些」）：反过来列的话，**新增一个页面而
// 忘了登记**，它不在清单里 → 没人管 → 那一页侧栏按 ↓ 进不去内容区。
// 而「漏登记」的症状是「有的页面进得去、有的进不去」——用户只以为这个应用有时候
// 键盘不好使。**评审已经在这上面抓到过两回**（我的关注 / 用户动态 / UP 主空间那
// 几页，还有本地音乐页的表格——它是默认那一档，而清单当时正正好漏着它）。
var focusNotRegistered = map[string]string{
	"sidebar_favorites.go": "侧栏里的收藏夹（在侧栏里，不在内容区）",
	"queue_drawer.go":      "右下角的播放列表抽屉",
	"partdrawer.go":        "「分 P」那个抽屉",
	"folderselect.go":      "「放进哪些收藏夹」那个弹窗",
	"lyricssearch.go":      "歌词搜索那个弹窗",
	"fullplayer.go":        "满屏播放器",
	// ⚠️ **下载记录页的表格还没有游标**（`ui.ListState.Selected` 没装），所以方向键
	// 在它上面**挪不动**——「内容区里能走位的列表」暂时还不包括它。
	// 而它**确实**是内容区的列表，所以这一条要跟着那张表格装上游标一起撤掉。
	"downloadlist.go": "下载记录页的表格还没有游标（`Selected` 没装），方向键挪不动它",
}

// focusTarget 是「能把键盘焦点接过去」的东西。
//
// 界面层自己画的那几张列表（不是 `widgets/list` 包里的）用
// `list.HandleFocus` 包一下框架的 `ui.ListState.Handle` 就行——那是同一个
// 框架机制。
type focusTarget = list.Focusable

// registerContentFocus 登记「内容区里当前那一个能被方向键走位的」。
//
// 每一帧各个列表都来登记一次，**最后登记的那个赢**——而最后登记的那个就是
// 画面上**最后画**的那一个（内容区的主列表画在作者栏 / 分组栏之后，理由见
// `widgets/list` 的 `focus.go`）。
//
// ⚠️ **每帧开头要清空**（见 `Shell.View`）：不清的话，切到「没有列表」的那一
// 页之后，焦点会被交给一个**不在画面上**的列表（用户看不见任何指示，而键盘
// 也收不到方向键了）。
func (s *Shell) registerContentFocus(l focusTarget) {
	s.contentFocus = l
}

// focusContent 请求把焦点交给内容区。
//
// ⚠️ **只是记下来，不当场兑现**：理由见文件头那一段。
func (s *Shell) focusContent() {
	s.focusIntoContent = true
}

// applyContentFocus 兑现「把焦点交给内容区」这个请求。
//
// 排在**这一遍 build 的最后**（见 `Shell.View`）：内容区已经画完了，登记也
// 已经拿到了。
//
// ⚠️ **兑现不到就什么都不做**：这一页没有列表（设置页、扫码登录、404）或者
// 内容区整个没画（满屏播放器开着）时，没有「内容区的主列表」可交——焦点留在
// 侧栏那一项上，用户按 ↑ / ↓ 什么也不发生，但这比把焦点交给一个看不见的东西
// 好。
func (s *Shell) applyContentFocus() {
	if !s.focusIntoContent {
		return
	}
	s.focusIntoContent = false
	if s.contentFocus == nil {
		return
	}
	s.contentFocus.TakeFocus()
}

// focusSide 把焦点交回侧栏的第一项。
//
// ⚠️ **不传窗口**：框架的 `Handle.Focus` 不传窗口时用的是「这个句柄唯一绑定着
// 的那个窗口」，而 `sideAnchor` 是**逐窗口**的一份（壳是逐窗口的）——所以它
// 只有一个绑定。
func (s *Shell) focusSide() {
	s.sideAnchor.Focus()
}

// sideFocusItem 把侧栏的第一项登记成「焦点回侧栏时落回的那一项」。
//
// ⚠️ **只有第一项登记**：登记是「一个句柄绑一个控件」，而这一帧侧栏里画着的
// 菜单项有好几个——全登记的话框架会当场 panic（`Handle bound to two controls
// in one build`）。
func (s *Shell) sideFocusItem(item ui.Element) {
	item.Bind(&s.sideAnchor)
}
