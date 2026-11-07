package ui

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// toolbarSortSelect 是页面工具条上的下拉（原型 `.select`，`sel()`）：高 36、左 12 右 10、
// 圆角 8、line-strong 描边、surface-2 底、13 号字，右边一枚 14 的向下箭头；展开的列表是
// 弹层面板（surface、圆角 12、`--shadow-pop`），选中项 accent-text 600 加对勾。
//
// 返回这一帧选中项变了没有（`*cur` 已经改好）。label 给每一项的显示字。
func toolbarSortSelect[T comparable](c *ui.Context, cur *T, opts []T, label func(T) string, minW float32) (changed bool) {
	t := c.Theme()
	pl := tok.Of(t)
	sel := ui.SelectBase[T](c, cur)

	trigger := sel.Trigger.Height(tok.SelectH).Radius(tok.R8).
		Padding(space.None, space.Px10, space.None, space.Px12).Gap(space.Px8).AlignItems(ui.Center).
		Background(pl.Surface2).MinWidth(scale.Font(c, minW)).Shrink(0)
	border := pl.LineStrong
	if trigger.Hovered() {
		border = pl.Text3
	}
	trigger = trigger.Border(1, border).Transition(motion.Fade).Label("排序：" + label(*cur))
	trigger.Children(func() {
		ui.Text(c, label(*cur)).FontSize(scale.Font(c, scale.Size13)).TextColor(pl.Text).SingleLine()
		ui.Icon(c, pud.Icon("down")).FontSize(14).TextColor(pl.Text3)
	})

	sel.Popup(func(panel ui.Element) {
		popupMenuPanel(t, panel.Padding(space.Px8))
		for _, o := range opts {
			item := sel.Item(o).Padding(space.Px8, space.Px10).Radius(tok.R8).Gap(space.Px16).AlignItems(ui.Center)
			if item.Highlighted() {
				item = item.Background(pl.Hover)
			}
			checked := o == *cur
			item.Children(func() {
				lbl := ui.Text(c, label(o)).FontSize(scale.Font(c, scale.Size13)).TextColor(pl.Text).SingleLine().Grow(1)
				if checked {
					lbl.TextColor(pl.AccentText).FontWeight(600)
					ui.Icon(c, pud.Icon("check")).FontSize(14).TextColor(pl.AccentText)
				}
			})
		}
	})
	return trigger.Changed()
}
