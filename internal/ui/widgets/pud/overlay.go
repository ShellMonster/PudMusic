package pud

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scrim"
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shadow"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// PopPanel 是弹层面板（原型 `.pop`）：surface 底、1 像素 line 描边、圆角 12、
// `--shadow-pop`、内边距 8、最小宽 200；进场 `motion.PopIn`。key 必须稳定。
// 位置由调用方用 `AttachTo` / `Absolute` 摆（播放条上的弹层在按钮上方 12）。
func PopPanel(c *ui.Context, key string) ui.Element {
	p := pal(c)
	e := ui.Column(c).Key(key).MinWidth(tok.PopMinW).Padding(space.Px8).Radius(tok.R12).
		Background(p.Surface).Border(1, p.Line).Transition(motion.PopIn)
	return shadow.Pop(e, c.Theme())
}

// PopStyle 给一块**别处建好的面板**（框架 `PopoverBase` 递进回调的那一块）上 `.pop` 的外观：surface 底、
// 1 像素 line 描边、圆角 12、`--shadow-pop`、内边距 8、最小宽 minW、进场 `motion.PopIn`。位置（`AttachTo` /
// `Margin`）由调用方接着链。key 必须稳定。
//
// `PopPanel` 自己建面板；需要 `PopoverBase` 管「点外面关 / Esc 关」的地方（全屏播放器底栏的弹层）走这个。
func PopStyle(c *ui.Context, panel ui.Element, key string, minW float32) ui.Element {
	p := pal(c)
	e := panel.Key(key).MinWidth(minW).Padding(space.Px8).Radius(tok.R12).
		Background(p.Surface).Border(1, p.Line).Transition(motion.PopIn).Label(key)
	return shadow.Pop(e, c.Theme())
}

// PopHeading 是弹层里的小标题（原型 `.pop h5`：12 号 text-3，左右 8、上 4 下 8）。
func PopHeading(c *ui.Context, s string) ui.Element {
	return text(c, s, scale.Size12, pal(c).Text3).FixedLineHeight(18.6).Margin(space.Px4, space.Px8, space.Px8, space.Px8)
}

// PopSep 是弹层里的分隔线（原型 `.pop-sep`：1 像素 line，上下 6、左右 4）。
func PopSep(c *ui.Context) ui.Element {
	return ui.Box(c).FillWidth().Height(1).Background(pal(c).Line).Margin(space.Px6, space.Px4)
}

// PopOpt 是弹层里的一个单选项（原型 `.pop-opt` + `aria-checked`）：整行宽，内边距
// 8/10、圆角 8、13 号；名字在上，note（12 号 text-3，如「FLAC 16bit」）在名字下面
// 另起一行；悬停 hover 底；选中（checked）时名字 accent-text 600，右边一枚 14 的对勾。
func PopOpt(c *ui.Context, key, label, note string, checked bool) ui.Element {
	p := pal(c)
	b := ui.ButtonBase(c).Key(key).Label(label).FillWidth().Padding(space.Px8, space.Px10).Gap(space.Px16).
		AlignItems(ui.Center).Radius(tok.R8)
	if b.Hovered() {
		b = b.Background(p.Hover)
	}
	b = b.Transition(motion.Fade)
	b.Children(func() {
		ui.Column(c).Grow(1).Children(func() {
			// 原型行高 1.55（body 继承）：13 号 ≈ 20.15，一项 8 + 20 + 8 = 36。
			t := text(c, label, scale.Size13, p.Text).FixedLineHeight(20.15).SingleLine()
			if checked {
				t.TextColor(p.AccentText).FontWeight(600)
			}
			if note != "" {
				note := text(c, note, scale.Size12, p.Text3).FixedLineHeight(18.6).SingleLine()
				if checked {
					note.TextColor(p.AccentText)
				}
			}
		})
		if checked {
			glyph(c, "check", 14, p.AccentText)
		}
	})
	return b
}

