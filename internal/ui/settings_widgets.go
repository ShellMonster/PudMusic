// 设置页的积木：分组、分组卡、设置行、开关行、下拉。数值全部来自原型 CSS
// `Settings` 一段（index.html 491–533 行）：
//
//	.set-group   下面空 40，最宽 720
//	.set-group h3 宋体 20，下面空 12
//	.set-card    1px line 描边、圆角 12、surface 底
//	.set-row     上下 16、左右 20、最小高 64、缝 16；行与行之间 1px line
//	.set-row strong 14 / 500；small 13 / text-2；两者之间空 16（`--od-gap`）
//
// 设置行里的控件一律用 `widgets/pud` 的（Switch / SelectBox 的外观 / Button），
// 旧的 `flat` 系控件不再用于这一页。
package ui

import (
	"fmt"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/leading"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shadow"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

const (
	// setGroupMaxW 是 `.set-group` 的最大宽。
	setGroupMaxW = 720
	// setGroupGap 是分组之间的距离（`.set-group` 的 margin-bottom）。
	setGroupGap = 40
	// setRowMinH / setRowPadY / setRowPadX 是 `.set-row` 的最小高和内边距。
	setRowMinH = 64
	setRowPadY = 16
	setRowPadX = 20
	// setNavW / setLayoutGap 是 `.set-layout` 的两列：180 的分类导航 + 缝 48。
	setNavW      = 180
	setLayoutGap = 48
)

// setGroup 是一个设置分组：宋体 20 的小标题（title 可以空）+ body。
func (s *Shell) setGroup(c *ui.Context, key, title string, body func()) {
	ui.Column(c).Key("set-group:"+key).FillWidth().MaxWidth(setGroupMaxW).
		Margin(space.None, space.None, setGroupGap, space.None).Children(func() {
		if title != "" {
			typo.Display(ui.Text(c, title), scale.Font(c, scale.Subhead)).LineHeight(leading.Body).
				Margin(space.None, space.None, space.Px12, space.None)
		}
		body()
	})
}

// setCard 是一张分组卡（`.set-card`）。
type setCard struct {
	s   *Shell
	c   *ui.Context
	key string
	ro  bool
	n   int
}

// card 画一张分组卡，body 里用 `cd.row` / `cd.switchRow` / `cd.selectRow` 往里放行。
func (s *Shell) card(c *ui.Context, key string, ro bool, body func(cd *setCard)) {
	p := tok.Of(c.Theme())
	cd := &setCard{s: s, c: c, key: key, ro: ro}
	ui.Column(c).Key("set-card:"+key).FillWidth().Radius(tok.R12).Background(p.Surface).
		Border(1, p.Line).Clip().Children(func() { body(cd) })
}

// divider 画行与行之间那条 1px 的 line（第一行前面不画）。
func (cd *setCard) divider() {
	if cd.n > 0 {
		ui.Box(cd.c).Key(fmt.Sprintf("set-div:%s:%d", cd.key, cd.n)).FillWidth().Height(1).
			Background(tok.Of(cd.c.Theme()).Line)
	}
	cd.n++
}

// row 是一个设置行：左边 14/500 标题 + 13 的说明，右边 ctrl。help 可以空。
// key 必须稳定。ctrl 为 nil 时只有左边那块（整行当一段文字用）。
func (cd *setCard) row(key, title, help string, ctrl func()) {
	c := cd.c
	p := tok.Of(c.Theme())
	cd.divider()
	ui.Row(c).Key("set-row:"+key).FillWidth().MinHeight(setRowMinH).
		Padding(setRowPadY, setRowPadX).Gap(space.Px16).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(space.Px16).Children(func() {
			ui.Text(c, title).FontSize(scale.Font(c, scale.Size14)).LineHeight(leading.Body).FontWeight(500).TextColor(p.Text)
			if help != "" {
				ui.Text(c, help).FontSize(scale.Font(c, scale.Size13)).LineHeight(leading.Body).TextColor(p.Text2).Wrap()
			}
		})
		if ctrl != nil {
			ui.Row(c).Shrink(0).AlignItems(ui.Center).Children(ctrl)
		}
	})
}

// rowText 是左边那块自己画的设置行（标题 + 说明之外的东西，如下载目录的等宽路径）。
func (cd *setCard) rowEl(key string, left func(), ctrl func()) {
	cd.rowFocus(key, false, left, ctrl)
}

// rowFocus 是 rowEl 加「这一行正被别的页面指着」：要滚进视野（只在那一帧调一次）。
func (cd *setCard) rowFocus(key string, focused bool, left func(), ctrl func()) {
	c := cd.c
	cd.divider()
	row := ui.Row(c).Key("set-row:"+key).FillWidth().MinHeight(setRowMinH).
		Padding(setRowPadY, setRowPadX).Gap(space.Px16).AlignItems(ui.Center)
	if focused && cd.s.settings.focusScroll {
		cd.s.settings.focusScroll = false
		row.ScrollIntoView()
	}
	row.Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(space.Px16).Children(left)
		if ctrl != nil {
			ui.Row(c).Shrink(0).AlignItems(ui.Center).Children(ctrl)
		}
	})
}

