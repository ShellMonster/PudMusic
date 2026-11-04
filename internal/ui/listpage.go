// 「深夜唱片行」列表类页面（搜索、稍后再看、历史、本地音乐、下载、统计）共用的骨架和
// 几件小零件，DESIGN.md §0′.T7。
//
// 页面外壳是原型的 `.page`：内容区居中、最宽 1280、左右边距 40 / 32 / 16（按窗口宽）、上 8；页头是
// `pud.PageHead`（Eyebrow + 48 宋体标题 + 导语 + 右侧操作）。
package ui

import (
	"strconv"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// pudPageMaxW 是原型 `.page { max-width: 1280px }`（含左右内边距）。
const pudPageMaxW = 1280

// pudPage 摆一页的外壳。key 是页面根元素的身份；body 收到**窗口宽**（页头标题字号、断点都按它，
// 原型的视口）和**可用内容宽**（去掉页边距、封顶 1280 之后，曲目行 / 表头用它）。
func (s *Shell) pudPage(c *ui.Context, key string, body func(winW, listW float32)) {
	// 断点和流式字号一律按**窗口宽**（原型的视口），页边距 / 标题字号 / 专辑列都由它定。
	winW, _ := c.Size()
	cw := s.spaceContentWidth(c)
	if cw <= 0 {
		cw = winW - tok.SideW
	}
	pad := space.PageHorizontalForWindow(winW)
	inner := min(cw, pudPageMaxW)
	ui.Column(c).Key(key).Grow(1).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Width(inner).Padding(space.PageTop, pad, space.None, pad).
			AlignItems(ui.Stretch).Children(func() { body(winW, inner-2*pad) })
	})
}

// pudSelect 是工具条上的下拉框（原型 `sel()`）：`pud.SelectBox` 加一个 `PopPanel` 里的
// 单选列表。open 是展开状态（页面自己存），cur 是选中的序号；返回这一帧是否换了选项。
func pudSelectBox(c *ui.Context, key string, open *bool, labels []string, cur *int) (changed bool) {
	if *cur < 0 || *cur >= len(labels) {
		*cur = 0
	}
	b := pud.SelectBox(c, key, labels[*cur], 0)
	if b.Clicked() {
		*open = !*open
	}
	ui.Popover(c, b, open, func() {
		pud.PopPanel(c, key+":pop").Children(func() {
			for i, l := range labels {
				if pud.PopOpt(c, key+":o"+strconv.Itoa(i), l, "", i == *cur).Clicked() {
					if i != *cur {
						*cur = i
						changed = true
					}
					*open = false
				}
			}
		})
	})
	return changed
}

// pudSortIcon 是工具条最左边那枚 16 号排序图标（原型 `icon('sort', 16)`，text-2）。
func pudSortIcon(c *ui.Context) {
	ui.Icon(c, pud.Icon("sort")).FontSize(16).TextColor(tok.Of(c.Theme()).Text2)
}

// pudToolbar 摆一行工具条（原型 `.toolbar`：上 24、缝 12、垂直居中）。
func pudToolbar(c *ui.Context, key string, children func()) {
	ui.Row(c).Key(key).FillWidth().Gap(space.Px12).AlignItems(ui.Center).
		Margin(space.Px24, space.None, space.None, space.None).Children(children)
}

// pudEmpty 画原型的空状态（插画 + 24 宋体标题 + 说明 + 可选操作）。
func pudEmpty(c *ui.Context, key string, art pud.ArtKind, title, body string, action func()) {
	ui.Column(c).Key(key).FillWidth().Children(func() {
		pud.Empty(c, art, title, body, action)
	})
}