// PopItem 是弹层里的一个普通菜单项（原型 `item()`）：和 `PopOpt` 同一个壳，没有说明
// 行也没有对勾；danger 时字是 err 色（删除类）。
func PopItem(c *ui.Context, key, label string, danger bool) ui.Element {
	p := pal(c)
	b := ui.ButtonBase(c).Key(key).Label(label).FillWidth().Padding(space.Px8, space.Px10).
		AlignItems(ui.Center).Radius(tok.R8)
	if b.Hovered() {
		b = b.Background(p.Hover)
	}
	b = b.Transition(motion.Fade)
	b.Children(func() {
		fg := p.Text
		if danger {
			fg = p.Err
		}
		text(c, label, scale.Size13, fg).FixedLineHeight(20.15).SingleLine().Grow(1)
	})
	return b
}

// PopText 是弹层里的一段说明（原型 `.pop-text`：13 号次要色、行高 1.6、最宽 300、
// 内边距 4/10/12）。
func PopText(c *ui.Context, s string) ui.Element {
	return text(c, s, scale.Size13, pal(c).Text2).MaxWidth(300).Padding(space.Px4, space.Px10, space.Px12, space.Px10)
}

// Scrim 是抽屉 / 对话框后面的遮罩（原型 `.scrim`）：抽屉用 32% 黑、盖到播放条上沿
// （`bottom` = 播放条高，传 0 盖满）；对话框用 `DialogScrim`（50%）。点它关掉。
// 返回被点了没有。进场 `motion.ScrimIn`。
func Scrim(c *ui.Context, key string, bottom float32) (clicked bool) {
	b := ui.ButtonBase(c).Key(key).Label("关闭").Absolute().Left(0).Top(0).Right(0).Bottom(bottom).
		Background(ui.RGBA(0, 0, 0, scrim.Drawer)).Transition(motion.ScrimIn)
	return b.Clicked()
}

// DialogScrim 是对话框的遮罩（原型 `dialog::backdrop`：50% 黑）。
func DialogScrim(c *ui.Context, key string) (clicked bool) {
	b := ui.ButtonBase(c).Key(key).Label("关闭").Absolute().Left(0).Top(0).Right(0).Bottom(0).
		Background(ui.RGBA(0, 0, 0, scrim.Dialog)).Transition(motion.ScrimIn)
	return b.Clicked()
}

// DrawerPanel 是右侧抽屉（原型 `.drawer`）：贴右、从顶到播放条上沿（bottom = 播放条
// 高）、宽 400（窄窗铺满）、surface 底、左边 1 像素 line、`--shadow-pop`；从右边整块
// 滑进来（`motion.DrawerIn`）。里面按「头 / 体 / 脚」三段排。
func DrawerPanel(c *ui.Context, key string, bottom float32) ui.Element {
	p := pal(c)
	e := ui.Column(c).Key(key).Absolute().Top(0).Right(0).Bottom(bottom).Width(tok.DrawerW).
		Background(p.Surface).Transition(motion.DrawerIn(tok.DrawerW)).
		BorderStartWidth(1).BorderColor(p.Line)
	return shadow.Pop(e, c.Theme())
}

// DrawerHead 是抽屉头（原型 `.drawer-head`）：24 号宋体标题撑满，右边是操作键；
// 内边距 上 20 / 右 16 / 下 12 / 左 24，键之间 8。
func DrawerHead(c *ui.Context, title string, actions func()) {
	ui.Row(c).FillWidth().Padding(space.Px20, space.Px16, space.Px12, space.Px24).Gap(space.Px8).
		AlignItems(ui.Center).Children(func() {
		typo.Display(ui.Text(c, title), scale.Font(c, scale.SectionTitle)).Grow(1).SingleLine()
		if actions != nil {
			actions()
		}
	})
}

