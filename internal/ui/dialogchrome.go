// 对话框外壳（「深夜唱片行」，原型 `dialog.dlg` / `.dlg-body` / `.dlg-foot`，DESIGN.md §0′.T3）。
//
// 加入收藏夹、新建 / 修改收藏夹、裁剪封面三个对话框用它：面板 surface 底、1 像素 line 描边、
// 圆角 12、`--shadow-pop`，遮罩 50% 黑；正文内边距 24、标题 24 号宋体；脚靠右、缝 12。
// 面板里的内容是各个对话框自己的事，外壳只管长相。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scrim"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shadow"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// 对话框宽度（原型 `dialog.dlg` 440、`dialog.sheet` 480，都不超过窗口宽 − 32）。
const (
	dlgW      float32 = 440
	dlgSheetW float32 = 480
)

// dlgPanel 给 `ui.DialogBase` 回调里的遮罩和面板上外观。w 是面板宽，窄窗口自动缩到窗口宽 − 32。
func dlgPanel(c *ui.Context, backdrop, panel ui.Element, label string, w float32) {
	t := c.Theme()
	pl := tok.Of(t)
	ww, hh := c.Size()
	backdrop.Background(ui.RGBA(0, 0, 0, scrim.Dialog))
	e := panel.Padding(space.None).Gap(space.None).Radius(tok.R12).
		Background(pl.Surface).Border(1, pl.Line).Width(min(w, ww-32)).MaxHeight(hh - 64).
		Transition(motion.PopIn)
	shadow.Pop(e, t).Role(ui.RoleDialog).Label(label + "（对话框）")
}

// dlgBody 是对话框正文那一块：内边距 24；标题 24 号宋体，下面空 8 放说明（次要色 14 号），
// 再下面是 children（表单、列表……）。
func dlgBody(c *ui.Context, title, body string, children func()) {
	pl := tok.Of(c.Theme())
	ui.Column(c).FillWidth().Padding(space.Px24).Children(func() {
		typo.Display(ui.Text(c, title), scale.Font(c, scale.SectionTitle)).TextColor(pl.Text)
		if body != "" {
			ui.Text(c, body).FontSize(scale.Font(c, scale.Size14)).TextColor(pl.Text2).
				MaxLines(2).Ellipsis("…").Margin(space.Px8, space.None, space.None, space.None)
		}
		if children != nil {
			children()
		}
	})
}

// dlgFoot 是对话框的按钮排：靠右、缝 12、内边距 0 24 24。left 非 nil 时在最左边多放一块
// （撑满剩下的宽，比如「全选」）。
func dlgFoot(c *ui.Context, left, buttons func()) {
	ui.Row(c).FillWidth().Gap(space.Px12).AlignItems(ui.Center).
		Padding(space.None, space.Px24, space.Px24, space.Px24).Children(func() {
		ui.Row(c).Grow(1).Shrink(1).Gap(space.Px12).AlignItems(ui.Center).Children(func() {
			if left != nil {
				left()
			}
		})
		buttons()
	})
}

// dlgPrimary 是脚上的主按钮：忙的时候是转圈的主色键（沿用 `busyPrimaryButton`），否则
// `pud.Button` 主色，disabled 时灰着。返回这一帧有没有被点。
func dlgPrimary(c *ui.Context, key, label string, busy, disabled bool) bool {
	if busy {
		busyPrimaryButton(c, label)
		return false
	}
	return pud.Button(c, key, pud.Primary, pud.Std, label, "").Disabled(disabled).Clicked()
}
