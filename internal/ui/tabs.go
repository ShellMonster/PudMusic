// 主 Tab 的那一种样式：**扁平**（选中那一项是一块淡底 + 加粗的字，没有下划线）。
//
// # 为什么不是框架的 `ui.Tabs`
//
// 框架那一套是**下划线**：选中那一项底下画一条主色的横条（`mygo@v0.5.5/ui/tabs.go`
// 的 `coreTabs` 里的 `DrawOver`）。而这一票要的是「**主 Tab 和次级排序不是同一个
// 样子**」（票 #138）——主 Tab 用这一套，次级排序保持框架那一条下划线。
//
// 而框架给了 `ui.TabsBase`：「没有外观的 Tab」。点击、左右方向键、Home/End、
// 焦点跟着选中走，全是框架给的那一套，我们不去重写——**只有画法是自己的**。
//
// # 七处一起换（换四处留三处更难看）
//
// 主 Tab 四处：首页那三档、搜索结果那三档、设置那四档、UP 空间那四档。
// 次级排序三处：歌词搜索的来源、歌词预览的原文 / 译文、搜索结果的排序。
//
// ⚠️ **次级那三处保持框架的 `ui.Tabs`**：它们是「排序 / 来源」那一档的小一档
// 选择，下划线那一套正合适（上游 HeroUI 的 `variant="light"` 是另一种，但我们
// 这里的次级排序沿用框架这一套，见 `searchpage.go` 的 `viewSearchToolbar`）。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/flat"
	"github.com/egoist/mygo/ui"
)

// flatTabGap 是主 Tab 之间隔多远。
//
// 框架那一套是 `t.Space(1)`（4）。这里取 8：选中那一项有一块淡底，4 的缝会让
// 两块底几乎连成一片（看着像一条灰带，而不是一格一格）。
const flatTabGap = 8

// flatTabs 画一排**扁平**的主 Tab。selected 是当前选中的下标。
//
// ⚠️ **选中那一层用 `flatCurrent`**（侧栏「当前那一项」那一份配方）：浅色下是
// 主色很淡的一层底 + 正文的字，深色下是中性灰底 + 主色的字。**别拿主色当字色
// 用在浅色底下**——那个亮绿压在白底上不到 2:1，整行读不出来（理由在
// `flatbutton.go` 的 `flatCurrent` 那一段）。
//
// ⚠️ **悬停 / 按下那一层用 `flat.Feel`**：Tab 是 `TabsBase` 给的，我们拿不到
// 框架那个 `styleFn`，而反馈这一层不是装饰（没有它用户不知道哪里点得动）。
func flatTabs(c *ui.Context, selected *int, labels ...string) ui.Element {
	t := c.Theme()
	tabs := ui.TabsBase(c, selected, len(labels))
	tabs.List.Gap(flatTabGap).Children(func() {
		for i, label := range labels {
			// ⚠️ **内边距照框架那一套**（`t.Space(2), t.Space(3)`）：换掉的话
			// 四页的 Tab 那一行高度就变了，而它下面那些内容会跟着挪——而这一票
			// 要改的是「样子」，不是「版面」。
			tab := tabs.Tab(i).
				Padding(t.Space(2), t.Space(3)).
				Radius(t.Radius)
			if i == *selected {
				tab = flatCurrent(c, tab).FontWeight(600)
			} else {
				tab = tab.TextColor(t.TextMuted)
			}
			tab = flat.Feel(c, tab)
			tab.Children(func() { ui.Text(c, label).SingleLine() })
		}
	})
	return tabs.List
}