// DrawerSub 是抽屉头下面的一行说明（原型 `.drawer-sub`：12 号 text-3，左右 24、
// 下 12、底下一条 line）。
func DrawerSub(c *ui.Context, children func()) {
	p := pal(c)
	ui.Row(c).FillWidth().Padding(space.None, space.Px24, space.Px12, space.Px24).Gap(space.Px8).AlignItems(ui.Center).
		Draw(func(pp *ui.Painter, r ui.Rect) {
			pp.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 1, W: r.W, H: 1}, p.Line, 0)
		}).Children(children)
}

// DialogPanel 是对话框面板（原型 `dialog.dlg`）：居中、宽 440（≤ 窗口宽 − 32）、
// surface 底、1 像素 line 描边、圆角 12、`--shadow-pop`；进场 `motion.PopIn`。
// 里面用 `DialogBody` + `DialogFoot`。
func DialogPanel(c *ui.Context, key string) ui.Element {
	p := pal(c)
	e := ui.Column(c).Key(key).Width(tok.DialogW).Background(p.Surface).Border(1, p.Line).
		Radius(tok.R12).Transition(motion.PopIn)
	return shadow.Pop(e, c.Theme())
}

// DialogBody 是对话框正文（原型 `.dlg-body`）：内边距 24；标题 24 号宋体（下面
// 空 8）、说明次要色。
func DialogBody(c *ui.Context, title, body string) {
	p := pal(c)
	ui.Column(c).FillWidth().Padding(space.Px24).Children(func() {
		typo.Display(ui.Text(c, title), scale.Font(c, scale.SectionTitle))
		if body != "" {
			text(c, body, scale.Size14, p.Text2).Margin(space.Px8, space.None, space.None, space.None)
		}
	})
}

// DialogFoot 是对话框底部的按钮排（原型 `.dlg-foot`）：靠右、缝 12、内边距
// 0 24 24。
func DialogFoot(c *ui.Context, buttons func()) {
	ui.Row(c).FillWidth().Justify(ui.End).Gap(space.Px12).Padding(space.None, space.Px24, space.Px24, space.Px24).
		Children(buttons)
}

// ToastLife 是 Toast 默认停多久（原型 `toast()` 的 2.4 秒；带操作键的停久一点）。
const (
	ToastLife       = 2400 * time.Millisecond
	ToastActionLife = 5 * time.Second
)

// Toast 画一枚提示（原型 `.toast`）：**反色胶囊**——正文色底、页面底色字，13 号 500，
// 内边距 10/16、缝 8、`--shadow-pop`；可带一枚 16 的图标，可带一个操作键（主色字
// 600，离文字 8）。进场 `motion.ToastIn`。key 必须稳定且唯一（提示是动态列表）。
// 返回操作键被点了没有。
func Toast(c *ui.Context, key, msg, icon, action string) (acted bool) {
	p := pal(c)
	// 反色：深色下底是浅的、字是深的，操作键的主色字要在浅底上读得出来——浅底上用
	// accent（深色主题）/ 原型给浅色主题的 #ff8a70 的对位：压深一档的 accent-text 反着用。
	actionFg := p.Accent
	if !c.Theme().Dark {
		actionFg = ui.Hex("#ff8a70")
	}
	e := ui.Row(c).Key(key).Padding(space.Px10, space.Px16).Gap(space.Px8).AlignItems(ui.Center).
		Radius(tok.Pill / 2).Background(p.Text).Transition(motion.ToastIn).Shrink(0)
	e = shadow.Pop(e, c.Theme())
	e.Children(func() {
		if icon != "" {
			glyph(c, icon, tok.IconSm, p.Bg)
		}
		text(c, msg, scale.Size13, p.Bg).FontWeight(500).FixedLineHeight(20.15).SingleLine()
		if action != "" {
			b := ui.ButtonBase(c).Key(key+":action").Margin(space.None, space.None, space.None, space.Px8)
			b.Children(func() {
				text(c, action, scale.Size13, actionFg).FontWeight(600).SingleLine()
			})
			acted = b.Clicked()
		}
	})
	return acted
}