// rowTitle / rowHelp 是 `rowEl` 左边两样的标准画法。
func rowTitle(c *ui.Context, title string) ui.Element {
	return ui.Text(c, title).FontSize(scale.Font(c, scale.Size14)).LineHeight(leading.Body).FontWeight(500).TextColor(tok.Of(c.Theme()).Text)
}

func rowHelp(c *ui.Context, help string) ui.Element {
	return ui.Text(c, help).FontSize(scale.Font(c, scale.Size13)).LineHeight(leading.Body).TextColor(tok.Of(c.Theme()).Text2).Wrap()
}

// switchRow 是「标题 + 说明 + 开关」一行。写成功才说「开了它会怎样」（`saySwitchHint`）。
func (cd *setCard) switchRow(key, title, help string, on bool, set func(*app.Settings, bool)) {
	cd.row(key, title, help, func() {
		v := on
		if pud.Switch(cd.c, "set-sw:"+key, &v, "开关："+title) && !cd.ro {
			if cd.s.applySettingOK(title, func(cur *app.Settings) { set(cur, v) }) {
				cd.s.saySwitchHint(title, v)
			}
		}
	})
}

// selectRow 是「标题 + 说明 + 下拉」一行。
func selectRow[T comparable](cd *setCard, key, title, help string, cur T, items []enumItem[T], apply func(T)) {
	cd.row(key, title, help, func() {
		v := cur
		dropSelect(cd.c, "set-sel:"+key, &v, items, "选择"+title, cd.ro)
		if v != cur {
			apply(v)
		}
	})
}

// dropSelect 是原型 `.select` 的下拉：收起是 `pud.SelectBox` 的样子（高 36、圆角 8、
// line-strong 描边、surface-2 底、13 号字 + 14 的向下箭头），展开是 `.pop` 面板
// （`PopPanel` 的样子）里一排 `PopOpt` 的样子（选中项 accent-text 600 + 对勾）。
//
// 存的是 value，画的是 label；`label` 是这个下拉自己的无障碍名字。
func dropSelect[T comparable](c *ui.Context, key string, value *T, items []enumItem[T], label string, ro bool) {
	dropSelectW(c, key, value, items, label, ro, false)
}

// dropSelectW 是 pudSelect 加「撑满父那一格」（代理表单里的类型下拉是整行宽）。
func dropSelectW[T comparable](c *ui.Context, key string, value *T, items []enumItem[T], label string, ro, fill bool) {
	p := tok.Of(c.Theme())
	labelOf := func(v T) string {
		for _, it := range items {
			if it.value == v {
				return it.label
			}
		}
		return fmt.Sprint(v)
	}
	c.Key(key)
	sel := ui.SelectBase(c, value)
	tr := sel.Trigger.Height(tok.SelectH).Radius(tok.R8).Shrink(0).
		Padding(space.None, space.Px10, space.None, space.Px12).Gap(space.Px8).AlignItems(ui.Center).
		Background(p.Surface2).Disabled(ro).Label(label)
	if fill {
		tr = tr.FillWidth()
	}
	border := p.LineStrong
	if tr.Hovered() && !ro {
		border = p.Text3
	}
	tr = tr.Border(1, border).Transition(motion.Fade)
	tr.Children(func() {
		t := ui.Text(c, labelOf(*value)).FontSize(scale.Font(c, scale.Size13)).LineHeight(leading.Body).TextColor(p.Text).SingleLine()
		if fill {
			t.Grow(1)
		}
		ui.Icon(c, pud.Icon("down")).FontSize(14).TextColor(p.Text3)
	})
	sel.Popup(func(panel ui.Element) {
		pp := panel.MinWidth(tok.PopMinW).Padding(space.Px8).Radius(tok.R12).
			Background(p.Surface).Border(1, p.Line).Transition(motion.PopIn)
		shadow.Pop(pp, c.Theme()).Children(func() {
			for _, it := range items {
				checked := it.value == *value
				opt := sel.Item(it.value).Padding(space.Px8, space.Px10).Radius(tok.R8).Gap(space.Px16).
					AlignItems(ui.Center)
				if opt.Highlighted() {
					opt = opt.Background(p.Hover)
				}
				opt.Children(func() {
					t := ui.Text(c, it.label).FontSize(scale.Font(c, scale.Size13)).LineHeight(leading.Body).TextColor(p.Text).SingleLine().Grow(1)
					if checked {
						t.TextColor(p.AccentText).FontWeight(600)
						ui.Icon(c, pud.Icon("check")).FontSize(14).TextColor(p.AccentText)
					}
				})
			}
		})
	})
}
